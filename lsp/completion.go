package lsp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	parser "github.com/pawnkit/pawn-parser"
	coresource "github.com/pawnkit/pawnkit-core/source"
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
	items := completionItems(doc, prefix, int(offset))
	items = s.workspaceCompletionItems(items, prefix)
	return s.respond(id, items)
}

func completionItems(doc *document, prefix string, offset int) []map[string]any {
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
				if !completionSymbolVisible(doc, table, item, offset) {
					continue
				}
				candidate := map[string]any{
					"kind": completionSymbolKind(item.Kind), "detail": symbolSummary(item),
					"sortText": "0_" + strings.ToLower(item.Name),
				}
				if documentation := localDocumentation(doc.Analysis, item); documentation != "" {
					candidate["documentation"] = map[string]any{"kind": "markdown", "value": documentation}
				}
				add(item.Name, candidate)
			}
		}
		if doc.Analysis.Preprocess != nil {
			for _, macro := range doc.Analysis.Preprocess.Macros {
				detail := "macro"
				if macro.Kind == preprocess.MacroFunctionLike {
					detail = macroSignature(macro)
				}
				add(macro.Name, map[string]any{
					"kind": 14, "detail": detail, "sortText": "1_" + strings.ToLower(macro.Name),
				})
			}
		}
	}

	if resolver, ok := doc.Names.(apiNameResolver); ok && resolver.index != nil {
		for _, entry := range resolver.index.All() {
			if !resolver.available(entry) {
				continue
			}
			item := map[string]any{
				"kind": completionAPIKind(entry.Kind), "detail": apiDeclaration(entry),
				"sortText": "3_" + strings.ToLower(entry.Name),
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

	sort.Slice(items, func(i, j int) bool { return completionItemLess(items[i], items[j]) })
	return items
}

func completionSymbolVisible(doc *document, table *symbol.Table, item symbol.Symbol, offset int) bool {
	scope, ok := table.Scope(item.Scope)
	if !ok || scope.Kind == symbol.ScopeFile {
		return ok
	}
	if item.Span.Start > coresource.Offset(offset) || doc.Analysis == nil || doc.Analysis.Parse == nil {
		return false
	}
	function, ok := callableByName(table, containingFunctionName(doc.Analysis.Parse.Syntax(), offset))
	return ok && function.FuncScope != 0 && scopeWithin(table, item.Scope, function.FuncScope)
}

func containingFunctionName(root parser.SyntaxNode, offset int) string {
	for _, node := range syntaxPath(root, offset) {
		function, ok := parser.AsFunction(node)
		if !ok {
			continue
		}
		name, ok := function.Name()
		if ok {
			return name.Text()
		}
	}
	return ""
}

func completionItemLess(left, right map[string]any) bool {
	leftSort, _ := left["sortText"].(string)
	rightSort, _ := right["sortText"].(string)
	if leftSort != rightSort {
		return leftSort < rightSort
	}
	leftLabel, _ := left["label"].(string)
	rightLabel, _ := right["label"].(string)
	return strings.ToLower(leftLabel) < strings.ToLower(rightLabel)
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
	parts := make([]string, 0, 5)
	if entry.DocumentationSummary != "" {
		parts = append(parts, entry.DocumentationSummary)
	}
	if entry.OwningInclude != "" {
		parts = append(parts, "Include: `"+entry.OwningInclude+"`")
	}
	if entry.CallbackContext != "" {
		parts = append(parts, entry.CallbackContext)
	}
	if len(entry.Constraints) > 0 {
		parts = append(parts, "**Notes**\n\n- "+strings.Join(entry.Constraints, "\n- "))
	}
	if entry.Signature != nil && entry.Signature.ReturnSemantics != "" {
		parts = append(parts, "**Returns:** "+entry.Signature.ReturnSemantics)
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
