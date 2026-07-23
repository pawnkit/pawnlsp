package lsp

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	parser "github.com/pawnkit/pawn-parser"
	projectinclude "github.com/pawnkit/pawn-project/include"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

type textPositionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position position `json:"position"`
}

type completionData struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri,omitempty"`
	Source string `json:"source,omitempty"`
	Name   string `json:"name"`
	Start  int    `json:"start,omitempty"`
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
	if context, ok := includeCompletionAt(doc.Text, int(offset)); ok {
		return s.respond(id, includeCompletionItems(doc, context))
	}
	if context, ok := directiveCompletionAt(doc.Text, int(offset)); ok {
		return s.respond(id, directiveCompletionItems(doc.Text, context))
	}
	prefix, _, _ := identifierAt(doc.Text, int(offset))
	items := completionItems(doc, prefix, int(offset))
	items = s.workspaceCompletionItems(items, prefix)
	return s.respond(id, items)
}

type includeCompletionContext struct {
	Prefix string
	Angle  bool
	Start  int
	End    int
}

type includeCandidateProvider interface {
	Complete(fromURI, prefix string, angle bool, limit int) []projectinclude.Candidate
}

func includeCompletionAt(text []byte, offset int) (includeCompletionContext, bool) {
	if offset < 0 || offset > len(text) {
		return includeCompletionContext{}, false
	}
	lineStart := bytes.LastIndexByte(text[:offset], '\n') + 1
	line := strings.TrimLeft(string(text[lineStart:offset]), " \t")
	if !strings.HasPrefix(line, "#include") && !strings.HasPrefix(line, "#tryinclude") {
		return includeCompletionContext{}, false
	}
	opening := strings.IndexAny(line, `<"`)
	if opening < 0 {
		return includeCompletionContext{}, false
	}
	prefix := line[opening+1:]
	if strings.ContainsAny(prefix, ">\"\r\n") {
		return includeCompletionContext{}, false
	}
	start := lineStart + len(string(text[lineStart:offset])) - len(line) + opening + 1
	return includeCompletionContext{Prefix: prefix, Angle: line[opening] == '<', Start: start, End: offset}, true
}

func includeCompletionItems(doc *document, context includeCompletionContext) []map[string]any {
	provider := doc.Candidates
	if provider == nil {
		return []map[string]any{}
	}
	candidates := provider.Complete(doc.URI, context.Prefix, context.Angle, 200)
	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		kind, detail := 17, "include file"
		if candidate.Directory {
			kind, detail = 19, "include directory"
		}
		items = append(items, map[string]any{
			"label": candidate.Path, "kind": kind, "detail": detail,
			"textEdit": textEdit{Range: offsetRange(doc.Text, context.Start, context.End), NewText: candidate.Path},
		})
	}
	return items
}

func includeCandidates(resolver preprocess.IncludeResolver) includeCandidateProvider {
	provider, _ := resolver.(includeCandidateProvider)
	return provider
}

type directiveCompletionContext struct {
	Prefix string
	Start  int
	End    int
}

func directiveCompletionAt(text []byte, offset int) (directiveCompletionContext, bool) {
	if offset < 0 || offset > len(text) {
		return directiveCompletionContext{}, false
	}
	lineStart := bytes.LastIndexByte(text[:offset], '\n') + 1
	line := string(text[lineStart:offset])
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	line = line[indent:]
	if !strings.HasPrefix(line, "#") || strings.ContainsAny(line[1:], " \t") {
		return directiveCompletionContext{}, false
	}
	return directiveCompletionContext{Prefix: line[1:], Start: lineStart + indent + 1, End: offset}, true
}

func directiveCompletionItems(text []byte, context directiveCompletionContext) []map[string]any {
	directives := []struct{ name, detail string }{
		{"include", "Include a required file"},
		{"tryinclude", "Include a file when available"},
		{"define", "Define a macro"},
		{"undef", "Remove a macro definition"},
		{"if", "Start a conditional block"},
		{"elseif", "Add a conditional branch"},
		{"else", "Add a fallback branch"},
		{"endif", "End a conditional block"},
		{"error", "Stop with a compiler error"},
		{"warning", "Emit a compiler warning"},
		{"assert", "Check a compile-time condition"},
		{"pragma", "Set a compiler option"},
	}
	items := make([]map[string]any, 0, len(directives))
	for _, directive := range directives {
		if !strings.HasPrefix(directive.name, strings.ToLower(context.Prefix)) {
			continue
		}
		items = append(items, map[string]any{
			"label": "#" + directive.name, "kind": 14, "detail": directive.detail,
			"textEdit": textEdit{Range: offsetRange(text, context.Start, context.End), NewText: directive.name},
		})
	}
	return items
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
					"data":     completionData{Kind: "local", URI: doc.URI, Source: symbolSourceURI(doc.Analysis, item), Name: item.Name, Start: int(item.Span.Start)},
				}
				add(item.Name, candidate)
			}
		}
		if doc.Analysis.Preprocess != nil {
			for _, macro := range doc.Analysis.Preprocess.Macros {
				detail := "macro"
				kind := 21
				if macro.Kind == preprocess.MacroFunctionLike {
					detail = macroSignature(macro)
					kind = 3
				}
				add(macro.Name, map[string]any{
					"kind": kind, "detail": detail, "sortText": "1_" + strings.ToLower(macro.Name),
					"data": completionData{Kind: "macro", URI: doc.URI, Name: macro.Name},
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
				"data":     completionData{Kind: "api", URI: doc.URI, Name: entry.Name},
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

func (s *server) resolveCompletion(id, raw json.RawMessage) error {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	encoded, err := json.Marshal(item["data"])
	if err != nil {
		return s.respond(id, item)
	}
	var data completionData
	if err := json.Unmarshal(encoded, &data); err != nil {
		return s.respond(id, item)
	}
	documentation := s.completionDocumentation(data)
	if documentation != "" {
		item["documentation"] = map[string]any{"kind": "markdown", "value": documentation}
	}
	if detail := s.completionDetail(data); detail != "" {
		item["detail"] = detail
	}
	return s.respond(id, item)
}

func (s *server) completionDetail(data completionData) string {
	switch data.Kind {
	case "local":
		doc := s.readyDocument(data.URI)
		if doc != nil && doc.Analysis != nil {
			if item, ok := completionSymbol(doc.Analysis, data); ok {
				return localDeclaration(doc.Analysis, item)
			}
		}
	case "workspace":
		for _, occurrence := range s.workspaceOccurrences(data.Name) {
			if occurrence.declaration && occurrence.uri.String() == data.URI && int(occurrence.span.Start) == data.Start {
				return declarationText(occurrence.text, occurrence.span)
			}
		}
	}
	return ""
}

func (s *server) completionDocumentation(data completionData) string {
	switch data.Kind {
	case "local":
		doc := s.readyDocument(data.URI)
		if doc == nil || doc.Analysis == nil {
			return ""
		}
		if item, ok := completionSymbol(doc.Analysis, data); ok {
			return localDocumentation(doc.Analysis, item)
		}
	case "macro":
		doc := s.readyDocument(data.URI)
		if doc != nil && doc.Analysis != nil && doc.Analysis.Preprocess != nil {
			if macro, ok := doc.Analysis.Preprocess.Macros[data.Name]; ok {
				return macroHover(doc.Analysis.Preprocess, macro)
			}
		}
	case "api":
		doc := s.readyDocument(data.URI)
		if doc != nil {
			if entry, ok := apiEntry(doc.Names, data.Name); ok {
				return apiDocumentation(entry)
			}
		}
	case "workspace":
		for _, occurrence := range s.workspaceOccurrences(data.Name) {
			if occurrence.declaration && occurrence.uri.String() == data.URI && int(occurrence.span.Start) == data.Start {
				return declarationDocumentation(occurrence.text, occurrence.span)
			}
		}
	}
	return ""
}

func completionSymbol(result *analysis.Result, data completionData) (symbol.Symbol, bool) {
	if table := navigationTable(result); table != nil {
		for _, item := range table.Symbols {
			if item.Name == data.Name && int(item.Span.Start) == data.Start && (data.Source == "" || symbolSourceURI(result, item) == data.Source) {
				return item, true
			}
		}
	}
	return symbol.Symbol{}, false
}

func symbolSourceURI(result *analysis.Result, item symbol.Symbol) string {
	if result == nil || result.Registry == nil {
		return ""
	}
	uri, _ := result.Registry.URI(item.Span.File)
	return uri.String()
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
