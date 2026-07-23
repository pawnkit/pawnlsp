package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestCompletionIncludesProjectPaths(t *testing.T) {
	root := t.TempDir()
	include := filepath.Join(root, "include", "YSI_Coding")
	if err := os.MkdirAll(include, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(include, "y_hooks.inc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	source := "#include <YSI_Coding/y"
	mainPath := filepath.Join(root, "main.pwn")
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"main.pwn","pawnkit":{"schemaVersion":1,"includePaths":["include"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, _, _ := loadProjectContext(mainPath)
	provider, ok := resolver.(includeCandidateProvider)
	if !ok || len(provider.Complete(coresource.FileURI(mainPath).String(), "YSI_Coding/y", true, 20)) != 1 {
		t.Fatalf("project include completion is unavailable: %T", resolver)
	}
	context, ok := includeCompletionAt([]byte(source), len(source))
	if !ok || context.Prefix != "YSI_Coding/y" {
		t.Fatalf("include context = %+v, %v", context, ok)
	}
	if items := includeCompletionItems(&document{URI: coresource.FileURI(mainPath).String(), Includes: resolver, Candidates: provider, Text: []byte(source)}, context); len(items) != 1 {
		t.Fatalf("include items = %+v", items)
	}
	uri := coresource.FileURI(mainPath).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": source},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": len(source)},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"YSI_Coding/y_hooks"`, `"newText":"YSI_Coding/y_hooks"`, `"detail":"include file"`} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("include completion missing %q: %s", value, output.String())
		}
	}
	if strings.Contains(output.String(), `"label":"SetPlayerPos"`) {
		t.Fatalf("include completion contains symbols: %s", output.String())
	}
}

func TestCompletionIncludesDirectives(t *testing.T) {
	uri := tempDocumentURI(t)
	source := "#inc"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": source},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": len(source)},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"label":"#include"`) || !strings.Contains(output.String(), `"newText":"include"`) {
		t.Fatalf("directive completion missing: %s", output.String())
	}
	if strings.Contains(output.String(), `"label":"#define"`) {
		t.Fatalf("directive completion ignored prefix: %s", output.String())
	}
}

func TestCompletionResponseCapsLargeLists(t *testing.T) {
	items := make([]map[string]any, maxCompletionItems+1)
	for index := range items {
		items[index] = map[string]any{"label": "item"}
	}
	response, ok := completionResponse(items).(map[string]any)
	if !ok || response["isIncomplete"] != true {
		t.Fatalf("response = %#v", response)
	}
	got, ok := response["items"].([]map[string]any)
	if !ok || len(got) != maxCompletionItems {
		t.Fatalf("items = %T, %d", response["items"], len(got))
	}
}

func TestCompletionPriority(t *testing.T) {
	project := map[string]any{"sortText": "2_helper"}
	api := map[string]any{"sortText": "3_helper"}
	if completionPriority(project) >= completionPriority(api) {
		t.Fatalf("project priority = %d, API priority = %d", completionPriority(project), completionPriority(api))
	}
}

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

func TestServerReturnsActorAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { CreateAct }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 18},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"CreateActor"`, "native CreateActor(skin, Float:x, Float:y, Float:z, Float:angle)"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("actor completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsCheckpointAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerRaceCheck }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 27},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"SetPlayerRaceCheckpoint"`, "CP_TYPE:type"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("checkpoint completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsDialogAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { ShowPlayerDial }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 23},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"ShowPlayerDialog"`, "DIALOG_STYLE:style", "OPEN_MP_TAGS:arguments..."} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("dialog completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsMenuAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { CreateMen }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 18},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"CreateMenu"`, "Menu:CreateMenu", "OPEN_MP_TAGS:arguments..."} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("menu completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsObjectAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { CreateObj }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 18},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"CreateObject"`, "Float:rotationZ", "Float:drawDistance = 0"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("object completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsObjectMaterialAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetObjectMaterialTe }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 26},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"SetObjectMaterialText"`, "OBJECT_MATERIAL_SIZE:materialSize", "OPEN_MP_TAGS:arguments..."} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("object material completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsPlayerObjectAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerObjectMaterialTe }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 32},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"SetPlayerObjectMaterialText"`, "OBJECT_MATERIAL_TEXT_ALIGN:textAlignment", "OPEN_MP_TAGS:arguments..."} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("player object completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsAttachedObjectAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { SetPlayerAttachedObj }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 30},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"SetPlayerAttachedObject"`, "Float:scaleX = 1", "materialColour2 = 0"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("attached object completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsClassAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { AddPlayerCla }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 21},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"AddPlayerClass"`, "WEAPON:weapon1 = WEAPON_FIST", "ammo3 = 0"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("class completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsPickupAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { CreatePlayerPick }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 25},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"CreatePlayerPickup"`, "Float:z", "virtualWorld = 0"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("pickup completion missing %q: %s", value, output.String())
		}
	}
}

func TestServerReturnsGangZoneAPICompletion(t *testing.T) {
	uri := tempDocumentURI(t)
	text := "main() { CreatePlayerGangZ }"
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": text},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 27},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"label":"CreatePlayerGangZone"`, "Float:minx", "Float:maxy"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("gang-zone completion missing %q: %s", value, output.String())
		}
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
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "completionItem/resolve", "params": map[string]any{
		"label": "Add", "data": completionData{Kind: "local", URI: uri, Source: uri, Name: "Add", Start: strings.Index(text, "stock Add") + len("stock ")},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"value":"Adds two values."`) || !strings.Contains(output.String(), `"detail":"stock Add(left, right)"`) {
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
