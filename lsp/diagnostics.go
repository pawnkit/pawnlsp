package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawnkit-core/diagnostic"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintdiagnostic "github.com/pawnkit/pawnlint/pkg/diagnostic"
)

func (s *server) documentDiagnostics(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		PreviousResultID string `json:"previousResultId"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil {
		return s.respond(id, map[string]any{"kind": "full", "items": []any{}})
	}
	full := doc.fullReady()
	resultID := documentDiagnosticResultID(doc, full)
	if params.PreviousResultID == resultID {
		return s.respond(id, map[string]any{"kind": "unchanged", "resultId": resultID})
	}
	return s.respond(id, map[string]any{
		"kind": "full", "resultId": resultID, "items": s.documentDiagnosticItems(doc, full),
	})
}

func documentDiagnosticResultID(doc *document, full bool) string {
	stage := "analysis"
	if full {
		stage = "full"
	}
	return fmt.Sprintf("%d:%d:%s", doc.Revision, doc.Version, stage)
}

func (s *server) workspaceDiagnostics(id json.RawMessage) error {
	s.mu.Lock()
	indexes := make([]*workspaceIndex, 0, len(s.workspaces))
	for _, index := range s.workspaces {
		indexes = append(indexes, index)
	}
	s.mu.Unlock()

	items := make([]map[string]any, 0)
	for _, index := range indexes {
		<-index.ready
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
			text := analysisSource(result)
			items = append(items, map[string]any{
				"uri": uri.String(), "kind": "full", "items": analysisDiagnosticItems(result, text),
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

func workspaceDiagnosticURI(root string, uri coresource.URI) bool {
	path, err := uri.Filename()
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	for _, directory := range []string{"dependencies", "pawno"} {
		if relative == directory || strings.HasPrefix(relative, directory+string(filepath.Separator)) {
			return false
		}
	}
	return true
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
	for _, finding := range result.Diagnostics {
		uri, ok := result.Registry.URI(finding.Primary.File)
		if !ok || macroDiagnostic(result, finding) {
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

func (s *server) documentDiagnosticItems(doc *document, includeLint bool) []lspDiagnostic {
	index := doc.lineIndex()
	capacity := 0
	if includeLint {
		capacity = len(doc.Diagnostics)
	}
	items := make([]lspDiagnostic, 0, capacity)
	if includeLint {
		for _, finding := range doc.Diagnostics {
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
	items = append(items, analysisDiagnosticItemsWithIndex(doc.Analysis, doc.text(), index)...)
	return dedupeDiagnostics(items)
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
	for _, finding := range result.Diagnostics {
		if finding.Primary.File != result.File || macroDiagnostic(result, finding) {
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

func macroDiagnostic(result *analysis.Result, finding diagnostic.Diagnostic) bool {
	return finding.Code == "pawn-analysis:symbol/redeclared" &&
		macroInvocationAt(result, int(finding.Primary.Start), int(finding.Primary.End))
}

func analysisSource(result *analysis.Result) []byte {
	if result == nil || result.Preprocess == nil {
		return nil
	}
	return result.Preprocess.Source
}
