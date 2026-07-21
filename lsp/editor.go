package lsp

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/pawnkit/pawn-analysis/symbol"
	parser "github.com/pawnkit/pawn-parser"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func (s *server) documentHighlights(id, raw json.RawMessage) error {
	var params textPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, []any{})
	}
	table := navigationTable(doc.Analysis)
	item, found := symbolAt(table, offset)
	name, _, _ := identifierAt(doc.Text, int(offset))
	global := !found
	if found {
		name = item.Name
		if scope, ok := table.Scope(item.Scope); ok {
			global = scope.Kind == symbol.ScopeFile
		}
	}
	highlights := make([]map[string]any, 0)
	if global {
		for _, occurrence := range s.workspaceOccurrences(name) {
			if occurrence.uri.String() != doc.URI {
				continue
			}
			kind := 2
			if occurrence.declaration {
				kind = 1
			}
			highlights = append(highlights, map[string]any{
				"range": offsetRange(occurrence.text, int(occurrence.span.Start), int(occurrence.span.End)), "kind": kind,
			})
		}
		return s.respond(id, highlights)
	}
	highlights = append(highlights, map[string]any{
		"range": offsetRange(doc.Text, int(item.Span.Start), int(item.Span.End)), "kind": 1,
	})
	for _, reference := range table.References {
		if reference.Resolved == item.ID && reference.Span.File == doc.Analysis.File {
			highlights = append(highlights, map[string]any{
				"range": offsetRange(doc.Text, int(reference.Span.Start), int(reference.Span.End)), "kind": 2,
			})
		}
	}
	return s.respond(id, highlights)
}

func (s *server) foldingRanges(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil || doc.Analysis == nil || doc.Analysis.Parse == nil {
		return s.respond(id, []any{})
	}
	index := coresource.NewLineIndex(string(doc.Text))
	ranges := make([]map[string]any, 0)
	seen := make(map[[2]int]bool)
	walkSyntax(doc.Analysis.Parse.Syntax(), func(node parser.SyntaxNode) {
		if !foldableKind(node.Kind()) {
			return
		}
		rng := node.Range()
		start, startErr := index.Position(coresource.Offset(rng.Start), coresource.UTF16)
		end, endErr := index.Position(coresource.Offset(rng.End), coresource.UTF16)
		if startErr != nil || endErr != nil || start.Line >= end.Line {
			return
		}
		key := [2]int{start.Line, end.Line}
		if seen[key] {
			return
		}
		seen[key] = true
		item := map[string]any{
			"startLine": start.Line, "startCharacter": start.Character,
			"endLine": end.Line, "endCharacter": end.Character,
		}
		if node.Kind() == parser.KindComment {
			item["kind"] = "comment"
		}
		ranges = append(ranges, item)
	})
	sort.Slice(ranges, func(i, j int) bool {
		left, _ := ranges[i]["startLine"].(int)
		right, _ := ranges[j]["startLine"].(int)
		return left < right
	})
	return s.respond(id, ranges)
}

func (s *server) selectionRanges(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Positions []position `json:"positions"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil || doc.Analysis == nil || doc.Analysis.Parse == nil {
		return s.respond(id, []any{})
	}
	index := coresource.NewLineIndex(string(doc.Text))
	items := make([]any, 0, len(params.Positions))
	for _, requested := range params.Positions {
		offset, err := index.Offset(coresource.Position{Line: requested.Line, Character: requested.Character}, coresource.UTF16)
		if err != nil {
			items = append(items, map[string]any{"range": lspRange{Start: requested, End: requested}})
			continue
		}
		path := syntaxPath(doc.Analysis.Parse.Syntax(), int(offset))
		var parent any
		for _, node := range slices.Backward(path) {
			rng := node.Range()
			item := map[string]any{"range": offsetRange(doc.Text, rng.Start, rng.End)}
			if parent != nil {
				item["parent"] = parent
			}
			parent = item
		}
		if parent == nil {
			parent = map[string]any{"range": lspRange{Start: requested, End: requested}}
		}
		items = append(items, parent)
	}
	return s.respond(id, items)
}

func walkSyntax(root parser.SyntaxNode, visit func(parser.SyntaxNode)) {
	if !root.Valid() {
		return
	}
	visit(root)
	children := root.Children()
	for children.Next() {
		walkSyntax(children.Node(), visit)
	}
}

func syntaxPath(root parser.SyntaxNode, offset int) []parser.SyntaxNode {
	if !root.Valid() {
		return nil
	}
	rng := root.Range()
	if offset < rng.Start || offset > rng.End {
		return nil
	}
	children := root.Children()
	for children.Next() {
		if path := syntaxPath(children.Node(), offset); len(path) > 0 {
			return append(path, root)
		}
	}
	return []parser.SyntaxNode{root}
}

func foldableKind(kind parser.Kind) bool {
	switch kind {
	case parser.KindComment, parser.KindConditionalRegion, parser.KindConditionalBranch,
		parser.KindFunctionDefinition, parser.KindEnumDeclaration, parser.KindBlock,
		parser.KindSwitchStatement, parser.KindCaseClause, parser.KindDefaultClause:
		return true
	default:
		return false
	}
}
