package lsp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestServerReturnsCompletionItems(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "#define PROJECT_NAME \"test\"\nstock Helper() {}\nmain() { SetP }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 13},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"completionProvider", `"label":"SetPlayerPos"`, "native bool:SetPlayerPos"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("completion output missing %q: %s", value, output.String())
		}
	}
	if strings.Contains(output.String(), `"label":"Helper"`) || strings.Contains(output.String(), `"label":"PROJECT_NAME"`) {
		t.Fatalf("completion ignored the typed prefix: %s", output.String())
	}
}

func TestCompletionIncludesLocalSymbolsAndMacros(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "#define PROJECT_NAME \"test\"\nstock Helper() {}\nmain() {}"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 8},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"Helper"`, `"label":"PROJECT_NAME"`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("completion output missing %q: %s", value, output.String())
		}
	}
}

func TestCompletionIncludesLocalDocumentation(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "// Adds two values.\nstock Add(left, right) { return left + right; }\nmain() { Ad }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 11},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"value":"Adds two values."`) {
		t.Fatalf("completion documentation missing: %s", output.String())
	}
}

func TestCompletionKeepsLocalsInTheirFunction(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock First() { new hidden; }\nstock Second() { new visible;  }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 30},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"label":"visible"`) || !strings.Contains(output.String(), `"sortText":"0_visible"`) {
		t.Fatalf("visible local missing: %s", output.String())
	}
	if strings.Contains(output.String(), `"label":"hidden"`) {
		t.Fatalf("completion leaked a local from another function: %s", output.String())
	}
}

func TestDeclarationDocumentation(t *testing.T) {
	t.Parallel()

	text := []byte("/**\n * Adds two values.\n */\nstock Add(left, right);\n")
	start := bytes.Index(text, []byte("stock Add")) + len("stock ")
	got := declarationDocumentation(text, coresource.Span{Start: coresource.Offset(start), End: coresource.Offset(start + 3)})
	if got != "Adds two values." {
		t.Fatalf("documentation = %q", got)
	}
}

func TestAPIDocumentationIncludesUsageDetails(t *testing.T) {
	t.Parallel()

	got := apiDocumentation(pawnapi.Entry{
		DocumentationSummary: "Changes the player's score.",
		OwningInclude:        "a_samp",
		Constraints:          []string{"The player must be connected."},
		Signature:            &pawnapi.Signature{ReturnSemantics: "True when the score was changed."},
	})
	for _, want := range []string{"Changes the player's score.", "Include: `a_samp`", "The player must be connected.", "**Returns:** True"} {
		if !strings.Contains(got, want) {
			t.Fatalf("API documentation missing %q: %s", want, got)
		}
	}
}

func TestServerReturnsSignatureHelp(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerPos(0, 1.0, ); }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/signatureHelp", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 30},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"signatureHelpProvider", "native bool:SetPlayerPos(playerid, Float:x, Float:y, Float:z)",
		`"activeParameter":2`, `"label":"Float:y"`,
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("signature output missing %q: %s", value, output.String())
		}
	}
}

func TestActiveParameterIgnoresNestedCallsAndStrings(t *testing.T) {
	text := []byte(`Call(Other(1, 2), "a,b", value`)
	name, opening, ok := callAt(text, len(text))
	if !ok || name != "Call" {
		t.Fatalf("call = %q, %v", name, ok)
	}
	if got := activeParameter(text, opening+1, len(text)); got != 2 {
		t.Fatalf("active parameter = %d", got)
	}
}

func TestServerReturnsSemanticTokens(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Add(left, right) { return left + right; }\nmain() { return SetPlayerScore(0, Add(20, 22)); }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/semanticTokens/full", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"semanticTokensProvider", `"tokenTypes":["function","variable","parameter"`, `"data":[0,6,3,0,1`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("semantic token output missing %q: %s", value, output.String())
		}
	}
}

func TestSemanticClassifications(t *testing.T) {
	tokenType, modifiers := semanticSymbol(symbol.Symbol{Kind: symbol.KindConstant})
	if tokenType != semanticVariable || modifiers&modifierReadonly == 0 {
		t.Fatalf("constant classification = %d, %d", tokenType, modifiers)
	}
	tokenType, modifiers = semanticAPI(pawnapi.Entry{Kind: pawnapi.KindNative, Deprecated: &pawnapi.Deprecation{}})
	if tokenType != semanticFunction || modifiers&modifierDefaultLibrary == 0 || modifiers&modifierDeprecated == 0 {
		t.Fatalf("API classification = %d, %d", tokenType, modifiers)
	}
}
