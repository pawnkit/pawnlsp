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

func TestServerFormatsRangeAndOnType(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock First(){new value=1;}\nstock Second(){new value=2;}\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/rangeFormatting", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 1, "character": 0}, "end": map[string]any{"line": 1, "character": 28}},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/onTypeFormatting", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 28}, "ch": ";",
		"options": map[string]any{"tabSize": 4, "insertSpaces": true},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"documentRangeFormattingProvider", "documentOnTypeFormattingProvider", "new value = 2;"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("range formatting output missing %q: %s", value, output.String())
		}
	}
	if strings.Contains(output.String(), "new value = 1;") {
		t.Fatalf("range formatting changed the unselected function: %s", output.String())
	}
}

func TestServerReturnsInlayHints(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerPos(0, 1.0, 2.0, 3.0); }\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/inlayHint", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 1, "character": 0}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"inlayHintProvider", `"label":"playerid:"`, `"label":"x:"`, `"label":"y:"`, `"label":"z:"`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("inlay hint output missing %q: %s", value, output.String())
		}
	}
}

func TestParameterName(t *testing.T) {
	for input, want := range map[string]string{
		"const &Float:position[3]": "position",
		"count = 1":                "count",
		"values...":                "values",
	} {
		if got := parameterName(input); got != want {
			t.Fatalf("parameterName(%q) = %q, want %q", input, got, want)
		}
	}
}
