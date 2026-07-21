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
	lintrules "github.com/pawnkit/pawnlint/pkg/rules"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type document struct {
	URI         string
	Path        string
	Root        string
	Text        []byte
	Version     int
	Diagnostics []diagnostic.Diagnostic
	Includes    preprocess.IncludeResolver
	Names       sema.Resolver
	Analysis    *analysis.Result
	Revision    int64
	ready       chan struct{}
	cancel      context.CancelFunc
}

type server struct {
	in              *bufio.Reader
	out             io.Writer
	documents       map[string]*document
	names           sema.Resolver
	snapshot        *query.Snapshot
	shutdown        bool
	mu              sync.Mutex
	writeMu         sync.Mutex
	workers         sync.WaitGroup
	rules           *lint.Registrar
	includeRoots    []string
	workspaces      map[string]*workspaceIndex
	projectRevision int64
}

type apiNameResolver struct {
	index   *pawnapi.Index
	profile string
}

type projectIncludeResolver struct {
	resolver interface {
		Resolve(fromFile, spec string, quoted bool) (string, bool)
	}
	fsys fsx.FS
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

func loadProjectContext(path string, extraRoots ...string) (preprocess.IncludeResolver, string, string) {
	fsys := fsx.OS{}
	project, err := projectmodel.Load(coresource.NewRegistry(), fsys, path, projectmodel.Options{})
	if err != nil {
		return nil, "", filepath.Dir(path)
	}
	roots := append([]string{}, project.Paths().IncludeRoots...)
	roots = append(roots, extraRoots...)
	resolver := projectinclude.New(fsys, roots)
	return projectIncludeResolver{resolver: resolver, fsys: fsys}, project.Selection().ProfileID, project.Root()
}

func loadProjectIncludes(path string, extraRoots ...string) (preprocess.IncludeResolver, string) {
	resolver, profile, _ := loadProjectContext(path, extraRoots...)
	return resolver, profile
}

func cleanIncludeRoots(roots []string) []string {
	cleaned := make([]string, 0, len(roots))
	seen := make(map[string]bool)
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		if !seen[root] {
			seen[root] = true
			cleaned = append(cleaned, root)
		}
	}
	return cleaned
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
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Code     string   `json:"code"`
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

func Run(in io.Reader, out io.Writer) error {
	apiIndex, err := pawnapi.Load()
	if err != nil {
		return fmt.Errorf("load Pawn API metadata: %w", err)
	}
	s := &server{
		in: bufio.NewReader(in), out: out, documents: make(map[string]*document),
		names: apiNameResolver{index: apiIndex}, snapshot: query.New(), rules: lintrules.Default(),
		workspaces: make(map[string]*workspaceIndex),
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
	if s.shutdown && request.Method != "exit" {
		if len(request.ID) == 0 || bytes.Equal(request.ID, []byte("null")) {
			return false, nil
		}
		return false, s.respondError(request.ID, -32600, "server is shutting down")
	}
	switch request.Method {
	case "initialize":
		var params struct {
			InitializationOptions struct {
				IncludePaths []string `json:"includePaths"`
			} `json:"initializationOptions"`
		}
		if len(request.Params) != 0 {
			_ = json.Unmarshal(request.Params, &params)
		}
		s.includeRoots = cleanIncludeRoots(params.InitializationOptions.IncludePaths)
		s.projectRevision++
		return false, s.respond(request.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync": 1, "codeActionProvider": true,
				"callHierarchyProvider":  true,
				"diagnosticProvider":     map[string]any{"interFileDependencies": true, "workspaceDiagnostics": true},
				"completionProvider":     map[string]any{"triggerCharacters": []string{"@"}},
				"documentSymbolProvider": true, "definitionProvider": true,
				"documentHighlightProvider": true,
				"foldingRangeProvider":      true,
				"inlayHintProvider":         true,
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
		s.cancelDocuments()
		s.workers.Wait()
		return false, s.respond(request.ID, nil)
	case "exit":
		s.cancelDocuments()
		s.workers.Wait()
		return true, nil
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
	case "workspace/symbol":
		return false, s.workspaceSymbols(request.ID, request.Params)
	case "workspace/diagnostic":
		return false, s.workspaceDiagnostics(request.ID)
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
	case "textDocument/documentSymbol":
		return false, s.documentSymbols(request.ID, request.Params)
	case "textDocument/diagnostic":
		return false, s.documentDiagnostics(request.ID, request.Params)
	case "textDocument/documentHighlight":
		return false, s.documentHighlights(request.ID, request.Params)
	case "textDocument/definition":
		return false, s.definition(request.ID, request.Params)
	case "textDocument/foldingRange":
		return false, s.foldingRanges(request.ID, request.Params)
	case "textDocument/inlayHint":
		return false, s.inlayHints(request.ID, request.Params)
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
	roots := cleanIncludeRoots(params.Settings.Pawn.IncludePaths)
	if slices.Equal(roots, s.includeRoots) {
		return nil
	}
	s.includeRoots = roots
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
	includes, profile, root := loadProjectContext(path, s.includeRoots...)
	names := s.names
	if resolver, ok := names.(apiNameResolver); ok {
		resolver.profile = profile
		names = resolver
	}
	doc := &document{
		URI: params.TextDocument.URI, Path: path, Root: root, Text: []byte(params.TextDocument.Text),
		Version: params.TextDocument.Version, Includes: includes, Names: names, ready: make(chan struct{}),
		Revision: s.projectRevision,
	}
	if previous := s.document(doc.URI); previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	if s.snapshot == nil {
		s.snapshot = query.New()
	}
	s.snapshot, _ = s.snapshot.Update(query.Document{URI: coresource.URI(doc.URI), Text: doc.Text, Version: int64(doc.Version)})
	s.mu.Lock()
	s.documents[doc.URI] = doc
	s.mu.Unlock()
	s.schedulePublish(doc, s.snapshot)
	s.startWorkspaceIndex(doc)
	return nil
}

func (s *server) didChange(raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
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
	next := &document{
		URI: doc.URI, Path: doc.Path, Root: doc.Root, Text: []byte(params.ContentChanges[len(params.ContentChanges)-1].Text),
		Version: params.TextDocument.Version, Includes: doc.Includes, Names: doc.Names, ready: make(chan struct{}),
		Revision: doc.Revision,
	}
	var accepted bool
	s.snapshot, accepted = s.snapshot.Update(query.Document{URI: coresource.URI(next.URI), Text: next.Text, Version: int64(next.Version)})
	if !accepted {
		return nil
	}
	s.mu.Lock()
	s.documents[next.URI] = next
	s.mu.Unlock()
	s.schedulePublish(next, s.snapshot)
	s.restartWorkspaceIndex(next)
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
	s.mu.Unlock()
	s.restartWorkspaceIndex(doc)
	return s.notify("textDocument/publishDiagnostics", map[string]any{"uri": params.TextDocument.URI, "diagnostics": []any{}})
}

func (s *server) reloadProjects() error {
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
		includes, profile, root := loadProjectContext(doc.Path, s.includeRoots...)
		names := s.names
		if resolver, ok := names.(apiNameResolver); ok {
			resolver.profile = profile
			names = resolver
		}
		next := &document{
			URI: doc.URI, Path: doc.Path, Root: root, Text: doc.Text, Version: doc.Version,
			Includes: includes, Names: names, Revision: revision, ready: make(chan struct{}),
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

func (s *server) schedulePublish(doc *document, snapshot *query.Snapshot) {
	ctx, cancel := context.WithCancel(context.Background())
	doc.cancel = cancel
	s.workers.Go(func() {
		defer cancel()
		defer close(doc.ready)
		_ = s.publish(ctx, doc, snapshot)
	})
}

func (s *server) publish(ctx context.Context, doc *document, snapshot *query.Snapshot) error {
	diagnostics, err := lintDocument(doc)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		diagnostics = []diagnostic.Diagnostic{{RuleID: "configuration", Severity: diagnostic.SeverityError, Message: err.Error(), Filename: doc.Path}}
	}
	shared, analysisErr := snapshot.Analyze(ctx, coresource.URI(doc.URI), analysis.Options{
		URI: coresource.URI(doc.URI), Includes: doc.Includes, Names: doc.Names, RetainExpanded: true,
		Revision: fmt.Sprintf("%s:%T:%T:%d", doc.Path, doc.Includes, doc.Names, doc.Revision),
	})
	if analysisErr != nil {
		return analysisErr
	}
	diagnostics = reconcileDiagnostics(diagnostics, shared)
	doc.Diagnostics = diagnostics
	doc.Analysis = shared
	items := documentDiagnosticItems(doc)
	if ctx.Err() != nil || s.document(doc.URI) != doc {
		return ctx.Err()
	}
	return s.notify("textDocument/publishDiagnostics", map[string]any{"uri": doc.URI, "version": doc.Version, "diagnostics": items})
}

func reconcileDiagnostics(items []diagnostic.Diagnostic, shared *analysis.Result) []diagnostic.Diagnostic {
	missing := make(map[[2]int]bool)
	for _, item := range shared.Diagnostics {
		if item.Code == string(preprocess.CodeIncludeNotFound) && item.Primary.File == shared.File {
			missing[[2]int{int(item.Primary.Start), int(item.Primary.End)}] = true
		}
	}
	result := items[:0]
	for _, item := range items {
		key := [2]int{item.Range.Start.Offset, item.Range.End.Offset}
		resolvedInclude := item.RuleID == "missing-include" && !missing[key]
		macroDeclaration := item.RuleID == "duplicate-function-definition" && macroInvocationAt(shared, key[0], key[1])
		if !resolvedInclude && !macroDeclaration {
			result = append(result, item)
		}
	}
	return result
}

func macroInvocationAt(result *analysis.Result, start, end int) bool {
	if result == nil || result.Preprocess == nil {
		return false
	}
	for _, item := range result.Preprocess.ExpandedTokens {
		for origin := item.Origin; origin != nil; origin = origin.Parent {
			span := origin.Span
			if origin.Macro != "" && span.File == 0 && start >= span.Start.Offset && end <= span.End.Offset {
				return true
			}
		}
	}
	return false
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
	doc := s.document(uri)
	if doc != nil && doc.ready != nil {
		<-doc.ready
	}
	return doc
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
			rng := offsetRange(doc.Text, int(item.Span.Start), int(item.Span.End))
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
	index := coresource.NewLineIndex(string(doc.Text))
	offset, err := index.Offset(coresource.Position{
		Line: params.Position.Line, Character: params.Position.Character,
	}, coresource.UTF16)
	if err != nil {
		return s.respond(id, nil)
	}
	table := navigationTable(doc.Analysis)
	if include, ok := includeAt(doc.Analysis, int(offset)); ok {
		if !include.Resolved || include.ResolvedURI == "" {
			return s.respond(id, nil)
		}
		return s.respond(id, map[string]any{
			"uri":   include.ResolvedURI,
			"range": offsetRange(nil, 0, 0),
		})
	}
	for _, ref := range table.References {
		if ref.Resolved == 0 || !ref.Span.Contains(offset) {
			continue
		}
		decl, ok := table.Symbol(ref.Resolved)
		if !ok {
			break
		}
		return s.respond(id, analysisLocation(doc, decl.Span))
	}
	name, _, _ := identifierAt(doc.Text, int(offset))
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
		start, end := includePathRange(doc.Text, include)
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": includeHover(include)},
			"range":    offsetRange(doc.Text, start, end),
		})
	}
	item, ok := symbolAt(navigationTable(doc.Analysis), doc.Analysis.File, offset)
	if ok {
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": hoverText(doc, item)},
			"range":    offsetRange(doc.Text, int(item.Span.Start), int(item.Span.End)),
		})
	}
	name, start, end := identifierAt(doc.Text, int(offset))
	if macro, ok := doc.Analysis.Preprocess.Macros[name]; ok {
		return s.respond(id, map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": macroHover(doc.Analysis.Preprocess, macro)},
			"range":    offsetRange(doc.Text, start, end),
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
					"range":    offsetRange(doc.Text, start, end),
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
		"range":    offsetRange(doc.Text, start, end),
	})
}

func macroHover(result *preprocess.Result, macro preprocess.Macro) string {
	declaration := macroSignature(macro)
	if result != nil && int(macro.File) < len(result.Files) {
		if source := macroDefinition(result.Files[macro.File].Content, macro.DefSpan); source != "" {
			declaration = source
		}
	}
	return "```pawn\n" + declaration + "\n```"
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
	if result == nil || result.Registry == nil || result.Preprocess == nil {
		return ""
	}
	uri, ok := result.Registry.URI(item.Span.File)
	if !ok {
		return ""
	}
	for _, file := range result.Preprocess.Files {
		if file.URI == uri.String() {
			return declarationDocumentation(file.Content, item.Span)
		}
	}
	return ""
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
	if result == nil || result.Registry == nil || result.Preprocess == nil {
		return ""
	}
	uri, ok := result.Registry.URI(item.Span.File)
	if !ok {
		return ""
	}
	for _, file := range result.Preprocess.Files {
		if file.URI != uri.String() {
			continue
		}
		return declarationText(file.Content, item.Span)
	}
	return ""
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
	index := coresource.NewLineIndex(string(doc.Text))
	offset, err := index.Offset(coresource.Position{Line: pos.Line, Character: pos.Character}, coresource.UTF16)
	return offset, err == nil
}

func navigationTable(result *analysis.Result) *symbol.Table {
	if result.ExpandedSymbols != nil {
		return result.ExpandedSymbols
	}
	return result.Symbols
}

func symbolAt(table *symbol.Table, file coresource.FileID, offset coresource.Offset) (symbol.Symbol, bool) {
	for _, ref := range table.References {
		if ref.Resolved != 0 && ref.Span.File == file && ref.Span.Contains(offset) {
			return table.Symbol(ref.Resolved)
		}
	}
	for _, item := range table.Symbols {
		if item.Span.File == file && item.Span.Contains(offset) {
			return item, true
		}
	}
	return symbol.Symbol{}, false
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
	table := navigationTable(doc.Analysis)
	item, ok := symbolAt(table, doc.Analysis.File, offset)
	name := ""
	global := false
	if ok {
		name = item.Name
		if scope, found := table.Scope(item.Scope); found {
			global = scope.Kind == symbol.ScopeFile
		}
	} else {
		name, _, _ = identifierAt(doc.Text, int(offset))
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
	uri, text := doc.URI, doc.Text
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

func lintDocument(doc *document) ([]diagnostic.Diagnostic, error) {
	return editor.Diagnose(doc.Path, doc.Text, filepath.Dir(doc.Path))
}

func (s *server) codeActions(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	actions := make([]map[string]any, 0)
	if doc != nil {
		for _, finding := range doc.Diagnostics {
			if finding.Fix == nil || !safeFix(s.rules, finding.RuleID) {
				continue
			}
			edits := make([]textEdit, 0, len(finding.Fix.Edits))
			for _, edit := range finding.Fix.Edits {
				edits = append(edits, textEdit{Range: offsetRange(doc.Text, edit.Range.Start.Offset, edit.Range.End.Offset), NewText: edit.NewText})
			}
			actions = append(actions, map[string]any{
				"title":       finding.Fix.Description,
				"kind":        "quickfix",
				"isPreferred": true,
				"edit":        map[string]any{"changes": map[string]any{doc.URI: edits}},
			})
		}
	}
	return s.respond(id, actions)
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
	formatted, err := pawnfmt.Format(doc.Text, pawnfmt.Options{
		TabSize: params.Options.TabSize, UseTabs: !params.Options.InsertSpaces,
	})
	if err != nil {
		return s.respondError(id, -32603, err.Error())
	}
	if bytes.Equal(formatted, doc.Text) {
		return s.respond(id, []textEdit{})
	}
	return s.respond(id, []textEdit{{Range: offsetRange(doc.Text, 0, len(doc.Text)), NewText: string(formatted)}})
}

func diagnosticRange(source []byte, finding diagnostic.Diagnostic) lspRange {
	return offsetRange(source, finding.Range.Start.Offset, finding.Range.End.Offset)
}

func offsetRange(source []byte, start, end int) lspRange {
	return lspRange{Start: offsetPosition(source, start), End: offsetPosition(source, end)}
}

func offsetPosition(text []byte, offset int) position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	index := coresource.NewLineIndex(string(text))
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

func (s *server) notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
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
