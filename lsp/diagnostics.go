package lsp

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawnkit-core/diagnostic"
	coresource "github.com/pawnkit/pawnkit-core/source"
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
	return s.respond(id, map[string]any{"kind": "full", "items": s.documentDiagnosticItems(doc)})
}

func (s *server) workspaceDiagnostics(id json.RawMessage) error {
	s.mu.Lock()
	indexes := make([]*workspaceIndex, 0, len(s.workspaces))
	for _, index := range s.workspaces {
		indexes = append(indexes, index)
	}
	documents := make([]*document, 0, len(s.documents))
	for _, doc := range s.documents {
		documents = append(documents, doc)
	}
	s.mu.Unlock()
	active := make(map[string][]*analysis.Result)
	for _, doc := range documents {
		<-doc.ready
		if doc.Analysis == nil || doc.Analysis.Preprocess == nil {
			continue
		}
		active[doc.Root] = append(active[doc.Root], doc.Analysis)
	}

	items := make([]map[string]any, 0)
	for _, index := range indexes {
		if results := active[index.root]; len(results) > 0 {
			byURI := make(map[coresource.URI][]lspDiagnostic)
			for _, result := range results {
				for uri, diagnostics := range analysisGraphDiagnosticItems(result) {
					if !workspaceDiagnosticURI(index.root, uri) {
						continue
					}
					byURI[uri] = append(byURI[uri], diagnostics...)
				}
			}
			for uri, diagnostics := range byURI {
				items = append(items, map[string]any{
					"uri": uri.String(), "kind": "full", "items": dedupeDiagnostics(diagnostics),
				})
			}
			continue
		}
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
	for _, finding := range result.Diagnostics {
		uri, ok := result.Registry.URI(finding.Primary.File)
		if !ok || macroDiagnostic(result, finding) {
			continue
		}
		items[uri] = append(items[uri], lspDiagnostic{
			Range:    offsetRange(text[uri], int(finding.Primary.Start), int(finding.Primary.End)),
			Severity: coreLSPSeverity(finding.Severity), Code: finding.Code,
			CodeDescription: analysisDiagnosticDocumentation(finding.DocsURL), Source: finding.Source, Message: finding.Message,
		})
	}
	return items
}

func (s *server) documentDiagnosticItems(doc *document) []lspDiagnostic {
	items := make([]lspDiagnostic, 0, len(doc.Diagnostics))
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
			Range: diagnosticRange(doc.Text, finding), Severity: lspSeverity(finding.Severity),
			Code: finding.RuleID, CodeDescription: documentation,
			Source: "pawnlint", Message: finding.Message,
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
			CodeDescription: analysisDiagnosticDocumentation(finding.DocsURL), Source: finding.Source, Message: finding.Message,
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
