package lsp

import (
	"os"
	"path/filepath"
	"slices"
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
