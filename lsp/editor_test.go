package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
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

func TestRangeFormattingDoesNotWaitForAnalysis(t *testing.T) {
	uri := "file:///main.pwn"
	text := []byte("main(){new value=1;}\n")
	doc := &document{URI: uri, Text: text, analysisReady: make(chan struct{})}
	var output bytes.Buffer
	s := &server{out: &output, documents: map[string]*document{uri: doc}}
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 1, "character": 0},
		},
		"options": map[string]any{"tabSize": 4, "insertSpaces": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.rangeFormatting(json.RawMessage("1"), params)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("range formatting waited for analysis")
	}
}

func TestRangeFormattingSpansTopLevelDeclarations(t *testing.T) {
	uri := "file:///main.pwn"
	text := []byte("stock First(){new first=1;}\nstock Second(){new second=2;}\n")
	doc := &document{URI: uri, Text: text}
	var output bytes.Buffer
	s := &server{out: &output, documents: map[string]*document{uri: doc}}
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 2, "character": 0},
		},
		"options": map[string]any{"tabSize": 4, "insertSpaces": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.rangeFormatting(json.RawMessage("1"), params); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"new first = 1;", "new second = 2;"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("range output missing %q: %s", value, output.String())
		}
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

func TestServerReturnsInlayHintForMatchingArgumentName(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Save(playerid) { return playerid; }\nmain() { new playerid; Save(playerid); }\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/inlayHint", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 1, "character": 0}, "end": map[string]any{"line": 2, "character": 0}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"label":"playerid:"`) {
		t.Fatalf("matching parameter hint missing: %s", output.String())
	}
}

func TestServerUsesForwardedMacroSignatureForInlayHints(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Dialog_Open(playerid, const function[], style, const caption[]) { return style; }\n#define PlayerDialog_Show(%0,%1, \\\n Dialog_Open(%0,#%1,\nmain() { PlayerDialog_Show(0, Menu, 2, \"Title\"); }\n"
	result := analysis.Analyze([]byte(text), analysis.Options{})
	signature, ok := (&server{}).callSignature(&document{Analysis: result}, "PlayerDialog_Show")
	if !ok || len(signature.Parameters) != 4 || parameterName(signature.Parameters[0]) != "playerid" {
		t.Fatalf("forwarded signature = %#v, %v", signature, ok)
	}
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/inlayHint", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 3, "character": 0}, "end": map[string]any{"line": 4, "character": 0}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{`"label":"playerid:"`, `"label":"function:"`, `"label":"style:"`, `"label":"caption:"`} {
		if !strings.Contains(output.String(), label) {
			t.Fatalf("forwarded macro hint missing %s: %s", label, output.String())
		}
	}
}

func TestServerReturnsPawnColors(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "new red = 0xFF0000;\nnew translucent = 0x00FF0080;\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/documentColor", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/colorPresentation", "params": map[string]any{
		"color": map[string]any{"red": 1, "green": 0, "blue": 0, "alpha": 1},
		"range": map[string]any{"start": map[string]any{"line": 0, "character": 10}, "end": map[string]any{"line": 0, "character": 18}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"colorProvider":true`, `"red":1`, `"green":1`, `"alpha":0.5019607843137255`, `"label":"0xFF0000"`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("color output missing %q: %s", value, output.String())
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
