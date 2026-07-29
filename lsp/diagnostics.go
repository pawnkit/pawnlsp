package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawnkit-core/diagnostic"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintdiagnostic "github.com/pawnkit/pawnlint/pkg/diagnostic"
)

func (s *server) documentDiagnostics(ctx context.Context, id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		PreviousResultID string          `json:"previousResultId"`
		WorkDoneToken    json.RawMessage `json:"workDoneToken"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	progress := s.delayedProgress(params.WorkDoneToken, id, "Analysing Pawn document")
	defer progress.finish()
	doc, err := s.readyDocumentContext(ctx, params.TextDocument.URI)
	if err != nil {
		return err
	}
	if doc == nil {
		return s.respond(id, map[string]any{"kind": "full", "items": []any{}})
	}
	full := doc.fullReady()
	graph, err := s.workspaceGraphContext(ctx, doc)
	if err != nil {
		return err
	}
	resultID := documentDiagnosticResultID(doc, full)
	if graph != nil {
		resultID += fmt.Sprintf(":%p", graph)
	}
	if params.PreviousResultID == resultID {
		return s.respond(id, map[string]any{"kind": "unchanged", "resultId": resultID})
	}
	return s.respond(id, map[string]any{
		"kind": "full", "resultId": resultID, "items": s.documentDiagnosticItems(doc, full, graph),
	})
}

func documentDiagnosticResultID(doc *document, full bool) string {
	stage := "analysis"
	if full {
		stage = "full"
	}
	return fmt.Sprintf("%d:%d:%s", doc.Revision, doc.Version, stage)
}

func (s *server) workspaceDiagnostics(ctx context.Context, id, raw json.RawMessage) error {
	var params struct {
		WorkDoneToken json.RawMessage `json:"workDoneToken"`
	}
	if err := json.Unmarshal(raw, &params); err != nil && len(raw) != 0 {
		return err
	}
	progress := s.delayedProgress(params.WorkDoneToken, id, "Analysing Pawn workspace")
	defer progress.finish()

	s.mu.Lock()
	indexes := make([]*workspaceIndex, 0, len(s.workspaces))
	for _, index := range s.workspaces {
		indexes = append(indexes, index)
	}
	s.mu.Unlock()

	items := make([]map[string]any, 0)
	for _, index := range indexes {
		var err error
		index, err = s.readyWorkspaceIndex(ctx, index)
		if err != nil {
			return err
		}
		if index == nil {
			continue
		}
		if index.graph != nil {
			for uri, diagnostics := range analysisGraphDiagnosticItems(index.graph) {
				if !workspaceDiagnosticURI(index.root, uri) {
					continue
				}
				items = append(items, map[string]any{
					"uri": uri.String(), "kind": "full", "items": dedupeDiagnostics(diagnostics),
				})
			}
			continue
		}
		for uri, result := range index.files {
			items = append(items, map[string]any{
				"uri": uri.String(), "kind": "full", "items": standaloneWorkspaceDiagnosticItems(uri, result),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := items[i]["uri"].(string)
		right, _ := items[j]["uri"].(string)
		return left < right
	})
	return s.respond(id, map[string]any{"items": items})
}

func (s *server) readyWorkspaceIndex(ctx context.Context, index *workspaceIndex) (*workspaceIndex, error) {
	for index != nil {
		select {
		case <-workspaceDiagnosticsReady(index):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		s.mu.Lock()
		current := s.workspaces[index.root]
		s.mu.Unlock()
		if current == index {
			return index, nil
		}
		index = current
	}
	return nil, nil
}

func (s *server) readyDocumentContext(ctx context.Context, uri string) (*document, error) {
	for {
		doc := s.document(uri)
		if doc == nil || doc.analysisReady == nil {
			return doc, nil
		}
		select {
		case <-doc.analysisReady:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if s.document(uri) == doc {
			return doc, nil
		}
	}
}

type delayedProgress struct {
	server  *server
	token   json.RawMessage
	request json.RawMessage
	title   string
	timer   *time.Timer
	mu      sync.Mutex
	started bool
	done    bool
	create  bool
}

func (s *server) delayedProgress(token, request json.RawMessage, title string) *delayedProgress {
	progress := &delayedProgress{server: s, token: token, request: request, title: title}
	if (len(token) == 0 || string(token) == "null") && s.workDoneProgress {
		value, _ := json.Marshal(fmt.Sprintf("pawn-analysis-%d", s.nextRequestID.Add(1)))
		progress.token = value
		progress.create = true
	}
	if len(progress.token) == 0 || string(progress.token) == "null" {
		return progress
	}
	progress.timer = time.AfterFunc(500*time.Millisecond, progress.begin)
	return progress
}

func (p *delayedProgress) begin() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return
	}
	p.started = true
	if p.request != nil {
		p.server.mu.Lock()
		cancel := p.server.requestCancels[string(p.request)]
		if cancel != nil {
			p.server.progressCancels[string(p.token)] = cancel
		}
		p.server.mu.Unlock()
	}
	if p.create {
		_ = p.server.write(map[string]any{
			"jsonrpc": "2.0", "id": p.server.nextRequestID.Add(1),
			"method": "window/workDoneProgress/create",
			"params": map[string]any{"token": p.token},
		})
	}
	_ = p.server.write(map[string]any{
		"jsonrpc": "2.0", "method": "$/progress",
		"params": map[string]any{
			"token": p.token,
			"value": map[string]any{"kind": "begin", "title": p.title, "cancellable": true},
		},
	})
}

func (p *delayedProgress) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
	if p.timer != nil {
		p.timer.Stop()
	}
	if !p.started {
		return
	}
	p.server.mu.Lock()
	delete(p.server.progressCancels, string(p.token))
	p.server.mu.Unlock()
	_ = p.server.write(map[string]any{
		"jsonrpc": "2.0", "method": "$/progress",
		"params": map[string]any{
			"token": p.token,
			"value": map[string]any{"kind": "end"},
		},
	})
}

func standaloneWorkspaceDiagnosticItems(uri coresource.URI, result *analysis.Result) []lspDiagnostic {
	if path, err := uri.Filename(); err == nil && strings.EqualFold(filepath.Ext(path), ".inc") {
		return []lspDiagnostic{}
	}
	return analysisDiagnosticItems(result, analysisSource(result))
}

func workspaceDiagnosticURI(root string, uri coresource.URI) bool {
	path, err := uri.Filename()
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return !ignoredWorkspacePath(relative)
}

func ignoredWorkspacePath(path string) bool {
	for part := range strings.SplitSeq(filepath.Clean(path), string(filepath.Separator)) {
		if part == "dependencies" || part == "pawno" {
			return true
		}
	}
	return false
}

type lineIndexSet map[coresource.URI]*coresource.LineIndex

func (set lineIndexSet) forURI(uri coresource.URI, text []byte) *coresource.LineIndex {
	if index, ok := set[uri]; ok {
		return index
	}
	index := coresource.NewLineIndex(string(text))
	set[uri] = index
	return index
}

func analysisGraphDiagnosticItems(result *analysis.Result) map[coresource.URI][]lspDiagnostic {
	items := make(map[coresource.URI][]lspDiagnostic)
	if result == nil || result.Preprocess == nil || result.Registry == nil {
		return items
	}
	text := make(map[coresource.URI][]byte, len(result.Preprocess.Files))
	for _, file := range result.Preprocess.Files {
		uri := coresource.URI(file.URI)
		if !uri.IsValid() {
			continue
		}
		text[uri] = file.Content
		items[uri] = nil
	}
	indexes := make(lineIndexSet, len(text))
	macros := newMacroInvocationIndex(result)
	for _, finding := range result.Diagnostics {
		uri, ok := result.Registry.URI(finding.Primary.File)
		if !ok || macroDiagnostic(macros, finding) {
			continue
		}
		items[uri] = append(items[uri], lspDiagnostic{
			Range:    offsetRangeWithIndex(indexes.forURI(uri, text[uri]), int(finding.Primary.Start), int(finding.Primary.End)),
			Severity: coreLSPSeverity(finding.Severity), Code: finding.Code,
			CodeDescription: analysisDiagnosticDocumentation(finding.DocsURL), Source: finding.Source, Message: finding.Message,
			RelatedInformation: analysisRelatedInformation(result, finding, indexes),
		})
	}
	return items
}

func (s *server) workspaceGraph(doc *document) *analysis.Result {
	graph, _ := s.workspaceGraphContext(context.Background(), doc)
	return graph
}

func (s *server) workspaceGraphContext(ctx context.Context, doc *document) (*analysis.Result, error) {
	if doc == nil || doc.Root == "" {
		return nil, nil
	}
	s.mu.Lock()
	index := s.workspaces[doc.Root]
	s.mu.Unlock()
	if index == nil {
		return nil, nil
	}
	select {
	case <-workspaceDiagnosticsReady(index):
		return index.graph, index.diagnosticErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func workspaceDiagnosticsReady(index *workspaceIndex) <-chan struct{} {
	if index.diagnosticsReady != nil {
		return index.diagnosticsReady
	}
	return index.ready
}

func (s *server) documentDiagnosticItems(doc *document, includeLint bool, graph *analysis.Result) []lspDiagnostic {
	if ignoredWorkspacePath(doc.Path) {
		return []lspDiagnostic{}
	}
	index := doc.lineIndex()
	capacity := 0
	if includeLint {
		capacity = len(doc.Diagnostics)
	}
	items := make([]lspDiagnostic, 0, capacity)
	if includeLint {
		for _, finding := range doc.Diagnostics {
			if graph != nil && strings.HasPrefix(finding.RuleID, "pawn-analysis:") {
				continue
			}
			var documentation *lspCodeDescription
			if strings.HasPrefix(finding.RuleID, "pawn-analysis:") {
				documentation = analysisDiagnosticDocumentation("")
			}
			if s.rules != nil {
				if _, ok := s.rules.Lookup(finding.RuleID); ok {
					documentation = diagnosticDocumentation("https://github.com/pawnkit/pawnlint/blob/main/docs/rules/" + finding.RuleID + ".md")
				}
			}
			items = append(items, lspDiagnostic{
				Range: offsetRangeWithIndex(index, finding.Range.Start.Offset, finding.Range.End.Offset), Severity: lspSeverity(finding.Severity),
				Code: finding.RuleID, CodeDescription: documentation,
				Source: "pawnlint", Message: finding.Message,
				RelatedInformation: lintRelatedInformationWithIndex(doc.URI, index, finding),
			})
		}
	}
	if graph != nil {
		analysisItems := analysisGraphDiagnosticItems(graph)[coresource.URI(doc.URI)]
		items = append(items, removeMirroredAnalysisDiagnostics(analysisItems, items)...)
	} else {
		analysisItems := analysisDiagnosticItemsWithIndex(doc.Analysis, doc.text(), index)
		items = append(items, removeMirroredAnalysisDiagnostics(analysisItems, items)...)
	}
	return dedupeDiagnostics(items)
}

func removeMirroredAnalysisDiagnostics(analysisItems, lintItems []lspDiagnostic) []lspDiagnostic {
	mirrors := map[string]string{"pawn-analysis:sema/unreachable": "unreachable-code"}
	result := analysisItems[:0]
	for _, item := range analysisItems {
		lintCode, mirrored := mirrors[item.Code]
		if mirrored && hasDiagnosticAt(lintItems, lintCode, item.Range) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func hasDiagnosticAt(items []lspDiagnostic, code string, target lspRange) bool {
	for _, item := range items {
		if item.Code == code && rangesOverlap(item.Range, target) {
			return true
		}
	}
	return false
}

func analysisDiagnosticItems(result *analysis.Result, text []byte) []lspDiagnostic {
	return analysisDiagnosticItemsWithIndex(result, text, nil)
}

func analysisDiagnosticItemsWithIndex(result *analysis.Result, text []byte, index *coresource.LineIndex) []lspDiagnostic {
	if result == nil {
		return nil
	}
	if index == nil {
		index = coresource.NewLineIndex(string(text))
	}
	indexes := make(lineIndexSet, 1)
	if result.Registry != nil {
		if uri, ok := result.Registry.URI(result.File); ok {
			indexes[uri] = index
		}
	}
	items := make([]lspDiagnostic, 0, len(result.Diagnostics))
	macros := newMacroInvocationIndex(result)
	for _, finding := range result.Diagnostics {
		if finding.Primary.File != result.File || macroDiagnostic(macros, finding) {
			continue
		}
		items = append(items, lspDiagnostic{
			Range:    offsetRangeWithIndex(index, int(finding.Primary.Start), int(finding.Primary.End)),
			Severity: coreLSPSeverity(finding.Severity), Code: finding.Code,
			CodeDescription: analysisDiagnosticDocumentation(finding.DocsURL), Source: finding.Source, Message: finding.Message,
			RelatedInformation: analysisRelatedInformation(result, finding, indexes),
		})
	}
	return items
}

func analysisRelatedInformation(result *analysis.Result, finding diagnostic.Diagnostic, indexes lineIndexSet) []lspDiagnosticRelatedInformation {
	if result == nil || result.Registry == nil {
		return nil
	}
	items := make([]lspDiagnosticRelatedInformation, 0, len(finding.Related))
	for _, related := range finding.Related {
		uri, ok := result.Registry.URI(related.Span.File)
		if !ok {
			continue
		}
		index := indexes.forURI(uri, analysisFileText(result, uri))
		items = append(items, lspDiagnosticRelatedInformation{
			Location: lspLocation{
				URI:   uri.String(),
				Range: offsetRangeWithIndex(index, int(related.Span.Start), int(related.Span.End)),
			},
			Message: related.Message,
		})
	}
	return items
}

func analysisFileText(result *analysis.Result, uri coresource.URI) []byte {
	if result == nil || result.Preprocess == nil {
		return nil
	}
	for _, file := range result.Preprocess.Files {
		if file.URI == uri.String() {
			return file.Content
		}
	}
	return nil
}

func lintRelatedInformationWithIndex(uri string, index *coresource.LineIndex, finding lintdiagnostic.Diagnostic) []lspDiagnosticRelatedInformation {
	items := make([]lspDiagnosticRelatedInformation, 0, len(finding.Notes))
	for _, related := range finding.Notes {
		items = append(items, lspDiagnosticRelatedInformation{
			Location: lspLocation{URI: uri, Range: offsetRangeWithIndex(index, related.Range.Start.Offset, related.Range.End.Offset)},
			Message:  related.Message,
		})
	}
	return items
}

func diagnosticDocumentation(url string) *lspCodeDescription {
	if url == "" {
		return nil
	}
	return &lspCodeDescription{Href: url}
}

func analysisDiagnosticDocumentation(url string) *lspCodeDescription {
	if url == "" {
		url = "https://github.com/pawnkit/pawn-analysis/blob/main/docs/diagnostics.md"
	}
	return diagnosticDocumentation(url)
}

func macroDiagnostic(index macroInvocationIndex, finding diagnostic.Diagnostic) bool {
	return finding.Code == "pawn-analysis:symbol/redeclared" &&
		index.contains(int(finding.Primary.Start), int(finding.Primary.End))
}

type macroInvocationSpan struct {
	start int
	end   int
}

type macroInvocationIndex struct {
	spans  []macroInvocationSpan
	maxEnd []int
}

func newMacroInvocationIndex(result *analysis.Result) macroInvocationIndex {
	if result == nil || result.Preprocess == nil {
		return macroInvocationIndex{}
	}
	unique := make(map[macroInvocationSpan]struct{})
	for _, item := range result.Preprocess.ExpandedTokens {
		for origin := item.Origin; origin != nil; origin = origin.Parent {
			span := origin.Span
			if origin.Macro != "" && span.File == 0 {
				unique[macroInvocationSpan{start: span.Start.Offset, end: span.End.Offset}] = struct{}{}
			}
		}
	}
	spans := make([]macroInvocationSpan, 0, len(unique))
	for span := range unique {
		spans = append(spans, span)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	maxEnd := make([]int, len(spans))
	for i, span := range spans {
		maxEnd[i] = span.end
		if i > 0 && maxEnd[i-1] > maxEnd[i] {
			maxEnd[i] = maxEnd[i-1]
		}
	}
	return macroInvocationIndex{spans: spans, maxEnd: maxEnd}
}

func (i macroInvocationIndex) contains(start, end int) bool {
	index := sort.Search(len(i.spans), func(index int) bool {
		return i.spans[index].start > start
	}) - 1
	return index >= 0 && i.maxEnd[index] >= end
}

func analysisSource(result *analysis.Result) []byte {
	if result == nil || result.Preprocess == nil {
		return nil
	}
	return result.Preprocess.Source
}
