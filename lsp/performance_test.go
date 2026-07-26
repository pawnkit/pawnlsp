package lsp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/query"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintproject "github.com/pawnkit/pawnlint/pkg/project"
	lintrules "github.com/pawnkit/pawnlint/pkg/rules"
)

func BenchmarkFullDidChangeToDiagnostics50K(b *testing.B) {
	server, doc, text := benchmarkLSPServer(b, 50_000)
	editOffset := strings.LastIndex(string(text), "return 0") + len("return ")

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for iteration := 0; b.Loop(); iteration++ {
		b.StopTimer()
		version := iteration + 2
		replacement := '0' + byte(iteration%10)
		next := append([]byte(nil), text...)
		next[editOffset] = replacement
		params, err := json.Marshal(map[string]any{
			"textDocument":   map[string]any{"uri": doc.URI, "version": version},
			"contentChanges": []map[string]any{{"text": string(next)}},
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		runBenchmarkChange(b, server, doc.URI, version, params)
	}
}

func BenchmarkIncrementalDidChangeToDiagnostics50K(b *testing.B) {
	server, doc, text := benchmarkLSPServer(b, 50_000)
	editOffset := strings.LastIndex(string(text), "return 0")
	line := strings.Count(string(text[:editOffset]), "\n")

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for iteration := 0; b.Loop(); iteration++ {
		b.StopTimer()
		version := iteration + 2
		params, err := json.Marshal(map[string]any{
			"textDocument": map[string]any{"uri": doc.URI, "version": version},
			"contentChanges": []map[string]any{{
				"range": map[string]any{
					"start": map[string]any{"line": line, "character": 16},
					"end":   map[string]any{"line": line, "character": 17},
				},
				"text": strconv.Itoa(iteration % 10),
			}},
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		runBenchmarkChange(b, server, doc.URI, version, params)
	}
}

func benchmarkLSPServer(b *testing.B, lines int) (*server, *document, []byte) {
	b.Helper()
	uri := coresource.FileURI("benchmark.pwn")
	text := benchmarkGamemode(lines)
	doc := &document{
		URI: uri.String(), Path: "benchmark.pwn", Text: text, Version: 1,
		Index: coresource.NewLineIndex(string(text)),
		ready: closedChannel(),
	}
	server := &server{
		documents:  map[string]*document{doc.URI: doc},
		snapshot:   query.New(query.Document{URI: uri, Text: text, Version: 1}),
		parseCache: lintproject.NewParseCache(),
		tokenCache: preprocess.NewTokenCache(),
		rules:      lintrules.Default(),
		workspaces: make(map[string]*workspaceIndex),
	}
	return server, doc, text
}

func runBenchmarkChange(b *testing.B, server *server, uri string, version int, params []byte) {
	b.Helper()
	if err := server.didChange(params); err != nil {
		b.Fatal(err)
	}
	if current := server.readyDocument(uri); current == nil || current.Version != version || current.Analysis == nil {
		b.Fatalf("version %d was not analyzed", version)
	}
}

func benchmarkGamemode(lines int) []byte {
	var source strings.Builder
	source.Grow(lines * 32)
	source.WriteString("new gValue;\n")
	for index := 0; index*6 < lines-3; index++ {
		fmt.Fprintf(&source, "stock Function%d(value)\n{\n", index)
		source.WriteString("    new result = value + gValue;\n")
		source.WriteString("    if (result > 10) result--;\n")
		source.WriteString("    return result;\n}\n")
	}
	source.WriteString("main() { return 0; }\n")
	return []byte(source.String())
}

func closedChannel() chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}
