package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/query"
	"github.com/pawnkit/pawn-analysis/sema"
	"github.com/pawnkit/pawn-api/pawnapi"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintrules "github.com/pawnkit/pawnlint/pkg/rules"
)

func TestDidChangeRejectsStaleVersion(t *testing.T) {
	uri := tempDocumentURI(t)
	doc := &document{URI: uri, Path: "/tmp/test.pwn", Text: []byte("main() {}"), Version: 2}
	s := &server{
		documents: map[string]*document{uri: doc},
		snapshot:  query.New(query.Document{URI: coresource.URI(uri), Text: doc.Text, Version: 2}),
	}
	params, _ := json.Marshal(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 1},
		"contentChanges": []map[string]any{{"text": "changed"}},
	})
	if err := s.didChange(params); err != nil {
		t.Fatal(err)
	}
	if string(doc.Text) != "main() {}" || doc.Version != 2 {
		t.Fatalf("stale change applied: %+v", doc)
	}
}

func TestServerReturnsDiagnosticsAndFixes(t *testing.T) {
	uri := tempDocumentURI(t)
	source := "main() { if (true); { return; } }\n"
	messages := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": source}}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{"textDocument": map[string]any{"uri": uri}}},
		map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/codeAction", "params": map[string]any{"textDocument": map[string]any{"uri": uri}}},
		map[string]any{"jsonrpc": "2.0", "id": 4, "method": "shutdown", "params": nil},
		map[string]any{"jsonrpc": "2.0", "method": "exit"},
	}
	var input bytes.Buffer
	for _, value := range messages {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n", len(body))
		_, _ = input.Write(body)
	}
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, fragment := range []string{"textDocumentSync", "diagnosticProvider", "empty-condition-body", "remove the stray semicolon", "Suppress empty-condition-body", "Explain empty-condition-body", "quickfix"} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("output does not contain %q: %s", fragment, got)
		}
	}
	if strings.Contains(got, "textDocument/publishDiagnostics") {
		t.Fatalf("server returned push diagnostics: %s", got)
	}
}

func TestServerReturnsRelatedDiagnosticLocations(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "new value;\nnew value;\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"relatedInformation", "previous declaration", uri} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("related diagnostic location missing %q: %s", value, output.String())
		}
	}
}

func TestServerFormatsDocument(t *testing.T) {
	uri := tempDocumentURI(t)
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": "main(){new value=1;}\n"},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/formatting", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"documentFormattingProvider", "newText", "new value = 1"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("format output missing %q: %s", value, output.String())
		}
	}
}

func TestServerFormatsBackslashInclude(t *testing.T) {
	uri := tempDocumentURI(t)
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": "#include <YSI_Server\\y_flooding>\nmain(){return 1;}\n"},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/formatting", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": false},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "does not parse cleanly") || !strings.Contains(output.String(), `YSI_Server\\y_flooding`) {
		t.Fatalf("formatting failed: %s", output.String())
	}
}

func TestServerPublishesSharedAnalysisDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		text string
		code string
	}{
		{"argument count", "Helper(a, b) {} main() { Helper(1); }", "pawn-analysis:sema/argument-count"},
		{"not callable", "main() { new value; value(); }", "pawn-analysis:sema/not-callable"},
		{"tag mismatch", "Float:Get() { return bool:true; }", "pawn-analysis:sema/tag-mismatch"},
		{"unreachable", "main() { return; new value; }", "pawn-analysis:sema/unreachable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uri := tempDocumentURI(t)
			var input bytes.Buffer
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
				"textDocument": map[string]any{"uri": uri, "version": 1, "text": test.text},
			}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
				"textDocument": map[string]any{"uri": uri},
			}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

			var output bytes.Buffer
			if err := Run(&input, &output); err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(output.String(), test.code); count != 1 {
				t.Fatalf("shared diagnostic %s count = %d: %s", test.code, count, output.String())
			}
		})
	}
}

func TestServerOmitsResolvedAnalysisFalsePositives(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "new Iterator:values[10]<20>;\nvoid:Reset() { values[0] = 0; }\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"pawn-analysis:sema/missing-return", "pawn-analysis:sema/state-variable-shadow", "pawn-analysis:sema/invalid-state-variable"} {
		if strings.Contains(output.String(), code) {
			t.Fatalf("unexpected diagnostic %s: %s", code, output.String())
		}
	}
}

func TestServerReturnsWorkspaceDiagnostics(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.pwn")
	brokenPath := filepath.Join(root, "broken.pwn")
	unrelatedPath := filepath.Join(root, "unrelated.pwn")
	dependencyPath := filepath.Join(root, "dependencies", "stdlib", "open.mp.inc")
	if err := os.MkdirAll(filepath.Dir(dependencyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"main.pwn"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dependencyPath, []byte("#define OPEN_MP\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brokenPath, []byte("#include <open.mp>\nstock Value;\nstock Value;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedPath, []byte("#include <not-installed>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := coresource.FileURI(mainPath).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": "#include \"broken.pwn\"\nmain() {}\n"},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "workspace/diagnostic", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"diagnosticProvider", "workspaceDiagnostics", coresource.FileURI(brokenPath).String(), "pawn-analysis:symbol/redeclared", "docs/diagnostics.md"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("workspace diagnostics missing %q: %s", value, output.String())
		}
	}
	if strings.Contains(output.String(), "not-installed") {
		t.Fatalf("workspace diagnostics included an inactive source: %s", output.String())
	}
	if strings.Contains(output.String(), "include-not-found") {
		t.Fatalf("workspace diagnostics ignored the active include graph: %s", output.String())
	}
	if strings.Contains(output.String(), coresource.FileURI(dependencyPath).String()) {
		t.Fatalf("workspace diagnostics included a dependency: %s", output.String())
	}
}

func TestWorkspaceAndOpenDiagnosticsAcceptTagMacros(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.pwn")
	helperPath := filepath.Join(root, "helper.inc")
	targetPath := filepath.Join(root, "target.inc")
	files := map[string]string{
		helperPath: "#define AnyTag {_, bool, Float}\n#define HandleTag {Handle}\nnative Accept(AnyTag:value);\nnative UseHandle(HandleTag:value);\n",
		targetPath: "stock Check() { Accept(String:1); UseHandle(Handle:1); }\n",
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(mainPath).String(), "version": 1, "text": "main() {}\n"},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "workspace/diagnostic", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(targetPath).String(), "version": 1, "text": files[targetPath]},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(targetPath).String()},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "pawn-analysis:sema/tag-mismatch") {
		t.Fatalf("tag macro produced inconsistent diagnostics: %s", output.String())
	}
}

func TestServerReturnsSharedDocumentSymbols(t *testing.T) {
	uri := tempDocumentURI(t)
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": "new counter;\nstock Helper(value) { return value; }"},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/documentSymbol", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"documentSymbolProvider", "counter", "Helper", "value"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "main.pwn")
	other := filepath.Join(root, "filterscripts", "admin.pwn")
	for _, path := range []string{entry, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"gamemodes/main.pwn","preset":"openmp"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mainText := "main() { return RemoteHelper(1); }\n"
	if err := os.WriteFile(entry, []byte(mainText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("stock RemoteHelper(playerid) { return playerid; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := coresource.FileURI(entry).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": mainText},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "workspace/symbol", "params": map[string]any{"query": "remote"}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/references", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 20},
		"context": map[string]any{"includeDeclaration": true},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "textDocument/prepareRename", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 20},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 5, "method": "textDocument/rename", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 20}, "newName": "GlobalHelper",
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"workspaceSymbolProvider", "RemoteHelper", "filterscripts/admin.pwn", coresource.FileURI(other).String()} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("workspace symbol output missing %q: %s", value, output.String())
		}
	}
	if !strings.Contains(output.String(), `"placeholder":"RemoteHelper"`) {
		t.Fatalf("prepare rename output missing: %s", output.String())
	}
	if count := strings.Count(output.String(), `"newText":"GlobalHelper"`); count != 2 {
		t.Fatalf("rename edit count = %d: %s", count, output.String())
	}
}

func TestServerReturnsSharedDefinition(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Helper() { return 1; }\nmain() { return Helper(); }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/definition", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 17},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"definitionProvider", `"line":0`, `"character":6`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsIncludeDefinition(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "main.pwn")
	include := filepath.Join(root, "includes", "helper.inc")
	for _, dir := range []string{filepath.Dir(entry), filepath.Dir(include)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"entry":"gamemodes/main.pwn","include_path":"includes","preset":"openmp","pawnkit":{"schemaVersion":1}}`
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	text := "#include <helper>\nmain() { return Helper(); }"
	if err := os.WriteFile(include, []byte("Helper() { return 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := coresource.FileURI(entry).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/definition", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 17},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/definition", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 12},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 12},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	want := coresource.FileURI(include).String()
	if count := strings.Count(output.String(), want); count < 2 {
		t.Fatalf("include definition URI %q missing: %s", want, output.String())
	}
	for _, value := range []string{"```pawn", "helper", "Resolved file:"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("include hover missing %q: %s", value, output.String())
		}
	}
}

func TestServerResolvesProjectAndExtraIncludes(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "main.pwn")
	projectInclude := filepath.Join(root, "include", "math.inc")
	extraRoot := filepath.Join(root, "managed")
	extraInclude := filepath.Join(extraRoot, "pawntest.inc")
	for _, path := range []string{entry, projectInclude, extraInclude} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{
  "entry": "gamemodes/main.pwn",
  "preset": "openmp",
  "pawnkit": {"schemaVersion": 1, "includePaths": ["include"]}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectInclude, []byte("stock Add(a, b) { return a + b; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraInclude, []byte("#define TEST(%0) \\\n    forward test_%0(); \\\n    public test_%0()\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	text := "#include <math>\n#include <pawntest>\nTEST(one) { return Add(20, 22); }\nTEST(two) { return Add(2, 3); }\n"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"initializationOptions": map[string]any{"pawnkit": map[string]any{
			"protocolVersion": 1, "managedIncludeRoots": []string{extraRoot},
		}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(entry).String(), "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/documentSymbol", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(entry).String()},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(entry).String()},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "include-not-found") || strings.Contains(output.String(), "missing-include") {
		t.Fatalf("resolved include reported missing: %s", output.String())
	}
	if strings.Contains(output.String(), "duplicate-function-definition") || strings.Contains(output.String(), "symbol/redeclared") {
		t.Fatalf("macro invocation reported as a duplicate: %s", output.String())
	}
}

func TestManagedToolStateUpdate(t *testing.T) {
	root := t.TempDir()
	s := &server{documents: make(map[string]*document)}
	raw, err := json.Marshal(map[string]any{
		"protocolVersion": 1, "managedIncludeRoots": []string{root, root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.didChangeManagedTools(raw); err != nil {
		t.Fatal(err)
	}
	if len(s.managedRoots) != 1 || s.managedRoots[0] != filepath.Clean(root) {
		t.Fatalf("managed roots = %v", s.managedRoots)
	}
}

func TestManagedToolStateRejectsInvalidValues(t *testing.T) {
	if _, err := cleanManagedIncludeRoots([]string{"relative/include"}); err == nil {
		t.Fatal("relative root was accepted")
	}
	roots := make([]string, managedIncludeRootLimit+1)
	for index := range roots {
		roots[index] = filepath.Join(t.TempDir(), strconv.Itoa(index))
	}
	if _, err := cleanManagedIncludeRoots(roots); err == nil {
		t.Fatal("oversized root list was accepted")
	}
}

func TestServerAcceptsLegacyManagedIncludeOptions(t *testing.T) {
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"initializationOptions": map[string]any{"includePaths": []string{t.TempDir()}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"error"`) {
		t.Fatalf("legacy options failed: %s", output.String())
	}
}

func TestManagedIncludesSurviveDocumentLifecycle(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.pwn")
	managed := filepath.Join(t.TempDir(), "include")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"main.pwn"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, "pawntest.inc"), []byte("#define TEST(%0) forward test_%0(); public test_%0()\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := coresource.FileURI(entry).String()
	text := "#include <pawntest>\nTEST(example) { return 1; }\n"

	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"initializationOptions": map[string]any{"pawnkit": map[string]any{
			"protocolVersion": 1, "managedIncludeRoots": []string{managed},
		}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": text + "\n"}},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "workspace/didChangeWatchedFiles", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 3, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "include-not-found") || strings.Contains(output.String(), "missing-include") {
		t.Fatalf("managed include was lost: %s", output.String())
	}
}

func TestServerResolvesNestedQuotedIncludesAndSparseMacroLabels(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "gamemode.pwn")
	module := filepath.Join(root, "gamemodes", "modules", "player", "main.pwn")
	joining := filepath.Join(root, "gamemodes", "modules", "player", "joining.pwn")
	admin := filepath.Join(root, "gamemodes", "modules", "player", "admin.pwn")
	for _, path := range []string{entry, module, joining, admin} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"gamemodes/gamemode.pwn","preset":"openmp"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#include \"modules/player/main.pwn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{joining, admin} {
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	text := "#include \"modules/player/joining.pwn\"\n" +
		"#include \"modules/player/admin.pwn\"\n" +
		"#define PICK(%1) (%1)\n" +
		"main() { return PICK(3); }\n"

	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(module).String(), "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(module).String()},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"include-not-found", "missing-include", "macro-argument-mismatch"} {
		if strings.Contains(output.String(), code) {
			t.Fatalf("unexpected %s diagnostic: %s", code, output.String())
		}
	}
}

func TestServerReturnsSharedHover(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "stock Float:Measure(value, scale = 1) { return value; }\nmain() { return Measure(2); }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 17},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"hoverProvider", "```pawn\\nstock Float:Measure(value, scale = 1)\\n```"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsMacroHover(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "// Highest accepted value.\n#define LIMIT 42\nmain() { return LIMIT; }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 18},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "#define LIMIT 42") || !strings.Contains(output.String(), "Highest accepted value.") {
		t.Fatalf("macro hover missing: %s", output.String())
	}
}

func TestServerReturnsEnumMemberHover(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "enum SAMPLE_RECORD\n{\n    SAMPLE_ID,\n    bool:SAMPLE_ENABLED,\n    Float:SAMPLE_RATIO\n};\nmain() { return SAMPLE_ENABLED; }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 6, "character": 20},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "```pawn\\nbool:SAMPLE_ENABLED\\n```") {
		t.Fatalf("enum member hover missing: %s", got)
	}
	if strings.Contains(got, "Float:SAMPLE_RATIO") {
		t.Fatalf("enum member hover includes adjacent members: %s", got)
	}
}

func TestMacroDefinitionIncludesContinuationLines(t *testing.T) {
	t.Parallel()

	text := []byte("#define DOUBLE(%0) \\\n    ((%0) * 2)\nmain() {}\n")
	start := bytes.Index(text, []byte("#define"))
	end := bytes.Index(text, []byte("DOUBLE")) + len("DOUBLE")
	got := macroDefinition(text, preprocess.ByteRange{Start: start, End: end})
	if !strings.Contains(got, "((%0) * 2)") {
		t.Fatalf("macro definition = %q", got)
	}
}

func TestVariadicTagSetDeclaration(t *testing.T) {
	t.Parallel()

	text := []byte("native printf(const format[], {Float, _}:...);\n")
	start := bytes.Index(text, []byte("printf"))
	declaration := declarationText(text, coresource.Span{
		Start: coresource.Offset(start), End: coresource.Offset(start + len("printf")),
	})
	if declaration != "native printf(const format[], {Float, _}:...)" {
		t.Fatalf("declaration = %q", declaration)
	}
	parameters := declarationParameters(declaration)
	if len(parameters) != 2 || parameters[1] != "{Float, _}:..." {
		t.Fatalf("parameters = %q", parameters)
	}
}

func TestHoverKeepsIncludedFileSpansSeparate(t *testing.T) {
	root := t.TempDir()
	includeRoot := filepath.Join(root, "include")
	if err := os.MkdirAll(includeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mathSource := "// Multiplies two values.\nstock Multiply(left, right) { return left * right; }\n"
	playerSource := "#include <math>\n\n// Calculates the starting score.\nstock StartingScore(bonus) { return bonus; }\n"
	if err := os.WriteFile(filepath.Join(includeRoot, "math.inc"), []byte(mathSource), 0o600); err != nil {
		t.Fatal(err)
	}
	playerPath := filepath.Join(includeRoot, "player.inc")
	if err := os.WriteFile(playerPath, []byte(playerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{
  "entry": "include/player.inc",
  "preset": "openmp",
  "pawnkit": {"schemaVersion": 1, "includePaths": ["include"]}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	uri := coresource.FileURI(playerPath).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": playerSource},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 3, "character": 8},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Calculates the starting score.") || strings.Contains(output.String(), "Multiplies two values.") {
		t.Fatalf("hover used an included-file span: %s", output.String())
	}
}

func TestAPIHoverIncludesSignatureAndDocumentation(t *testing.T) {
	entry := pawnapi.Entry{
		Kind: pawnapi.KindNative,
		Name: "SetPlayerScore",
		Signature: &pawnapi.Signature{
			Parameters: []pawnapi.Parameter{{Name: "playerid"}, {Name: "score"}},
			ReturnTag:  "bool", ReturnSemantics: "True when the score was updated.",
		},
		DocumentationSummary: "Sets a player's score.",
		DocumentationURL:     "https://open.mp/docs/scripting/functions/SetPlayerScore",
	}
	hover := apiHover(entry)
	for _, value := range []string{
		"native bool:SetPlayerScore(playerid, score)",
		"Sets a player's score.",
		"**Returns:** True when the score was updated.",
		"[Read the documentation](https://open.mp/docs/scripting/functions/SetPlayerScore)",
	} {
		if !strings.Contains(hover, value) {
			t.Fatalf("hover missing %q: %s", value, hover)
		}
	}
}

func TestServerReturnsAPIHover(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerPos(0, 1.0, 2.0, 3.0); }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 12},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"native bool:SetPlayerPos(playerid, Float:x, Float:y, Float:z)",
		"https://open.mp/docs/scripting/functions/SetPlayerPos",
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsSharedReferences(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "new counter;\nmain() { counter++; return counter; }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/references", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 5},
		"context": map[string]any{"includeDeclaration": true},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "referencesProvider") {
		t.Fatalf("references capability missing: %s", output.String())
	}
	if count := strings.Count(output.String(), `"uri":"`+uri+`"`); count != 3 {
		t.Fatalf("reference count = %d: %s", count, output.String())
	}
}

func TestAPINameResolver(t *testing.T) {
	index, err := pawnapi.Load()
	if err != nil {
		t.Fatal(err)
	}
	resolver := apiNameResolver{index: index}
	if got := resolver.ResolveName("SetPlayerPos"); got != sema.NameFound {
		t.Fatalf("SetPlayerPos = %v", got)
	}
	if got := resolver.ResolveName("NotInTheSeedDataset"); got != sema.NameUnknown {
		t.Fatalf("unknown name = %v", got)
	}
	callable, ok := resolver.ResolveCallable("SetPlayerPos")
	if !ok || callable.MinArgs == 0 || callable.MaxArgs < callable.MinArgs {
		t.Fatalf("SetPlayerPos signature = %+v, ok=%v", callable, ok)
	}
	resolver.profile = "nonexistent-profile"
	if got := resolver.ResolveName("SetPlayerPos"); got != sema.NameUnknown {
		t.Fatalf("unavailable name = %v", got)
	}
}

func TestSafeFixRequiresKnownSafeRule(t *testing.T) {
	t.Parallel()

	registry := lintrules.Default()
	if !safeFix(registry, "empty-condition-body") {
		t.Fatal("known safe fix was rejected")
	}
	if safeFix(registry, "external/example/fix") {
		t.Fatal("unknown fix was accepted")
	}
}

func TestLoadProjectIncludes(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "main.pwn")
	include := filepath.Join(root, "includes", "helper.inc")
	for _, dir := range []string{filepath.Dir(entry), filepath.Dir(include)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"entry":"gamemodes/main.pwn","include_path":"includes","preset":"openmp","pawnkit":{"schemaVersion":1}}`
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#include <helper>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte("Helper() {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolver, profile := loadProjectIncludes(entry)
	if resolver == nil {
		t.Fatal("project resolver was not loaded")
	}
	if profile == "" {
		t.Fatal("project profile was not selected")
	}
	content, _, ok := resolver.Resolve(coresource.FileURI(entry).String(), "helper", true)
	if !ok || string(content) != "Helper() {}" {
		t.Fatalf("resolved=%v content=%q", ok, content)
	}
}

func TestLoadPawnKitIncludePaths(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "gamemodes", "main.pwn")
	include := filepath.Join(root, "include", "math.inc")
	for _, dir := range []string{filepath.Dir(entry), filepath.Dir(include)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"entry":"gamemodes/main.pwn","pawnkit":{"schemaVersion":1,"includePaths":["include"]}}`
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#include <math>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte("Add(left, right) { return left + right; }"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolver, _ := loadProjectIncludes(entry)
	if resolver == nil {
		t.Fatal("project resolver was not loaded")
	}
	content, _, ok := resolver.Resolve(coresource.FileURI(entry).String(), "math", true)
	if !ok || !strings.Contains(string(content), "Add") {
		t.Fatalf("resolved=%v content=%q", ok, content)
	}
}

func TestLoadProjectIncludesWithExtraRoots(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.pwn")
	extra := filepath.Join(t.TempDir(), "include")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"main.pwn"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#include <pawntest>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "pawntest.inc"), []byte("#define TEST(%0)"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolver, _ := loadProjectIncludes(entry, extra)
	content, _, ok := resolver.Resolve(coresource.FileURI(entry).String(), "pawntest", true)
	if !ok || !strings.Contains(string(content), "TEST") {
		t.Fatalf("resolved=%v content=%q", ok, content)
	}
}

func TestOffsetPositionUsesUTF16(t *testing.T) {
	source := []byte("a😀b\nç")
	position := offsetPosition(source, len("a😀"))
	if position.Line != 0 || position.Character != 3 {
		t.Fatalf("position = %+v", position)
	}
	position = offsetPosition(source, len("a😀b\nç"))
	if position.Line != 1 || position.Character != 1 {
		t.Fatalf("position = %+v", position)
	}
}

func TestOffsetPositionClampsInvalidUTF8Boundary(t *testing.T) {
	position := offsetPosition([]byte("a😀b"), 2)
	if position.Line != 0 || position.Character != 1 {
		t.Fatalf("position = %+v", position)
	}
}

func TestReadFrameRejectsMissingLength(t *testing.T) {
	if _, err := readFrame(bufioReader("Header: value\r\n\r\n")); err == nil {
		t.Fatal("missing length was accepted")
	}
}

func TestReadFrameRejectsOversizedLength(t *testing.T) {
	if _, err := readFrame(bufioReader("Content-Length: 999999999999\r\n\r\n")); err == nil {
		t.Fatal("oversized length was accepted")
	}
}

func TestReadFrameRejectsDuplicateLength(t *testing.T) {
	if _, err := readFrame(bufioReader("Content-Length: 1\r\nContent-Length: 1\r\n\r\nx")); err == nil {
		t.Fatal("duplicate length was accepted")
	}
}

func TestReadFrameRejectsLongHeader(t *testing.T) {
	header := "X-Test: " + strings.Repeat("x", 5000) + "\r\n\r\n"
	if _, err := readFrame(bufioReader(header)); err == nil {
		t.Fatal("long header was accepted")
	}
}

func TestServerSurvivesMalformedMessage(t *testing.T) {
	var input bytes.Buffer
	writeFrame(&input, `{"jsonrpc": "2.0", "id": 1, "method"`) // truncated/invalid JSON
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "initialize", "params": map[string]any{}})
	writeFrame(&input, `{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": {"textDocument": {}}}`) // bad params, uriPath fails
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatalf("Run returned error for malformed messages instead of continuing: %v", err)
	}
	if !strings.Contains(output.String(), "textDocumentSync") {
		t.Fatalf("server did not process the valid initialize request after a malformed one: %s", output.String())
	}
	if !strings.Contains(output.String(), `"code":-32700`) {
		t.Fatalf("parse error response missing: %s", output.String())
	}
}

func TestServerRespondsToInvalidRequestParams(t *testing.T) {
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": "https://example.test/main.pwn", "version": 1, "text": ""},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"id":2`) || !strings.Contains(output.String(), `"code":-32602`) {
		t.Fatalf("invalid params response missing: %s", output.String())
	}
}

func frame(t *testing.T, buf *bytes.Buffer, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(buf, string(body))
}

func writeFrame(buf *bytes.Buffer, body string) {
	fmt.Fprintf(buf, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func bufioReader(value string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(value))
}

func tempDocumentURI(t *testing.T) string {
	t.Helper()
	return coresource.FileURI(filepath.Join(t.TempDir(), "test.pwn")).String()
}
