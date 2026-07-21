package lsp

import (
	"bytes"
	"strings"
	"testing"

	analysis "github.com/pawnkit/pawn-analysis"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestServerReturnsCallHierarchy(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Target() {}\nstock Caller() { Target(); }\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/prepareCallHierarchy", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 7},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "callHierarchy/outgoingCalls", "params": map[string]any{
		"item": map[string]any{"data": map[string]any{"name": "Caller", "uri": uri}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "callHierarchy/incomingCalls", "params": map[string]any{
		"item": map[string]any{"data": map[string]any{"name": "Target", "uri": uri}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"callHierarchyProvider", `"name":"Caller"`, `"name":"Target"`, `"to":`, `"from":`, `"fromRanges":`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("call hierarchy missing %q: %s", value, output.String())
		}
	}
}

func TestCallableDeclarationsPreferImplementation(t *testing.T) {
	t.Parallel()

	uri := coresource.FileURI("test.pwn")
	result := analysis.Analyze([]byte("forward Work();\nstock Work() {}\n"), analysis.Options{URI: uri})
	items := workspaceCallableDeclarations(map[coresource.URI]*analysis.Result{uri: result}, "Work")
	if len(items) != 1 || items[0].symbol.FuncScope == 0 {
		t.Fatalf("declarations = %+v", items)
	}
}
