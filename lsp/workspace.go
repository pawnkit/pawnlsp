package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/query"
	"github.com/pawnkit/pawn-analysis/sema"
	"github.com/pawnkit/pawn-analysis/symbol"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

const (
	workspaceFileLimit = 5000
	workspaceByteLimit = 64 << 20
	workspaceFileSize  = 4 << 20
)

type workspaceIndex struct {
	root             string
	entry            string
	ready            chan struct{}
	diagnosticsReady chan struct{}
	files            map[coresource.URI]*analysis.Result
	graph            *analysis.Result
	previous         *analysis.Result
	diagnosticErr    error
	err              error
	cancel           context.CancelFunc
}

type workspaceOccurrence struct {
	uri         coresource.URI
	text        []byte
	span        coresource.Span
	declaration bool
}

func (s *server) startWorkspaceIndex(doc *document) {
	s.startWorkspaceIndexAfter(doc, 0, nil)
}

func (s *server) refreshWorkspaceIndex(doc *document) {
	if doc == nil || doc.Root == "" {
		return
	}
	s.mu.Lock()
	if current := s.workspaces[doc.Root]; current != nil && current.cancel != nil {
		current.cancel()
	}
	delete(s.workspaces, doc.Root)
	s.mu.Unlock()
	s.startWorkspaceIndex(doc)
}

func (s *server) startWorkspaceIndexAfter(doc *document, delay time.Duration, previous *analysis.Result) {
	if doc == nil || doc.Root == "" {
		return
	}
	s.mu.Lock()
	if s.workspaces == nil {
		s.workspaces = make(map[string]*workspaceIndex)
	}
	if _, exists := s.workspaces[doc.Root]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	index := &workspaceIndex{
		root: doc.Root, entry: doc.Entry, ready: make(chan struct{}), diagnosticsReady: make(chan struct{}), cancel: cancel,
		previous: previous,
	}
	s.workspaces[doc.Root] = index
	open := make(map[string][]byte)
	for _, current := range s.documents {
		if current.Root == doc.Root {
			open[workspacePathKey(current.Path)] = append([]byte(nil), current.text()...)
		}
	}
	s.mu.Unlock()

	s.workers.Go(func() {
		defer cancel()
		diagnosticsClosed := false
		closeDiagnostics := func() {
			if !diagnosticsClosed {
				close(index.diagnosticsReady)
				diagnosticsClosed = true
			}
		}
		defer func() {
			closeDiagnostics()
			close(index.ready)
		}()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				index.err = ctx.Err()
				index.diagnosticErr = index.err
				return
			}
		}
		if doc.Entry != "" {
			graph, diagnosticErr := analyzeWorkspaceEntry(
				ctx, doc.Root, doc.Entry, open, doc.Includes, doc.Names, s.tokenCache, previous,
			)
			s.mu.Lock()
			index.graph = graph
			index.diagnosticErr = diagnosticErr
			s.mu.Unlock()
			closeDiagnostics()
			if index.diagnosticErr != nil {
				index.err = index.diagnosticErr
				return
			}
			s.mu.Lock()
			current := s.workspaces[doc.Root] == index
			s.mu.Unlock()
			if current {
				s.requestDiagnosticRefresh()
			}
			index.files, index.err = analyzeWorkspaceFiles(
				ctx, doc.Root, doc.Entry, open, doc.Includes, doc.Names, s.tokenCache,
			)
			return
		}
		index.files, index.err = analyzeWorkspaceFiles(
			ctx, doc.Root, "", open, doc.Includes, doc.Names, s.tokenCache,
		)
		index.diagnosticErr = index.err
		closeDiagnostics()
		if index.err == nil {
			s.requestDiagnosticRefresh()
		}
	})
}

func (s *server) restartWorkspaceIndex(doc *document) {
	if doc == nil || doc.Root == "" {
		return
	}
	s.mu.Lock()
	var previous *analysis.Result
	if current := s.workspaces[doc.Root]; current != nil && current.cancel != nil {
		current.cancel()
		previous = reusableWorkspaceGraph(current)
	}
	delete(s.workspaces, doc.Root)
	s.mu.Unlock()
	s.startWorkspaceIndexAfter(doc, 150*time.Millisecond, previous)
}

func reusableWorkspaceGraph(index *workspaceIndex) *analysis.Result {
	if index == nil {
		return nil
	}
	if index.graph != nil {
		return index.graph
	}
	return index.previous
}

func buildWorkspaceIndex(
	ctx context.Context,
	root string,
	entry string,
	open map[string][]byte,
	includes preprocess.IncludeResolver,
	names sema.Resolver,
	tokenCache *preprocess.TokenCache,
) (map[coresource.URI]*analysis.Result, *analysis.Result, error) {
	var graph *analysis.Result
	var err error
	if entry != "" {
		graph, err = analyzeWorkspaceEntry(ctx, root, entry, open, includes, names, tokenCache, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	files, err := analyzeWorkspaceFiles(ctx, root, entry, open, includes, names, tokenCache)
	return files, graph, err
}

func analyzeWorkspaceFiles(
	ctx context.Context,
	root string,
	entry string,
	open map[string][]byte,
	includes preprocess.IncludeResolver,
	names sema.Resolver,
	tokenCache *preprocess.TokenCache,
) (map[coresource.URI]*analysis.Result, error) {
	paths, err := workspaceSourceFiles(root)
	if err != nil {
		return nil, err
	}
	selected := paths[:0]
	for _, path := range paths {
		if entry != "" && (filepath.Ext(path) != ".pwn" || workspacePathKey(path) == workspacePathKey(entry)) {
			continue
		}
		if _, isOpen := open[workspacePathKey(path)]; !isOpen {
			selected = append(selected, path)
		}
	}
	paths = selected
	sort.Strings(paths)
	snapshot := query.New()
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := os.ReadFile(path) //nolint:gosec // Paths come from the bounded workspace scan.
		if err != nil {
			continue
		}
		uri := coresource.FileURI(path)
		snapshot, _ = snapshot.Update(query.Document{URI: uri, Text: text, Version: 1})
	}
	workspace, err := snapshot.AnalyzeWorkspace(ctx, analysis.Options{
		Includes: includes, Names: names, Revision: root, MaxOutputTokens: analysisOutputTokenLimit,
		TokenCache: tokenCache, SkipSemantics: true,
	})
	if err != nil {
		return nil, err
	}
	return workspace.Files, nil
}

func analyzeWorkspaceEntry(
	ctx context.Context,
	root string,
	entry string,
	open map[string][]byte,
	includes preprocess.IncludeResolver,
	names sema.Resolver,
	tokenCache *preprocess.TokenCache,
	previous *analysis.Result,
) (*analysis.Result, error) {
	text, ok := open[workspacePathKey(entry)]
	var err error
	if !ok {
		text, err = os.ReadFile(entry) //nolint:gosec // Entry comes from the resolved project manifest.
		if err != nil {
			return nil, err
		}
	}
	return analysis.AnalyzeContext(ctx, text, analysis.Options{
		URI: coresource.FileURI(entry), Includes: workspaceOverlayResolver{base: includes, open: open}, Names: names, RetainExpanded: true,
		Revision: root, MaxOutputTokens: analysisOutputTokenLimit, TokenCache: tokenCache, Previous: previous,
		ReuseCompatibleExpansion: true,
	})
}

type workspaceOverlayResolver struct {
	base preprocess.IncludeResolver
	open map[string][]byte
}

func (r workspaceOverlayResolver) Resolve(fromURI, path string, angle bool) ([]byte, string, bool) {
	if r.base == nil {
		return nil, "", false
	}
	content, uri, ok := r.base.Resolve(fromURI, path, angle)
	if !ok {
		return nil, "", false
	}
	if filename, err := coresource.URI(uri).Filename(); err == nil {
		if overlay, found := r.open[workspacePathKey(filename)]; found {
			content = overlay
		}
	}
	return content, uri, true
}

func workspacePathKey(path string) string {
	key := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(path, `\`, "/")))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func (s *server) workspaceSymbols(id, raw json.RawMessage) error {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	s.mu.Lock()
	indexes := make([]*workspaceIndex, 0, len(s.workspaces))
	for _, index := range s.workspaces {
		indexes = append(indexes, index)
	}
	s.mu.Unlock()
	queryText := strings.ToLower(params.Query)
	items := make([]map[string]any, 0)
	seen := make(map[string]bool)
	for uri, result := range s.workspaceResults() {
		if result == nil || result.Symbols == nil {
			continue
		}
		for _, item := range result.Symbols.Symbols {
			scope, ok := result.Symbols.Scope(item.Scope)
			if !ok || scope.Kind != symbol.ScopeFile || queryText != "" && !strings.Contains(strings.ToLower(item.Name), queryText) {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s", uri, item.Span.Start, item.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			container := uri.String()
			if path, err := uri.Filename(); err == nil {
				for _, index := range indexes {
					if relative, err := filepath.Rel(index.root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
						container = filepath.ToSlash(relative)
						break
					}
				}
			}
			items = append(items, map[string]any{
				"name": item.Name, "kind": symbolKind(item.Kind), "containerName": container,
				"location": map[string]any{
					"uri": uri.String(), "range": offsetRange(result.Preprocess.Source, int(item.Span.Start), int(item.Span.End)),
				},
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, leftOK := items[i]["name"].(string)
		right, rightOK := items[j]["name"].(string)
		return leftOK && rightOK && strings.ToLower(left) < strings.ToLower(right)
	})
	return s.respond(id, items)
}

func (s *server) workspaceOccurrences(name string) []workspaceOccurrence {
	items := make([]workspaceOccurrence, 0)
	seen := make(map[string]bool)
	for uri, result := range s.workspaceResults() {
		if result == nil || result.Symbols == nil || result.Preprocess == nil {
			continue
		}
		add := func(span coresource.Span, declaration bool) {
			key := fmt.Sprintf("%s:%d:%d", uri, span.Start, span.End)
			if seen[key] {
				return
			}
			seen[key] = true
			items = append(items, workspaceOccurrence{
				uri: uri, text: result.Preprocess.Source, span: span, declaration: declaration,
			})
		}
		for _, item := range result.Symbols.Symbols {
			scope, ok := result.Symbols.Scope(item.Scope)
			if ok && scope.Kind == symbol.ScopeFile && item.Name == name {
				add(item.Span, true)
			}
		}
		for _, reference := range result.Symbols.References {
			if reference.Name != name {
				continue
			}
			if reference.Resolved != 0 {
				item, ok := result.Symbols.Symbol(reference.Resolved)
				if !ok {
					continue
				}
				scope, ok := result.Symbols.Scope(item.Scope)
				if !ok || scope.Kind != symbol.ScopeFile {
					continue
				}
			}
			add(reference.Span, false)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].uri == items[j].uri {
			return items[i].span.Start < items[j].span.Start
		}
		return items[i].uri.String() < items[j].uri.String()
	})
	return items
}

func (s *server) workspaceCompletionItems(items []map[string]any, prefix string) []map[string]any {
	indexes := make(map[string]int, len(items))
	for index, item := range items {
		if label, ok := item["label"].(string); ok {
			indexes[label] = index
		}
	}
	for _, result := range s.workspaceResults() {
		if result == nil || result.Symbols == nil {
			continue
		}
		for _, candidate := range result.Symbols.Symbols {
			scope, ok := result.Symbols.Scope(candidate.Scope)
			if !ok || scope.Kind != symbol.ScopeFile {
				continue
			}
			if prefix != "" && !strings.HasPrefix(strings.ToLower(candidate.Name), strings.ToLower(prefix)) {
				continue
			}
			item := map[string]any{
				"label": candidate.Name, "kind": completionSymbolKind(candidate.Kind), "detail": symbolSummary(candidate),
				"sortText": "2_" + strings.ToLower(candidate.Name),
			}
			if result.Registry != nil {
				if uri, ok := result.Registry.URI(candidate.Span.File); ok {
					item["data"] = completionData{Kind: "workspace", URI: uri.String(), Name: candidate.Name, Start: int(candidate.Span.Start)}
				}
			}
			if existing, found := indexes[candidate.Name]; found {
				if completionPriority(items[existing]) <= completionPriority(item) {
					continue
				}
				items[existing] = item
				continue
			}
			indexes[candidate.Name] = len(items)
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return completionItemLess(items[i], items[j]) })
	return items
}

func workspaceSourceFiles(root string) ([]string, error) {
	files := make([]string, 0)
	total := int64(0)
	var walk func(string) error
	walk = func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if directory == root {
				return err
			}
			return nil
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				if skipWorkspaceDirectory(entry.Name()) {
					continue
				}
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 || !workspaceSourceExtension(filepath.Ext(entry.Name())) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > workspaceFileSize {
				continue
			}
			if len(files) >= workspaceFileLimit || total+info.Size() > workspaceByteLimit {
				return fmt.Errorf("workspace index exceeds %d files or %d bytes", workspaceFileLimit, workspaceByteLimit)
			}
			files = append(files, path)
			total += info.Size()
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return files, nil
}

func skipWorkspaceDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || name == "build" || name == "dependencies" || name == "dist" || name == "node_modules" || name == "pawno"
}

func workspaceSourceExtension(extension string) bool {
	return strings.EqualFold(extension, ".pwn") || strings.EqualFold(extension, ".inc")
}
