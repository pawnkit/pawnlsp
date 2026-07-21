package lsp

import (
	"bytes"
	"strings"
	"testing"

	parser "github.com/pawnkit/pawn-parser"
)

func TestServerReturnsEditorRanges(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Add(left, right)\n{\n    new total = left + right;\n    return total;\n}\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/documentHighlight", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 9},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/foldingRange", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "textDocument/selectionRange", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "positions": []map[string]any{{"line": 2, "character": 21}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"documentHighlightProvider", "foldingRangeProvider", "selectionRangeProvider",
		`"startLine":0`, `"parent":{"range"`,
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("editor range output missing %q: %s", value, output.String())
		}
	}
	if count := strings.Count(output.String(), `"line":2,"character":8`); count < 2 {
		t.Fatalf("local declaration was not highlighted: %s", output.String())
	}
}

func TestFoldableKind(t *testing.T) {
	if !foldableKind(parser.KindBlock) {
		t.Fatal("blocks should fold")
	}
	if foldableKind(parser.KindIdentifier) {
		t.Fatal("identifiers should not fold")
	}
}
