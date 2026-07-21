package lsp

import (
	"encoding/json"
	"sort"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawnkit-core/diagnostic"
)

func (s *server) documentDiagnostics(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil {
		return s.respond(id, map[string]any{"kind": "full", "items": []any{}})
	}
	return s.respond(id, map[string]any{"kind": "full", "items": documentDiagnosticItems(doc)})
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

func documentDiagnosticItems(doc *document) []lspDiagnostic {
	items := make([]lspDiagnostic, 0, len(doc.Diagnostics))
	for _, finding := range doc.Diagnostics {
		items = append(items, lspDiagnostic{
			Range: diagnosticRange(doc.Text, finding), Severity: lspSeverity(finding.Severity),
			Code: finding.RuleID, Source: "pawnlint", Message: finding.Message,
		})
	}
	items = append(items, analysisDiagnosticItems(doc.Analysis, doc.Text)...)
	return dedupeDiagnostics(items)
}

func analysisDiagnosticItems(result *analysis.Result, text []byte) []lspDiagnostic {
	if result == nil {
		return nil
	}
	items := make([]lspDiagnostic, 0, len(result.Diagnostics))
	for _, finding := range result.Diagnostics {
		if finding.Primary.File != result.File || macroDiagnostic(result, finding) {
			continue
		}
		items = append(items, lspDiagnostic{
			Range:    offsetRange(text, int(finding.Primary.Start), int(finding.Primary.End)),
			Severity: coreLSPSeverity(finding.Severity), Code: finding.Code,
			Source: finding.Source, Message: finding.Message,
		})
	}
	return items
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
