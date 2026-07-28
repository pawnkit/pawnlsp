package lsp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestWorkspaceSourceFiles(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "gamemodes", "main.pwn"),
		filepath.Join(root, "include", "helper.inc"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, ".git", "ignored.pwn"),
		filepath.Join(root, "build", "generated.pwn"),
		filepath.Join(root, "dependencies", "package", "external.inc"),
		filepath.Join(root, "pawno", "include", "compiler.inc"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := workspaceSourceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{paths[0], paths[1]}
	if !slices.Equal(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
}

func TestWorkspaceDiagnosticURIExcludesToolchainFiles(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "dependencies", "library", "api.inc"),
		filepath.Join(root, "pawno", "include", "open.mp.inc"),
	} {
		if workspaceDiagnosticURI(root, coresource.FileURI(path)) {
			t.Fatalf("toolchain file included: %s", path)
		}
	}
	if path := filepath.Join(root, "include", "project.inc"); !workspaceDiagnosticURI(root, coresource.FileURI(path)) {
		t.Fatalf("project file excluded: %s", path)
	}
}

func TestWorkspacePathKeyAcceptsBothSeparators(t *testing.T) {
	slashed := workspacePathKey(`C:/project/src/main.pwn`)
	backslashed := workspacePathKey(`C:\project\src\main.pwn`)
	if slashed != backslashed {
		t.Fatalf("path keys differ: %q != %q", slashed, backslashed)
	}
}

func TestWorkspaceEntryUsesOpenInclude(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.pwn")
	includePath := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(filepath.Join(root, "pawn.json"), []byte(`{"entry":"main.pwn"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte("#include \"shared.inc\"\nmain() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(includePath, []byte("stock DiskVersion() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	includes, _, projectRoot, entry := loadProjectContext(mainPath)
	graph, err := analyzeWorkspaceEntry(context.Background(), projectRoot, entry, map[string][]byte{
		workspacePathKey(includePath): []byte("stock OpenVersion() {}\n"),
	}, includes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range graph.Preprocess.Files {
		if file.URI == coresource.FileURI(includePath).String() {
			if !strings.Contains(string(file.Content), "OpenVersion") {
				t.Fatalf("included content = %q", file.Content)
			}
			return
		}
	}
	t.Fatal("open include was not in the project graph")
}

func TestRealProjectWorkspaceDiagnostics(t *testing.T) {
	root := os.Getenv("PAWN_REAL_PROJECT_DIR")
	if root == "" {
		t.Skip("PAWN_REAL_PROJECT_DIR is not set")
	}
	includes, _, projectRoot, entry := loadProjectContext(filepath.Join(root, "pawn.json"))
	if includes == nil || entry == "" {
		t.Fatalf("project context was not loaded: root=%q entry=%q", projectRoot, entry)
	}
	_, graph, err := buildWorkspaceIndex(context.Background(), projectRoot, entry, nil, includes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if graph == nil {
		t.Fatal("project entry was not analysed")
	}
	for _, finding := range graph.Diagnostics {
		if finding.Code == "pawn-analysis:preprocess/user-error" && strings.Contains(finding.Message, "not loaded") {
			t.Errorf("unexpected include guard diagnostic: %s", finding.Message)
		}
		if finding.Code == "pawn-analysis:sema/argument-count" && strings.Contains(finding.Message, `"format"`) {
			t.Errorf("unexpected format diagnostic: %s", finding.Message)
		}
	}
}
