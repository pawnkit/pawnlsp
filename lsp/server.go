package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/query"
	"github.com/pawnkit/pawn-analysis/sema"
	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	"github.com/pawnkit/pawn-project/fsx"
	projectinclude "github.com/pawnkit/pawn-project/include"
	projectmodel "github.com/pawnkit/pawn-project/project"
	"github.com/pawnkit/pawnfmt"
	corediagnostic "github.com/pawnkit/pawnkit-core/diagnostic"
	coresource "github.com/pawnkit/pawnkit-core/source"
	"github.com/pawnkit/pawnlint/pkg/diagnostic"
	"github.com/pawnkit/pawnlint/pkg/editor"
	"github.com/pawnkit/pawnlint/pkg/lint"
	lintproject "github.com/pawnkit/pawnlint/pkg/project"
	lintrules "github.com/pawnkit/pawnlint/pkg/rules"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type document struct {
	URI           string
	Path          string
	Root          string
	Entry         string
	Text          []byte
	Buffer        *coresource.TextBuffer
	Index         *coresource.LineIndex
	Version       int
	Diagnostics   []diagnostic.Diagnostic
	Includes      preprocess.IncludeResolver
	Candidates    includeCandidateProvider
	Names         sema.Resolver
	Analysis      *analysis.Result
	Revision      int64
	ready         chan struct{}
	analysisReady chan struct{}
	analysisOnce  sync.Once
	fullOnce      sync.Once
	cancel        context.CancelFunc
}

type publishRequest struct {
	doc      *document
	snapshot *query.Snapshot
	delay    time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

type publishQueue struct {
	active  *publishRequest
	pending *publishRequest
}

type server struct {
	in               *bufio.Reader
	out              io.Writer
	documents        map[string]*document
	names            sema.Resolver
	snapshot         *query.Snapshot
	shutdown         bool
	mu               sync.Mutex
	writeMu          sync.Mutex
	workers          sync.WaitGroup
	requestWorkers   sync.WaitGroup
	requestCancels   map[string]context.CancelFunc
	progressCancels  map[string]context.CancelFunc
	workDoneProgress bool
	rules            *lint.Registrar
	managedRoots     []string
	workspaces       map[string]*workspaceIndex
	projectRevision  int64
	parseCache       *lintproject.ParseCache
	tokenCache       *preprocess.TokenCache
	publishes        map[string]*publishQueue
	nextRequestID    atomic.Uint64
	lint             func(context.Context, *document, *lintproject.ParseCache, *analysis.Result) ([]diagnostic.Diagnostic, error)
	analysisTrace    func(string, int, analysis.TraceEvent)
}

const (
	analysisOutputTokenLimit = 8_000_000
	documentPublishDebounce  = 25 * time.Millisecond
)

type apiNameResolver struct {
	index   *pawnapi.Index
	profile string
}

type projectIncludeResolver struct {
	resolver interface {
		Resolve(fromFile, spec string, quoted bool) (string, bool)
		Complete(fromFile, prefix string, quoted bool, limit int) []projectinclude.Candidate
	}
	fsys fsx.FS
}

func (r projectIncludeResolver) Complete(fromURI, prefix string, angle bool, limit int) []projectinclude.Candidate {
	fromFile, err := coresource.URI(fromURI).Filename()
	if err != nil {
		return nil
	}
	return r.resolver.Complete(fromFile, prefix, !angle, limit)
}

func (r projectIncludeResolver) Resolve(fromURI, path string, angle bool) ([]byte, string, bool) {
	fromFile, err := coresource.URI(fromURI).Filename()
	if err != nil {
		return nil, "", false
	}
	resolved, ok := r.resolver.Resolve(fromFile, path, !angle)
	if !ok {
		return nil, "", false
	}
	content, err := r.fsys.ReadFile(resolved)
	if err != nil {
		return nil, "", false
	}
	return content, coresource.FileURI(resolved).String(), true
}

func loadProjectContext(path string, extraRoots ...string) (preprocess.IncludeResolver, string, string, string) {
	fsys := fsx.OS{}
	project, err := projectmodel.Load(coresource.NewRegistry(), fsys, path, projectmodel.Options{
		ManagedIncludeRoots: extraRoots,
	})
	if err != nil {
		return nil, "", filepath.Dir(path), ""
	}
	return projectIncludeResolver{
		resolver: project.IncludeResolver(),
		fsys:     fsys,
	}, project.Selection().ProfileID, project.Root(), project.Paths().Entry
}

func loadProjectIncludes(path string, extraRoots ...string) (preprocess.IncludeResolver, string) {
	resolver, profile, _, _ := loadProjectContext(path, extraRoots...)
	return resolver, profile
}

const managedIncludeRootLimit = 32

func cleanManagedIncludeRoots(roots []string) ([]string, error) {
	if len(roots) > managedIncludeRootLimit {
		return nil, fmt.Errorf("managed include roots exceed the limit of %d", managedIncludeRootLimit)
	}
	cleaned := make([]string, 0, len(roots))
	seen := make(map[string]bool)
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("managed include root %q must be absolute", root)
		}
		root = filepath.Clean(root)
		if !seen[root] {
			seen[root] = true
			cleaned = append(cleaned, root)
		}
	}
	return cleaned, nil
}

func (r apiNameResolver) ResolveName(name string) sema.NameState {
	if r.index == nil {
		return sema.NameUnknown
	}
	if slices.ContainsFunc(r.index.ByName(name), r.available) {
		return sema.NameFound
	}
	return sema.NameUnknown
}

func (r apiNameResolver) ResolveCallable(name string) (sema.Callable, bool) {
	if r.index == nil {
		return sema.Callable{}, false
	}
	for _, entry := range r.index.ByName(name) {
		if entry.Signature == nil || !r.available(entry) {
			continue
		}
		callable := sema.Callable{ReturnTag: entry.Signature.ReturnTag}
		for _, parameter := range entry.Signature.Parameters {
			if parameter.Variadic {
				callable.MaxArgs = -1
				return callable, true
			}
			callable.ParamTags = append(callable.ParamTags, parameter.Tag)
			callable.MaxArgs++
			if parameter.Default == nil {
				callable.MinArgs++
			}
		}
		return callable, true
	}
	return sema.Callable{}, false
}

func (r apiNameResolver) ResolveCallEffects(name string) (sema.CallEffects, bool) {
	if r.index == nil {
		return sema.CallEffects{}, false
	}
	for _, entry := range r.index.ByName(name) {
		if entry.Signature == nil || !r.available(entry) {
			continue
		}
		effects := sema.CallEffects{Complete: true, IntrinsicImpure: true}
		for index, parameter := range entry.Signature.Parameters {
			if parameter.Reference || (len(parameter.ArrayDimensions) > 0 && !parameter.Const) {
				effects.MutatedParameters = append(effects.MutatedParameters, index)
			}
		}
		return effects, true
	}
	return sema.CallEffects{}, false
}

func (r apiNameResolver) available(entry pawnapi.Entry) bool {
	if r.profile == "" {
		return true
	}
	for _, availability := range entry.Availability {
		if availability.Profile == r.profile {
			return true
		}
	}
	return false
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type lspDiagnostic struct {
	Range              lspRange                          `json:"range"`
	Severity           int                               `json:"severity"`
	Code               string                            `json:"code"`
	CodeDescription    *lspCodeDescription               `json:"codeDescription,omitempty"`
	Source             string                            `json:"source"`
	Message            string                            `json:"message"`
	RelatedInformation []lspDiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

type lspCodeDescription struct {
	Href string `json:"href"`
}

type lspDiagnosticRelatedInformation struct {
	Location lspLocation `json:"location"`
	Message  string      `json:"message"`
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

// Run serves LSP requests with default options.
func Run(in io.Reader, out io.Writer) error {
	return RunWithOptions(in, out, RunOptions{})
}

// RunOptions configures optional server diagnostics.
type RunOptions struct {
	AnalysisTrace func(uri string, version int, event analysis.TraceEvent)
}

// RunWithOptions serves LSP requests with options.
func RunWithOptions(in io.Reader, out io.Writer, opts RunOptions) error {
	apiIndex, err := pawnapi.Load()
	if err != nil {
		return fmt.Errorf("load Pawn API metadata: %w", err)
	}
	s := &server{
		in: bufio.NewReader(in), out: out, documents: make(map[string]*document),
		names: apiNameResolver{index: apiIndex}, snapshot: query.New(), rules: lintrules.Default(),
		workspaces: make(map[string]*workspaceIndex), parseCache: lintproject.NewParseCache(),
		tokenCache:      preprocess.NewTokenCache(),
		analysisTrace:   opts.AnalysisTrace,
		requestCancels:  make(map[string]context.CancelFunc),
		progressCancels: make(map[string]context.CancelFunc),
	}
	for {
		body, err := readFrame(s.in)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var request message
		if err := json.Unmarshal(body, &request); err != nil {
			if responseErr := s.respondError(nil, -32700, "parse error"); responseErr != nil {
				return errors.Join(err, responseErr)
			}
			continue
		}
		exit, err := s.handle(request)
		if err != nil {
			if hasRequestID(request.ID) {
				if responseErr := s.respondError(request.ID, -32602, err.Error()); responseErr != nil {
					return errors.Join(err, responseErr)
				}
			} else {
				fmt.Fprintf(os.Stderr, "pawnlsp: %s: %v\n", request.Method, err)
			}
			continue
		}
		if exit {
			return nil
		}
	}
}

func (s *server) handle(request message) (bool, error) {
	if request.Method == "" && hasRequestID(request.ID) {
		return false, nil
	}
	if s.shutdown && request.Method != "exit" {
		if len(request.ID) == 0 || bytes.Equal(request.ID, []byte("null")) {
			return false, nil
		}
		return false, s.respondError(request.ID, -32600, "server is shutting down")
	}
	switch request.Method {
	case "initialize":
		var params struct {
			Capabilities struct {
				Window struct {
					WorkDoneProgress bool `json:"workDoneProgress"`
				} `json:"window"`
			} `json:"capabilities"`
			InitializationOptions struct {
				IncludePaths []string `json:"includePaths"`
				PawnKit      *struct {
					ProtocolVersion     int      `json:"protocolVersion"`
					ManagedIncludeRoots []string `json:"managedIncludeRoots"`
				} `json:"pawnkit"`
			} `json:"initializationOptions"`
		}
		if len(request.Params) != 0 {
			_ = json.Unmarshal(request.Params, &params)
		}
		roots := params.InitializationOptions.IncludePaths
		if state := params.InitializationOptions.PawnKit; state != nil {
			if state.ProtocolVersion != 1 {
				return false, fmt.Errorf("unsupported PawnKit editor protocol version %d", state.ProtocolVersion)
			}
			roots = state.ManagedIncludeRoots
		}
		cleaned, err := cleanManagedIncludeRoots(roots)
		if err != nil {
			return false, err
		}
		s.managedRoots = cleaned
		s.workDoneProgress = params.Capabilities.Window.WorkDoneProgress
		s.projectRevision++
		return false, s.respond(request.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync": 2, "codeActionProvider": true,
				"callHierarchyProvider":  true,
				"diagnosticProvider":     map[string]any{"interFileDependencies": true, "workspaceDiagnostics": true},
				"completionProvider":     map[string]any{"triggerCharacters": []string{"@", "#", "<", "\"", "/", "\\"}, "resolveProvider": true},
				"documentSymbolProvider": true, "definitionProvider": true,
				"documentHighlightProvider": true,
				"foldingRangeProvider":      true,
				"inlayHintProvider":         true,
				"colorProvider":             true,
				"hoverProvider":             true, "referencesProvider": true,
				"renameProvider": map[string]any{"prepareProvider": true},
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{"tokenTypes": semanticTokenTypes, "tokenModifiers": semanticTokenModifiers},
					"full":   true,
				},
				"signatureHelpProvider":            map[string]any{"triggerCharacters": []string{"(", ","}, "retriggerCharacters": []string{","}},
				"selectionRangeProvider":           true,
				"workspaceSymbolProvider":          true,
				"documentFormattingProvider":       true,
				"documentRangeFormattingProvider":  true,
				"documentOnTypeFormattingProvider": map[string]any{"firstTriggerCharacter": "}", "moreTriggerCharacter": []string{";"}},
			},
			"serverInfo": map[string]any{"name": "pawnlsp"},
		})
	case "initialized":
		return false, nil
	case "shutdown":
		s.shutdown = true
		s.requestWorkers.Wait()
		s.cancelDocuments()
		s.workers.Wait()
		return false, s.respond(request.ID, nil)
	case "exit":
		s.requestWorkers.Wait()
		s.cancelDocuments()
		s.workers.Wait()
		return true, nil
	case "$/cancelRequest":
		s.cancelRequest(request.Params)
		return false, nil
	case "window/workDoneProgress/cancel":
		s.cancelProgress(request.Params)
		return false, nil
	case "textDocument/didOpen":
		return false, s.didOpen(request.Params)
	case "textDocument/didChange":
		return false, s.didChange(request.Params)
	case "textDocument/didClose":
		return false, s.didClose(request.Params)
	case "workspace/didChangeWatchedFiles":
		return false, s.reloadProjects()
	case "workspace/didChangeConfiguration":
		return false, s.didChangeConfiguration(request.Params)
	case "pawnkit/didChangeManagedTools":
		return false, s.didChangeManagedTools(request.Params)
	case "workspace/symbol":
		return false, s.workspaceSymbols(request.ID, request.Params)
	case "workspace/diagnostic":
		s.handleAsync(request, func(ctx context.Context) error {
			return s.workspaceDiagnostics(ctx, request.ID, request.Params)
		})
		return false, nil
	case "textDocument/codeAction":
		return false, s.codeActions(request.ID, request.Params)
	case "textDocument/prepareCallHierarchy":
		return false, s.prepareCallHierarchy(request.ID, request.Params)
	case "callHierarchy/incomingCalls":
		return false, s.incomingCalls(request.ID, request.Params)
	case "callHierarchy/outgoingCalls":
		return false, s.outgoingCalls(request.ID, request.Params)
	case "textDocument/completion":
		return false, s.completion(request.ID, request.Params)
	case "completionItem/resolve":
		return false, s.resolveCompletion(request.ID, request.Params)
	case "textDocument/documentSymbol":
		return false, s.documentSymbols(request.ID, request.Params)
	case "textDocument/diagnostic":
		s.handleAsync(request, func(ctx context.Context) error {
			return s.documentDiagnostics(ctx, request.ID, request.Params)
		})
		return false, nil
	case "textDocument/documentHighlight":
		return false, s.documentHighlights(request.ID, request.Params)
	case "textDocument/definition":
		return false, s.definition(request.ID, request.Params)
	case "textDocument/foldingRange":
		return false, s.foldingRanges(request.ID, request.Params)
	case "textDocument/inlayHint":
		return false, s.inlayHints(request.ID, request.Params)
	case "textDocument/documentColor":
		return false, s.documentColors(request.ID, request.Params)
	case "textDocument/colorPresentation":
		return false, s.colorPresentation(request.ID, request.Params)
	case "textDocument/hover":
		return false, s.hover(request.ID, request.Params)
	case "textDocument/references":
		return false, s.references(request.ID, request.Params)
	case "textDocument/prepareRename":
		return false, s.prepareRename(request.ID, request.Params)
	case "textDocument/rename":
		return false, s.rename(request.ID, request.Params)
	case "textDocument/semanticTokens/full":
		return false, s.semanticTokens(request.ID, request.Params)
	case "textDocument/selectionRange":
		return false, s.selectionRanges(request.ID, request.Params)
	case "textDocument/signatureHelp":
		return false, s.signatureHelp(request.ID, request.Params)
	case "textDocument/formatting":
		return false, s.formatting(request.ID, request.Params)
	case "textDocument/rangeFormatting":
		return false, s.rangeFormatting(request.ID, request.Params)
	case "textDocument/onTypeFormatting":
		return false, s.onTypeFormatting(request.ID, request.Params)
	default:
		if len(request.ID) == 0 || bytes.Equal(request.ID, []byte("null")) {
			return false, nil
		}
		return false, s.respondError(request.ID, -32601, "method not found")
	}
}

func (s *server) handleAsync(request message, handle func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background())
	key := string(request.ID)
	s.mu.Lock()
	if s.requestCancels == nil {
		s.requestCancels = make(map[string]context.CancelFunc)
	}
	s.requestCancels[key] = cancel
	s.mu.Unlock()
	s.requestWorkers.Go(func() {
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.requestCancels, key)
			s.mu.Unlock()
		}()
		if err := handle(ctx); err != nil {
			code := -32602
			if errors.Is(err, context.Canceled) {
				code = -32800
			}
			if responseErr := s.respondError(request.ID, code, err.Error()); responseErr != nil {
				fmt.Fprintf(os.Stderr, "pawnlsp: %s: %v\n", request.Method, errors.Join(err, responseErr))
			}
		}
	})
}

func (s *server) cancelRequest(raw json.RawMessage) {
	var params struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	s.mu.Lock()
	cancel := s.requestCancels[string(params.ID)]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *server) cancelProgress(raw json.RawMessage) {
	var params struct {
		Token json.RawMessage `json:"token"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	s.mu.Lock()
	cancel := s.progressCancels[string(params.Token)]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *server) didChangeConfiguration(raw json.RawMessage) error {
	var params struct {
		Settings struct {
			Pawn struct {
				IncludePaths []string `json:"includePaths"`
			} `json:"pawn"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	return s.updateManagedIncludeRoots(params.Settings.Pawn.IncludePaths)
}

func (s *server) didChangeManagedTools(raw json.RawMessage) error {
	var params struct {
		ProtocolVersion     int      `json:"protocolVersion"`
		ManagedIncludeRoots []string `json:"managedIncludeRoots"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	if params.ProtocolVersion != 1 {
		return fmt.Errorf("unsupported PawnKit editor protocol version %d", params.ProtocolVersion)
	}
	return s.updateManagedIncludeRoots(params.ManagedIncludeRoots)
}

func (s *server) updateManagedIncludeRoots(values []string) error {
	roots, err := cleanManagedIncludeRoots(values)
	if err != nil {
		return err
	}
	if slices.Equal(roots, s.managedRoots) {
		return nil
	}
	s.managedRoots = roots
	return s.reloadProjects()
}

func (s *server) didOpen(raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
			Text    string `json:"text"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	path, err := uriPath(params.TextDocument.URI)
	if err != nil {
		return err
	}
	includes, profile, root, entry := loadProjectContext(path, s.managedRoots...)
	names := s.names
	candidates := includeCandidates(includes)
	if active := s.activeProjectDocument(path); active != nil {
		includes = active.Includes
		candidates = active.Candidates
		names = active.Names
		root = active.Root
		entry = active.Entry
	} else if resolver, ok := names.(apiNameResolver); ok {
		resolver.profile = profile
		names = resolver
	}
	text := []byte(params.TextDocument.Text)
	buffer := coresource.NewTextBuffer(text)
	doc := &document{
		URI: params.TextDocument.URI, Path: path, Root: root, Entry: entry, Text: text,
		Buffer: buffer, Index: coresource.NewBufferedLineIndex(buffer),
		Version: params.TextDocument.Version, Includes: includes, Candidates: candidates, Names: names,
		ready: make(chan struct{}), analysisReady: make(chan struct{}),
		Revision: s.projectRevision,
	}
	if previous := s.document(doc.URI); previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	if s.snapshot == nil {
		s.snapshot = query.New()
	}
	s.snapshot, _ = s.snapshot.UpdateOwned(query.Document{
		URI: coresource.URI(doc.URI), Buffer: doc.Buffer, Version: int64(doc.Version),
	})
	s.mu.Lock()
	s.documents[doc.URI] = doc
	s.mu.Unlock()
	s.refreshWorkspaceIndex(doc)
	s.schedulePublish(doc, s.snapshot)
	return nil
}

func (s *server) activeProjectDocument(path string) *document {
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected *document
	for _, doc := range s.documents {
		if doc.Root == "" || doc.Entry == "" {
			continue
		}
		relative, err := filepath.Rel(doc.Root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		index := s.workspaces[doc.Root]
		if !ignoredWorkspacePath(relative) && !workspaceGraphContains(index, path) {
			continue
		}
		if selected == nil || len(doc.Root) > len(selected.Root) {
			selected = doc
		}
	}
	return selected
}

func workspaceGraphContains(index *workspaceIndex, path string) bool {
	if index == nil || index.graph == nil || index.graph.Preprocess == nil {
		return false
	}
	target := workspacePathKey(path)
	for _, file := range index.graph.Preprocess.Files {
		uri := coresource.URI(file.URI)
		filename, err := uri.Filename()
		if err == nil && workspacePathKey(filename) == target {
			return true
		}
	}
	return false
}

func (s *server) didChange(raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Range *lspRange `json:"range"`
			Text  string    `json:"text"`
		} `json:"contentChanges"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.document(params.TextDocument.URI)
	if doc == nil || len(params.ContentChanges) == 0 {
		return nil
	}
	if params.TextDocument.Version <= doc.Version {
		return nil
	}
	if doc.cancel != nil {
		doc.cancel()
	}
	index := doc.lineIndex()
	if index.TextBuffer() == nil {
		index = coresource.NewBufferedLineIndex(coresource.NewTextBuffer(doc.text()))
	}
	for _, change := range params.ContentChanges {
		if change.Range == nil {
			buffer := coresource.NewTextBuffer([]byte(change.Text))
			index = coresource.NewBufferedLineIndex(buffer)
			continue
		}
		start, err := index.Offset(coresource.Position{
			Line: change.Range.Start.Line, Character: change.Range.Start.Character,
		}, coresource.UTF16)
		if err != nil {
			return fmt.Errorf("invalid change range start: %w", err)
		}
		end, err := index.Offset(coresource.Position{
			Line: change.Range.End.Line, Character: change.Range.End.Character,
		}, coresource.UTF16)
		if err != nil {
			return fmt.Errorf("invalid change range end: %w", err)
		}
		if end < start {
			return errors.New("invalid change range: end precedes start")
		}
		nextIndex, err := index.Apply(start, end, change.Text)
		if err != nil {
			return fmt.Errorf("apply change: %w", err)
		}
		index = nextIndex
	}
	next := &document{
		URI: doc.URI, Path: doc.Path, Root: doc.Root, Entry: doc.Entry, Buffer: index.TextBuffer(), Index: index,
		Version: params.TextDocument.Version, Includes: doc.Includes, Candidates: doc.Candidates, Names: doc.Names,
		ready: make(chan struct{}), analysisReady: make(chan struct{}),
		Revision: doc.Revision,
	}
	var accepted bool
	s.snapshot, accepted = s.snapshot.UpdateOwned(query.Document{
		URI: coresource.URI(next.URI), Buffer: next.Buffer, Version: int64(next.Version),
	})
	if !accepted {
		return nil
	}
	s.mu.Lock()
	s.documents[next.URI] = next
	s.mu.Unlock()
	if next.Entry != "" {
		s.restartWorkspaceIndex(next)
	}
	s.schedulePublishAfter(next, s.snapshot, documentPublishDebounce)
	return nil
}

func (s *server) didClose(raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.document(params.TextDocument.URI)
	if doc != nil && doc.cancel != nil {
		doc.cancel()
	}
	s.mu.Lock()
	delete(s.documents, params.TextDocument.URI)
	inUse := false
	if doc != nil {
		for _, current := range s.documents {
			if current.Root == doc.Root {
				inUse = true
				break
			}
		}
	}
	if doc != nil && !inUse {
		if index := s.workspaces[doc.Root]; index != nil && index.cancel != nil {
			index.cancel()
		}
		delete(s.workspaces, doc.Root)
	}
	s.mu.Unlock()
	if doc == nil || !inUse {
		s.requestDiagnosticRefresh()
		return nil
	}
	s.restartWorkspaceIndex(doc)
	return nil
}

func (s *server) reloadProjects() error {
	s.parseCache.InvalidateFiles()
	s.mu.Lock()
	documents := make([]*document, 0, len(s.documents))
	for _, doc := range s.documents {
		documents = append(documents, doc)
		if doc.cancel != nil {
			doc.cancel()
		}
	}
	s.projectRevision++
	for _, index := range s.workspaces {
		if index.cancel != nil {
			index.cancel()
		}
	}
	s.workspaces = make(map[string]*workspaceIndex)
	revision := s.projectRevision
	s.mu.Unlock()

	for _, doc := range documents {
		includes, profile, root, entry := loadProjectContext(doc.Path, s.managedRoots...)
		names := s.names
		if resolver, ok := names.(apiNameResolver); ok {
			resolver.profile = profile
			names = resolver
		}
		next := &document{
			URI: doc.URI, Path: doc.Path, Root: root, Entry: entry, Text: doc.Text, Buffer: doc.Buffer, Index: doc.Index, Version: doc.Version,
			Includes: includes, Candidates: includeCandidates(includes), Names: names, Revision: revision,
			ready: make(chan struct{}), analysisReady: make(chan struct{}),
		}
		s.mu.Lock()
		if s.documents[doc.URI] == doc {
			s.documents[doc.URI] = next
			s.mu.Unlock()
			s.schedulePublish(next, s.snapshot)
			s.startWorkspaceIndex(next)
		} else {
			s.mu.Unlock()
		}
	}
	return nil
}

func (d *document) lineIndex() *coresource.LineIndex {
	if d.Index != nil {
		return d.Index
	}
	if d.Buffer != nil {
		return coresource.NewBufferedLineIndex(d.Buffer)
	}
	return coresource.NewLineIndexBytes(d.Text)
}

func (d *document) text() []byte {
	if d == nil {
		return nil
	}
	if d.Text != nil {
		return d.Text
	}
	if d.Buffer != nil {
		return d.Buffer.Bytes()
	}
	return nil
}

func (s *server) schedulePublish(doc *document, snapshot *query.Snapshot) {
	s.schedulePublishAfter(doc, snapshot, 0)
}

func (s *server) schedulePublishAfter(doc *document, snapshot *query.Snapshot, delay time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	doc.cancel = cancel
	request := &publishRequest{doc: doc, snapshot: snapshot, delay: delay, ctx: ctx, cancel: cancel}

	s.mu.Lock()
	if s.publishes == nil {
		s.publishes = make(map[string]*publishQueue)
	}
	if queue := s.publishes[doc.URI]; queue != nil {
		queue.active.cancel()
		if queue.pending != nil {
			queue.pending.cancel()
			queue.pending.doc.markAnalysisReady()
			queue.pending.doc.markFullReady()
		}
		queue.pending = request
		s.mu.Unlock()
		return
	}
	s.publishes[doc.URI] = &publishQueue{active: request}
	s.mu.Unlock()

	s.workers.Go(func() {
		defer cancel()
		for current := request; current != nil; {
			s.runPublish(current)
			current.cancel()

			s.mu.Lock()
			queue := s.publishes[doc.URI]
			if queue == nil || queue.pending == nil {
				delete(s.publishes, doc.URI)
				s.mu.Unlock()
				return
			}
			current = queue.pending
			queue.active = current
			queue.pending = nil
			s.mu.Unlock()
		}
	})
}

func (s *server) runPublish(request *publishRequest) {
	defer request.cancel()
	defer request.doc.markAnalysisReady()
	defer request.doc.markFullReady()
	if request.delay > 0 {
		timer := time.NewTimer(request.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-request.ctx.Done():
			return
		}
	}
	if err := s.publish(request.ctx, request.doc, request.snapshot); err != nil {
		return
	}
	request.doc.markFullReady()
	if s.document(request.doc.URI) == request.doc {
		s.requestDiagnosticRefresh()
	}
}

func (s *server) publish(ctx context.Context, doc *document, snapshot *query.Snapshot) error {
	var trace func(analysis.TraceEvent)
	if s.analysisTrace != nil {
		trace = func(event analysis.TraceEvent) {
			s.analysisTrace(doc.URI, doc.Version, event)
		}
	}
	var shared *analysis.Result
	var analysisErr error
	if workspacePathKey(doc.Path) == workspacePathKey(doc.Entry) {
		shared, analysisErr = s.workspaceGraphIfReady(doc)
	}
	if shared == nil && analysisErr == nil {
		shared, analysisErr = snapshot.Analyze(ctx, coresource.URI(doc.URI), analysis.Options{
			URI: coresource.URI(doc.URI), Includes: doc.Includes, Names: doc.Names, RetainExpanded: true,
			MaxOutputTokens:          analysisOutputTokenLimit,
			Revision:                 fmt.Sprintf("%s:%T:%T:%d", doc.Path, doc.Includes, doc.Names, doc.Revision),
			TokenCache:               s.tokenCache,
			ReuseCompatibleExpansion: true,
			Trace:                    trace,
		})
	}
	if analysisErr != nil {
		return analysisErr
	}
	if ctx.Err() != nil || s.document(doc.URI) != doc {
		return ctx.Err()
	}
	doc.Analysis = shared
	doc.markAnalysisReady()
	lintFn := s.lint
	if lintFn == nil {
		lintFn = lintDocument
	}
	diagnostics, err := lintFn(ctx, doc, s.parseCache, shared)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		diagnostics = []diagnostic.Diagnostic{{RuleID: "configuration", Severity: diagnostic.SeverityError, Message: err.Error(), Filename: doc.Path}}
	}
	diagnostics = reconcileDiagnostics(diagnostics, shared)
	doc.Diagnostics = diagnostics
	if ctx.Err() != nil || s.document(doc.URI) != doc {
		return ctx.Err()
	}
	return nil
}

func (d *document) markAnalysisReady() {
	if d != nil && d.analysisReady != nil {
		d.analysisOnce.Do(func() { close(d.analysisReady) })
	}
}

func (d *document) fullReady() bool {
	if d == nil || d.ready == nil {
		return true
	}
	select {
	case <-d.ready:
		return true
	default:
		return false
	}
}

func (d *document) markFullReady() {
	if d != nil && d.ready != nil {
		d.fullOnce.Do(func() { close(d.ready) })
	}
}

func reconcileDiagnostics(items []diagnostic.Diagnostic, shared *analysis.Result) []diagnostic.Diagnostic {
	missing := make(map[[2]int]bool)
	for _, item := range shared.Diagnostics {
		if item.Code == string(preprocess.CodeIncludeNotFound) && item.Primary.File == shared.File {
			missing[[2]int{int(item.Primary.Start), int(item.Primary.End)}] = true
		}
	}
	macros := newMacroInvocationIndex(shared)
	result := items[:0]
	for _, item := range items {
		key := [2]int{item.Range.Start.Offset, item.Range.End.Offset}
		resolvedInclude := item.RuleID == "missing-include" && !missing[key]
		macroDeclaration := item.RuleID == "duplicate-function-definition" && macros.contains(key[0], key[1])
		if !resolvedInclude && !macroDeclaration {
			result = append(result, item)
		}
	}
	return result
}

func (s *server) cancelDocuments() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, doc := range s.documents {
		if doc.cancel != nil {
			doc.cancel()
		}
	}
	for _, index := range s.workspaces {
		if index.cancel != nil {
			index.cancel()
		}
	}
}

func (s *server) document(uri string) *document {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.documents[uri]
}

func (s *server) readyDocument(uri string) *document {
	for {
		doc := s.document(uri)
		if doc == nil || doc.analysisReady == nil {
			return doc
		}
		<-doc.analysisReady
		if s.document(uri) == doc {
			return doc
		}
	}
}

func (s *server) fullReadyDocument(uri string) *document {
	for {
		doc := s.document(uri)
		if doc == nil || doc.ready == nil {
			return doc
		}
		<-doc.ready
		if s.document(uri) == doc {
			return doc
		}
	}
}

func (s *server) documentSymbols(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	items := make([]map[string]any, 0)
	if doc != nil && doc.Analysis != nil && doc.Analysis.Symbols != nil {
		for _, item := range doc.Analysis.Symbols.Symbols {
			rng := offsetRange(doc.text(), int(item.Span.Start), int(item.Span.End))
			items = append(items, map[string]any{
				"name": item.Name, "kind": symbolKind(item.Kind),
				"range": rng, "selectionRange": rng,
			})
		}
	}
	return s.respond(id, items)
}

func symbolKind(kind symbol.Kind) int {
	switch kind {
	case symbol.KindEnum:
		return 10
	case symbol.KindFunction, symbol.KindPublic, symbol.KindNative, symbol.KindForward, symbol.KindStock:
		return 12
	case symbol.KindConstant:
		return 14
	default:
		return 13
	}
}

func (s *server) definition(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position `json:"position"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil || doc.Analysis == nil || doc.Analysis.Symbols == nil {
		return s.respond(id, nil)
	}
	index := doc.lineIndex()
	offset, err := index.Offset(coresource.Position{
		Line: params.Position.Line, Character: params.Position.Character,
	}, coresource.UTF16)
	if err != nil {
		return s.respond(id, nil)
	}
	if include, ok := includeAt(doc.Analysis, int(offset)); ok {
		if !include.Resolved || include.ResolvedURI == "" {
			return s.respond(id, nil)
		}
		return s.respond(id, map[string]any{
			"uri":   include.ResolvedURI,
			"range": offsetRange(nil, 0, 0),
		})
	}
	name, _, _ := identifierAt(doc.text(), int(offset))
	for _, table := range navigationTables(doc.Analysis) {
		for _, ref := range table.References {
			if ref.Span.File != doc.Analysis.File || !ref.Span.Contains(offset) {
				continue
			}
			decl, ok := table.Symbol(ref.Resolved)
			if !ok || decl.Name != name {
				decl, ok = uniqueSymbolByName(table, name)
			}
			if !ok {
				break
			}
			if _, span, found := localDeclarationSource(doc.Analysis, decl); found {
				return s.respond(id, analysisLocation(doc, span))
			}
		}
	}
	occurrences := s.workspaceOccurrences(name)
	if workspaceDeclarationCount(occurrences) == 1 {
		for _, occurrence := range occurrences {
			if occurrence.declaration {
				return s.respond(id, map[string]any{
					"uri":   occurrence.uri.String(),
					"range": offsetRange(occurrence.text, int(occurrence.span.Start), int(occurrence.span.End)),
				})
			}
		}
	}
	return s.respond(id, nil)
}

func (s *server) hover(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position `json:"position"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, nil)
	}
	if include, ok := includeAt(doc.Analysis, int(offset)); ok {
		start, end := includePathRange(doc.text(), include)
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": includeHover(include)},
			"range":    offsetRange(doc.text(), start, end),
		})
	}
	name, start, end := identifierAt(doc.text(), int(offset))
	item, _, ok := symbolAtAnalysis(doc.Analysis, doc.Analysis.File, offset)
	if ok && item.Name == name {
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": hoverText(doc, item)},
			"range":    offsetRange(doc.text(), start, end),
		})
	}
	if macro, ok := doc.Analysis.Preprocess.Macros[name]; ok {
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": macroHover(doc.Analysis.Preprocess, macro)},
			"range":    offsetRange(doc.text(), start, end),
		})
	}
	if contents, ok := declarationHoverByName(doc.Analysis, name); ok {
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": contents},
			"range":    offsetRange(doc.text(), start, end),
		})
	}
	occurrences := s.workspaceOccurrences(name)
	if workspaceDeclarationCount(occurrences) == 1 {
		for _, occurrence := range occurrences {
			if occurrence.declaration {
				contents := "```pawn\n" + declarationText(occurrence.text, occurrence.span) + "\n```"
				if documentation := declarationDocumentation(occurrence.text, occurrence.span); documentation != "" {
					contents += "\n\n" + documentation
				}
				return s.respond(id, map[string]any{
					"contents": map[string]any{"kind": "markdown", "value": contents},
					"range":    offsetRange(doc.text(), start, end),
				})
			}
		}
	}
	entry, ok := apiEntry(doc.Names, name)
	if !ok {
		return s.respond(id, nil)
	}
	return s.respond(id, map[string]any{
		"contents": map[string]any{"kind": "markdown", "value": apiHover(entry)},
		"range":    offsetRange(doc.text(), start, end),
	})
}

func declarationHoverByName(result *analysis.Result, name string) (string, bool) {
	if result == nil || result.Preprocess == nil || name == "" {
		return "", false
	}
	kinds := []symbol.Kind{
		symbol.KindNative, symbol.KindForward, symbol.KindPublic, symbol.KindStock, symbol.KindFunction,
	}
	for _, kind := range kinds {
		for _, file := range result.Preprocess.Files {
			span, ok := findDeclarationSpan(file.Content, symbol.Symbol{Name: name, Kind: kind})
			if !ok {
				continue
			}
			contents := "```pawn\n" + declarationText(file.Content, span) + "\n```"
			if documentation := declarationDocumentation(file.Content, span); documentation != "" {
				contents += "\n\n" + documentation
			}
			return contents, true
		}
	}
	return "", false
}

func macroHover(result *preprocess.Result, macro preprocess.Macro) string {
	declaration := macroSignature(macro)
	documentation := ""
	if result != nil && int(macro.File) < len(result.Files) {
		file := result.Files[macro.File].Content
		if source := macroDefinition(file, macro.DefSpan); source != "" {
			declaration = source
		}
		documentation = declarationDocumentation(file, coresource.Span{Start: coresource.Offset(macro.DefSpan.Start), End: coresource.Offset(macro.DefSpan.End)})
	}
	text := "```pawn\n" + declaration + "\n```"
	if documentation != "" {
		text += "\n\n" + documentation
	}
	return text
}

func macroDefinition(text []byte, span preprocess.ByteRange) string {
	if span.Start < 0 || span.End <= span.Start || span.End > len(text) {
		return ""
	}
	start := bytes.LastIndexByte(text[:span.Start], '\n') + 1
	end := span.End
	limit := min(len(text), start+512)
	for end < limit {
		newline := bytes.IndexByte(text[end:limit], '\n')
		if newline < 0 {
			end = limit
			break
		}
		end += newline
		if !bytes.HasSuffix(bytes.TrimSpace(text[start:end]), []byte{'\\'}) {
			break
		}
		end++
	}
	return strings.TrimSpace(string(text[start:end]))
}

func includeAt(result *analysis.Result, offset int) (preprocess.Include, bool) {
	if result == nil || result.Preprocess == nil {
		return preprocess.Include{}, false
	}
	for _, include := range result.Preprocess.Includes {
		if include.File == 0 && offset >= include.DirectiveSpan.Start && offset < include.DirectiveSpan.End {
			return include, true
		}
	}
	return preprocess.Include{}, false
}

func includePathRange(text []byte, include preprocess.Include) (int, int) {
	start, end := include.DirectiveSpan.Start, include.DirectiveSpan.End
	if start < 0 || end > len(text) || start >= end {
		return start, end
	}
	if path := bytes.Index(text[start:end], []byte(include.Path)); path >= 0 {
		start += path
		return start, start + len(include.Path)
	}
	return start, end
}

func includeHover(include preprocess.Include) string {
	opening, closing := "<", ">"
	if !include.Angle {
		opening, closing = `"`, `"`
	}
	text := "```pawn\n#include " + opening + include.Path + closing + "\n```"
	if !include.Resolved || include.ResolvedURI == "" {
		return text + "\n\nInclude not found."
	}
	path := include.ResolvedURI
	if filename, err := coresource.URI(include.ResolvedURI).Filename(); err == nil {
		path = filename
	}
	return text + "\n\nResolved file: `" + path + "`"
}

func identifierAt(text []byte, offset int) (string, int, int) {
	if offset < 0 || offset > len(text) {
		return "", 0, 0
	}
	start, end := offset, offset
	for start > 0 && identifierByte(text[start-1]) {
		start--
	}
	for end < len(text) && identifierByte(text[end]) {
		end++
	}
	return string(text[start:end]), start, end
}

func identifierByte(value byte) bool {
	return value == '_' || value == '@' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func apiEntry(names sema.Resolver, name string) (pawnapi.Entry, bool) {
	resolver, ok := names.(apiNameResolver)
	if !ok || resolver.index == nil || name == "" {
		return pawnapi.Entry{}, false
	}
	for _, entry := range resolver.index.ByName(name) {
		if resolver.available(entry) {
			return entry, true
		}
	}
	return pawnapi.Entry{}, false
}

func hoverText(doc *document, item symbol.Symbol) string {
	if resolver, ok := doc.Names.(apiNameResolver); ok && resolver.index != nil {
		for _, entry := range resolver.index.ByName(item.Name) {
			if resolver.available(entry) {
				return apiHover(entry)
			}
		}
	}
	if declaration := localDeclaration(doc.Analysis, item); declaration != "" {
		parts := []string{"```pawn\n" + declaration + "\n```"}
		if documentation := localDocumentation(doc.Analysis, item); documentation != "" {
			parts = append(parts, documentation)
		}
		return strings.Join(parts, "\n\n")
	}
	return "```pawn\n" + symbolSummary(item) + "\n```"
}

func localDocumentation(result *analysis.Result, item symbol.Symbol) string {
	text, span, ok := localDeclarationSource(result, item)
	if !ok {
		return ""
	}
	return declarationDocumentation(text, span)
}

func declarationDocumentation(text []byte, span coresource.Span) string {
	start := int(span.Start)
	if start <= 0 || start > len(text) {
		return ""
	}
	lineStart := bytes.LastIndexByte(text[:start], '\n') + 1
	lines := strings.Split(string(text[:lineStart]), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		return ""
	}

	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.HasPrefix(last, "//") {
		first := len(lines) - 1
		for first > 0 && strings.HasPrefix(strings.TrimSpace(lines[first-1]), "//") {
			first--
		}
		parts := make([]string, 0, len(lines)-first)
		for _, line := range lines[first:] {
			parts = append(parts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//")))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if !strings.HasSuffix(last, "*/") {
		return ""
	}
	first := len(lines) - 1
	for first > 0 && !strings.Contains(lines[first], "/*") {
		first--
	}
	if !strings.Contains(lines[first], "/*") {
		return ""
	}
	comment := strings.Join(lines[first:], "\n")
	comment = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(comment), "/*"), "*/"))
	parts := strings.Split(comment, "\n")
	for index, line := range parts {
		parts[index] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func localDeclaration(result *analysis.Result, item symbol.Symbol) string {
	text, span, ok := localDeclarationSource(result, item)
	if !ok {
		return ""
	}
	if item.Kind == symbol.KindConstant {
		return declarationLine(text, span)
	}
	return declarationText(text, span)
}

func localDeclarationSource(result *analysis.Result, item symbol.Symbol) ([]byte, coresource.Span, bool) {
	if result == nil || result.Registry == nil || result.Preprocess == nil {
		return nil, coresource.Span{}, false
	}
	uri, ok := result.Registry.URI(item.Span.File)
	if ok {
		for _, file := range result.Preprocess.Files {
			if file.URI == uri.String() && spanDeclaresSymbol(file.Content, item) {
				return file.Content, item.Span, true
			}
		}
	}
	for fileIndex, file := range result.Preprocess.Files {
		if span, found := findDeclarationSpan(file.Content, item); found {
			fileID, exists := result.Registry.Lookup(coresource.URI(file.URI))
			if !exists && fileIndex == 0 {
				fileID = result.File
			}
			span.File = fileID
			return file.Content, span, true
		}
	}
	return nil, coresource.Span{}, false
}

func spanDeclaresSymbol(text []byte, item symbol.Symbol) bool {
	start, end := int(item.Span.Start), int(item.Span.End)
	if start < 0 || end > len(text) || start >= end || string(text[start:end]) != item.Name {
		return false
	}
	if !item.Kind.IsCallable() {
		return true
	}
	lineStart := bytes.LastIndexByte(text[:start], '\n') + 1
	return declarationPrefixMatches(item.Kind, strings.TrimSpace(string(text[lineStart:start])))
}

func findDeclarationSpan(text []byte, item symbol.Symbol) (coresource.Span, bool) {
	for offset := 0; offset < len(text); {
		index := bytes.Index(text[offset:], []byte(item.Name))
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(item.Name)
		offset = end
		if !identifierBoundaries(text, start, end) {
			continue
		}
		lineStart := bytes.LastIndexByte(text[:start], '\n') + 1
		prefix := strings.TrimSpace(string(text[lineStart:start]))
		if declarationPrefixMatches(item.Kind, prefix) {
			return coresource.Span{Start: coresource.Offset(start), End: coresource.Offset(end)}, true
		}
	}
	return coresource.Span{}, false
}

func identifierBoundaries(text []byte, start, end int) bool {
	return (start == 0 || !identifierByte(text[start-1])) &&
		(end == len(text) || !identifierByte(text[end]))
}

func declarationPrefixMatches(kind symbol.Kind, prefix string) bool {
	switch kind {
	case symbol.KindNative:
		return declarationKeywordPrefix(prefix, "native")
	case symbol.KindForward:
		return declarationKeywordPrefix(prefix, "forward")
	case symbol.KindPublic:
		return declarationKeywordPrefix(prefix, "public")
	case symbol.KindStock:
		return declarationKeywordPrefix(prefix, "stock")
	case symbol.KindConstant:
		return declarationKeywordPrefix(prefix, "const") || declarationKeywordPrefix(prefix, "#define")
	case symbol.KindFunction:
		return !strings.ContainsAny(prefix, ";{}")
	default:
		return false
	}
}

func declarationKeywordPrefix(prefix, keyword string) bool {
	return prefix == keyword || strings.HasPrefix(prefix, keyword+" ")
}

func declarationLine(text []byte, span coresource.Span) string {
	start := int(span.Start)
	if start < 0 || start >= len(text) {
		return ""
	}
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	end := bytes.IndexByte(text[start:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += start
	}
	return strings.TrimSuffix(strings.TrimSpace(string(text[start:end])), ",")
}

func declarationText(text []byte, span coresource.Span) string {
	start := int(span.Start)
	if start < 0 || start >= len(text) {
		return ""
	}
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	end := int(span.End)
	limit := min(len(text), end+512)
	parentheses, brackets := 0, 0
	for end < limit {
		switch text[end] {
		case '(':
			parentheses++
		case ')':
			if parentheses > 0 {
				parentheses--
			}
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case '{':
			if parentheses == 0 && brackets == 0 {
				return strings.Join(strings.Fields(string(text[start:end])), " ")
			}
		case ';':
			if parentheses == 0 && brackets == 0 {
				return strings.Join(strings.Fields(string(text[start:end])), " ")
			}
		}
		end++
	}
	return strings.Join(strings.Fields(string(text[start:end])), " ")
}

func apiHover(entry pawnapi.Entry) string {
	parts := []string{"```pawn\n" + apiDeclaration(entry) + "\n```"}
	if entry.Deprecated != nil {
		note := "Deprecated since " + entry.Deprecated.Since + "."
		if entry.Deprecated.Reason != "" {
			note += " " + entry.Deprecated.Reason
		}
		if entry.Deprecated.Replacement != "" {
			replacement := entry.Deprecated.Replacement
			if _, name, ok := strings.Cut(replacement, ":"); ok {
				replacement = name
			}
			note += " Use `" + replacement + "` instead."
		}
		parts = append(parts, "> **Deprecated:** "+note)
	}
	if documentation := apiDocumentation(entry); documentation != "" {
		parts = append(parts, documentation)
	}
	return strings.Join(parts, "\n\n")
}

func apiDeclaration(entry pawnapi.Entry) string {
	if entry.Signature == nil {
		if entry.Value != nil {
			return fmt.Sprintf("%s %s = %s", entry.Kind, entry.Name, entry.Value.String())
		}
		return string(entry.Kind) + " " + entry.Name
	}
	parameters := make([]string, 0, len(entry.Signature.Parameters))
	for _, parameter := range entry.Signature.Parameters {
		value := parameter.Name
		if parameter.Tag != "" {
			value = parameter.Tag + ":" + value
		}
		if parameter.Reference {
			value = "&" + value
		}
		var dimensions strings.Builder
		for _, size := range parameter.ArrayDimensions {
			if size > 0 {
				dimensions.WriteString("[")
				dimensions.WriteString(strconv.Itoa(size))
				dimensions.WriteString("]")
			} else {
				dimensions.WriteString("[]")
			}
		}
		value += dimensions.String()
		if parameter.Variadic {
			value += "..."
		}
		if parameter.Default != nil {
			value += " = " + parameter.Default.String()
		}
		if parameter.Const {
			value = "const " + value
		}
		parameters = append(parameters, value)
	}
	name := entry.Name
	if entry.Signature.ReturnTag != "" {
		name = entry.Signature.ReturnTag + ":" + name
	}
	return fmt.Sprintf("%s %s(%s)", entry.Kind, name, strings.Join(parameters, ", "))
}

func documentOffset(doc *document, pos position) (coresource.Offset, bool) {
	if doc == nil || doc.Analysis == nil || doc.Analysis.Symbols == nil {
		return 0, false
	}
	index := doc.lineIndex()
	offset, err := index.Offset(coresource.Position{Line: pos.Line, Character: pos.Character}, coresource.UTF16)
	return offset, err == nil
}

func navigationTables(result *analysis.Result) []*symbol.Table {
	if result == nil {
		return nil
	}
	tables := make([]*symbol.Table, 0, 2)
	if result.Symbols != nil {
		tables = append(tables, result.Symbols)
	}
	if result.ExpandedSymbols != nil && result.ExpandedSymbols != result.Symbols {
		tables = append(tables, result.ExpandedSymbols)
	}
	return tables
}

func navigationTable(result *analysis.Result) *symbol.Table {
	if result == nil {
		return nil
	}
	if result.Symbols != nil {
		return result.Symbols
	}
	return result.ExpandedSymbols
}

func symbolAtAnalysis(result *analysis.Result, file coresource.FileID, offset coresource.Offset) (symbol.Symbol, *symbol.Table, bool) {
	for _, table := range navigationTables(result) {
		if item, ok := symbolAt(table, file, offset); ok {
			return item, table, true
		}
	}
	return symbol.Symbol{}, nil, false
}

func symbolAt(table *symbol.Table, file coresource.FileID, offset coresource.Offset) (symbol.Symbol, bool) {
	for _, ref := range table.References {
		if ref.Span.File != file || !ref.Span.Contains(offset) {
			continue
		}
		if ref.Resolved != 0 {
			if item, ok := table.Symbol(ref.Resolved); ok && item.Name == ref.Name {
				return item, true
			}
		}
		if item, ok := uniqueSymbolByName(table, ref.Name); ok {
			return item, true
		}
	}
	for _, item := range table.Symbols {
		if item.Span.File == file && item.Span.Contains(offset) {
			return item, true
		}
	}
	return symbol.Symbol{}, false
}

func uniqueSymbolByName(table *symbol.Table, name string) (symbol.Symbol, bool) {
	var match symbol.Symbol
	for _, item := range table.Symbols {
		if item.Name != name || !item.Kind.IsCallable() {
			continue
		}
		if match.ID != 0 {
			return symbol.Symbol{}, false
		}
		match = item
	}
	return match, match.ID != 0
}

func symbolSummary(item symbol.Symbol) string {
	name := item.Name
	if item.Tag != "" {
		name = item.Tag + ":" + name
	}
	if !item.Kind.IsCallable() {
		return item.Kind.String() + " " + name
	}
	args := strconv.Itoa(item.MinArgs)
	if item.MaxArgs < 0 {
		args += "+"
	} else if item.MaxArgs != item.MinArgs {
		args = fmt.Sprintf("%d..%d", item.MinArgs, item.MaxArgs)
	}
	return fmt.Sprintf("%s %s (%s arguments)", item.Kind, name, args)
}

func (s *server) references(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position `json:"position"`
		Context  struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, []any{})
	}
	item, table, ok := symbolAtAnalysis(doc.Analysis, doc.Analysis.File, offset)
	name := ""
	global := false
	if ok {
		name = item.Name
		if scope, found := table.Scope(item.Scope); found {
			global = scope.Kind == symbol.ScopeFile
		}
	} else {
		name, _, _ = identifierAt(doc.text(), int(offset))
		global = name != ""
	}
	if global {
		occurrences := s.workspaceOccurrences(name)
		hasDeclaration := false
		for _, occurrence := range occurrences {
			hasDeclaration = hasDeclaration || occurrence.declaration
		}
		_, api := apiEntry(doc.Names, name)
		if hasDeclaration || api {
			locations := make([]map[string]any, 0, len(occurrences))
			for _, occurrence := range occurrences {
				if occurrence.declaration && !params.Context.IncludeDeclaration {
					continue
				}
				locations = append(locations, map[string]any{
					"uri":   occurrence.uri.String(),
					"range": offsetRange(occurrence.text, int(occurrence.span.Start), int(occurrence.span.End)),
				})
			}
			return s.respond(id, locations)
		}
	}
	if !ok {
		return s.respond(id, []any{})
	}
	locations := make([]map[string]any, 0)
	if params.Context.IncludeDeclaration {
		locations = append(locations, analysisLocation(doc, item.Span))
	}
	for _, ref := range table.References {
		if ref.Resolved == item.ID {
			locations = append(locations, analysisLocation(doc, ref.Span))
		}
	}
	return s.respond(id, locations)
}

func analysisLocation(doc *document, span coresource.Span) map[string]any {
	uri, text := spanDocument(doc, span)
	return map[string]any{
		"uri":   uri,
		"range": offsetRange(text, int(span.Start), int(span.End)),
	}
}

func spanDocument(doc *document, span coresource.Span) (string, []byte) {
	uri, text := doc.URI, doc.text()
	if span.File != doc.Analysis.File {
		if resolved, ok := doc.Analysis.Registry.URI(span.File); ok {
			uri = resolved.String()
			for _, file := range doc.Analysis.Preprocess.Files {
				if file.URI == uri {
					text = file.Content
					break
				}
			}
		}
	}
	return uri, text
}

func dedupeDiagnostics(items []lspDiagnostic) []lspDiagnostic {
	if len(items) == 0 {
		return []lspDiagnostic{}
	}
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, item := range items {
		key := fmt.Sprintf("%s\x00%s\x00%d:%d-%d:%d", item.Code, item.Message,
			item.Range.Start.Line, item.Range.Start.Character, item.Range.End.Line, item.Range.End.Character)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func lintDocument(ctx context.Context, doc *document, cache *lintproject.ParseCache, shared *analysis.Result) ([]diagnostic.Diagnostic, error) {
	return editor.DiagnoseContextWithCache(ctx, doc.Path, doc.text(), filepath.Dir(doc.Path), cache, shared)
}

func (s *server) codeActions(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range *lspRange `json:"range"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.fullReadyDocument(params.TextDocument.URI)
	actions := make([]map[string]any, 0)
	if doc != nil {
		seen := make(map[string]bool)
		for _, finding := range doc.Diagnostics {
			if params.Range != nil && !rangesOverlap(*params.Range, diagnosticRange(doc.text(), finding)) {
				continue
			}
			if s.rules == nil {
				continue
			}
			if _, known := s.rules.Lookup(finding.RuleID); !known {
				continue
			}
			if finding.Fix != nil && safeFix(s.rules, finding.RuleID) {
				edits := make([]textEdit, 0, len(finding.Fix.Edits))
				for _, edit := range finding.Fix.Edits {
					edits = append(edits, textEdit{Range: offsetRange(doc.text(), edit.Range.Start.Offset, edit.Range.End.Offset), NewText: edit.NewText})
				}
				action := map[string]any{
					"title":       finding.Fix.Description,
					"kind":        "quickfix",
					"isPreferred": true,
					"edit":        map[string]any{"changes": map[string]any{doc.URI: edits}},
				}
				appendCodeAction(&actions, seen, finding.Fix.Description, action)
			}
			suppressTitle := "Suppress " + finding.RuleID + " on this line"
			appendCodeAction(&actions, seen, suppressTitle, map[string]any{
				"title": suppressTitle,
				"kind":  "quickfix",
				"edit":  map[string]any{"changes": map[string]any{doc.URI: []textEdit{suppressionEdit(doc.text(), finding)}}},
			})
			explainTitle := "Explain " + finding.RuleID
			appendCodeAction(&actions, seen, explainTitle, map[string]any{
				"title":   explainTitle,
				"kind":    "quickfix",
				"command": map[string]any{"title": "Explain rule", "command": "pawn.openRuleDocumentation", "arguments": []string{finding.RuleID}},
			})
		}
	}
	return s.respond(id, actions)
}

func appendCodeAction(actions *[]map[string]any, seen map[string]bool, key string, action map[string]any) {
	if seen[key] {
		return
	}
	seen[key] = true
	*actions = append(*actions, action)
}

func rangesOverlap(left, right lspRange) bool {
	return comparePosition(left.Start, right.End) <= 0 && comparePosition(right.Start, left.End) <= 0
}

func comparePosition(left, right position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}

func suppressionEdit(source []byte, finding diagnostic.Diagnostic) textEdit {
	line := diagnosticRange(source, finding).Start.Line
	start := 0
	for current := 0; current < line && start < len(source); current++ {
		if next := bytes.IndexByte(source[start:], '\n'); next >= 0 {
			start += next + 1
		} else {
			start = len(source)
		}
	}
	end := start
	for end < len(source) && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	indent := string(source[start:end])
	point := lspRange{Start: position{Line: line}, End: position{Line: line}}
	return textEdit{Range: point, NewText: indent + "// pawnlint-disable-next-line " + finding.RuleID + "\n"}
}

func safeFix(registry *lint.Registrar, ruleID string) bool {
	if registry == nil {
		return false
	}
	metadata, ok := registry.Lookup(ruleID)
	return ok && metadata.Fixable && !metadata.UnsafeFix
}

func (s *server) formatting(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Options struct {
			TabSize      int  `json:"tabSize"`
			InsertSpaces bool `json:"insertSpaces"`
		} `json:"options"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.document(params.TextDocument.URI)
	if doc == nil {
		return s.respond(id, []textEdit{})
	}
	formatted, err := pawnfmt.Format(doc.text(), pawnfmt.Options{
		TabSize: params.Options.TabSize, UseTabs: !params.Options.InsertSpaces,
	})
	if err != nil {
		return s.respondError(id, -32603, err.Error())
	}
	if bytes.Equal(formatted, doc.text()) {
		return s.respond(id, []textEdit{})
	}
	return s.respond(id, []textEdit{{Range: offsetRange(doc.text(), 0, len(doc.text())), NewText: string(formatted)}})
}

func diagnosticRange(source []byte, finding diagnostic.Diagnostic) lspRange {
	return offsetRange(source, finding.Range.Start.Offset, finding.Range.End.Offset)
}

func offsetRange(source []byte, start, end int) lspRange {
	index := coresource.NewLineIndex(string(source))
	return offsetRangeWithIndex(index, start, end)
}

func offsetPosition(text []byte, offset int) position {
	index := coresource.NewLineIndex(string(text))
	return offsetPositionWithIndex(index, offset)
}

func offsetRangeWithIndex(index *coresource.LineIndex, start, end int) lspRange {
	return lspRange{Start: offsetPositionWithIndex(index, start), End: offsetPositionWithIndex(index, end)}
}

func offsetPositionWithIndex(index *coresource.LineIndex, offset int) position {
	content := index.Content()
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	for offset > 0 && !index.ValidOffset(coresource.Offset(offset)) {
		offset--
	}
	pos, _ := index.Position(coresource.Offset(offset), coresource.UTF16)
	return position{Line: pos.Line, Character: pos.Character}
}

func lspSeverity(severity diagnostic.Severity) int {
	switch severity {
	case diagnostic.SeverityError:
		return 1
	case diagnostic.SeverityWarning:
		return 2
	case diagnostic.SeverityHint:
		return 4
	default:
		return 3
	}
}

func coreLSPSeverity(severity corediagnostic.Severity) int {
	switch severity {
	case corediagnostic.SeverityError:
		return 1
	case corediagnostic.SeverityWarning:
		return 2
	case corediagnostic.SeverityHint:
		return 4
	default:
		return 3
	}
}

func uriPath(raw string) (string, error) {
	path, err := coresource.URI(raw).Filename()
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

const (
	maxFrameLength = 64 << 20
	maxHeaderLines = 100
)

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	ended := false
	for range maxHeaderLines {
		lineBytes, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, errors.New("header line is too long")
		}
		if err != nil {
			return nil, err
		}
		line := string(lineBytes)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			ended = true
			break
		}
		name, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if length >= 0 {
			return nil, errors.New("duplicate Content-Length")
		}
		length, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil || length < 0 || length > maxFrameLength {
			return nil, fmt.Errorf("invalid Content-Length %q", value)
		}
	}
	if !ended {
		return nil, errors.New("too many frame headers")
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func hasRequestID(id json.RawMessage) bool {
	return len(id) != 0 && !bytes.Equal(id, []byte("null"))
}

func (s *server) respond(id json.RawMessage, result any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *server) respondError(id json.RawMessage, code int, message string) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func (s *server) requestDiagnosticRefresh() {
	if s.out == nil {
		return
	}
	id := s.nextRequestID.Add(1)
	_ = s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": "workspace/diagnostic/refresh"})
}

func (s *server) write(value any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = s.out.Write(body)
	return err
}
