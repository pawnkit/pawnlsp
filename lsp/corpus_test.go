package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTagDiagnosticsMatchCorpus(t *testing.T) {
	root := lspCorpusRoot()
	if root == "" {
		t.Skip("pawn-corpus is unavailable")
	}
	paths := []string{
		filepath.Join(root, "semantics", "compiler_tag_mismatch.pwn"),
		filepath.Join(root, "semantics", "compiler_tag_union.pwn"),
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".pwn"), func(t *testing.T) {
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			uri := tempDocumentURI(t)
			var input bytes.Buffer
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
				"textDocument": map[string]any{"uri": uri, "version": 1, "text": string(text)},
			}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/diagnostic", "params": map[string]any{
				"textDocument": map[string]any{"uri": uri},
			}})
			frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

			var output bytes.Buffer
			if err := Run(&input, &output); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "pawn-analysis:sema/undefined-symbol") {
				t.Fatalf("tag reported as undefined: %s", output.String())
			}
		})
	}
}

func lspCorpusRoot() string {
	if root := os.Getenv("PAWN_CORPUS_DIR"); root != "" {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
		return ""
	}
	root := filepath.Join("..", "..", "pawn-corpus")
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		return root
	}
	return ""
}
