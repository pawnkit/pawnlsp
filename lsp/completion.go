package lsp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
)

type textPositionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position position `json:"position"`
}

func (s *server) completion(id, raw json.RawMessage) error {
	var params textPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, []any{})
	}
	prefix, _, _ := identifierAt(doc.Text, int(offset))
	items := completionItems(doc, prefix)
	return s.respond(id, items)
}

func completionItems(doc *document, prefix string) []map[string]any {
	items := make([]map[string]any, 0)
	seen := make(map[string]bool)
	add := func(name string, item map[string]any) {
		if name == "" || seen[name] || prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return
		}
		seen[name] = true
		item["label"] = name
		items = append(items, item)
	}

	if doc != nil && doc.Analysis != nil {
		if table := navigationTable(doc.Analysis); table != nil {
			for _, item := range table.Symbols {
				add(item.Name, map[string]any{
					"kind":   completionSymbolKind(item.Kind),
					"detail": symbolSummary(item),
				})
			}
		}
		if doc.Analysis.Preprocess != nil {
			for _, macro := range doc.Analysis.Preprocess.Macros {
				detail := "macro"
				if macro.Kind == preprocess.MacroFunctionLike {
					detail = macroSignature(macro)
				}
				add(macro.Name, map[string]any{"kind": 14, "detail": detail})
			}
		}
	}

	if resolver, ok := doc.Names.(apiNameResolver); ok && resolver.index != nil {
		for _, entry := range resolver.index.All() {
			if !resolver.available(entry) {
				continue
			}
			item := map[string]any{
				"kind":   completionAPIKind(entry.Kind),
				"detail": apiDeclaration(entry),
			}
			if documentation := apiDocumentation(entry); documentation != "" {
				item["documentation"] = map[string]any{"kind": "markdown", "value": documentation}
			}
			if entry.Deprecated != nil {
				item["tags"] = []int{1}
			}
			add(entry.Name, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		left, leftOK := items[i]["label"].(string)
		right, rightOK := items[j]["label"].(string)
		if !leftOK || !rightOK {
			return false
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})
	return items
}

func completionSymbolKind(kind symbol.Kind) int {
	switch {
	case kind.IsCallable():
		return 3
	case kind == symbol.KindConstant:
		return 21
	case kind == symbol.KindEnum:
		return 13
	default:
		return 6
	}
}

func completionAPIKind(kind pawnapi.Kind) int {
	switch kind {
	case pawnapi.KindNative, pawnapi.KindCallback, pawnapi.KindFunction:
		return 3
	case pawnapi.KindConstant, pawnapi.KindDefine:
		return 21
	case pawnapi.KindTag:
		return 25
	default:
		return 1
	}
}

func apiDocumentation(entry pawnapi.Entry) string {
	parts := make([]string, 0, 2)
	if entry.DocumentationSummary != "" {
		parts = append(parts, entry.DocumentationSummary)
	}
	if entry.DocumentationURL != "" {
		parts = append(parts, "[Read the documentation]("+entry.DocumentationURL+")")
	}
	return strings.Join(parts, "\n\n")
}

func macroSignature(macro preprocess.Macro) string {
	parameters := make([]string, macro.ParamCount)
	for name, index := range macro.NamedParams {
		if index >= 0 && index < len(parameters) {
			parameters[index] = name
		}
	}
	for index := range parameters {
		if parameters[index] == "" {
			parameters[index] = "%" + strconv.Itoa(index)
		}
	}
	return macro.Name + "(" + strings.Join(parameters, ", ") + ")"
}
