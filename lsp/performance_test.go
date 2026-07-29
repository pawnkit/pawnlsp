package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
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

		runBenchmarkChange(b, server, doc.URI, version, params, true)
	}
}

func BenchmarkIncrementalDidChangeToDiagnostics50K(b *testing.B) {
	benchmarkIncrementalDidChange(b, 50_000, true)
}

func BenchmarkIncrementalDidChangeToAnalysis50K(b *testing.B) {
	benchmarkIncrementalDidChange(b, 50_000, false)
}

func BenchmarkIncrementalDidChangeStages50K(b *testing.B) {
	server, doc, text := benchmarkLSPServer(b, 50_000)
	editOffset := strings.LastIndex(string(text), "return 0")
	line := strings.Count(string(text[:editOffset]), "\n")
	durations := make(map[analysis.Stage]time.Duration)
	server.analysisTrace = func(_ string, _ int, event analysis.TraceEvent) {
		durations[event.Stage] += event.Duration
	}

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
		runBenchmarkChange(b, server, doc.URI, version, params, false)
		b.StopTimer()
		server.fullReadyDocument(doc.URI)
		b.StartTimer()
	}
	for stage, duration := range durations {
		b.ReportMetric(float64(duration.Nanoseconds())/float64(b.N), string(stage)+"-ns/op")
	}
}

func BenchmarkIncrementalDidChangeScaling(b *testing.B) {
	for _, lines := range []int{10_000, 25_000, 50_000, 100_000} {
		b.Run(strconv.Itoa(lines), func(b *testing.B) {
			benchmarkIncrementalDidChange(b, lines, true)
		})
	}
}

func BenchmarkDocumentDiagnostics50K(b *testing.B) {
	server, doc, text := benchmarkLSPServer(b, 50_000)

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		_ = server.documentDiagnosticItems(doc, true, nil)
	}
}

func TestIncrementalPerformanceBudget(t *testing.T) {
	if os.Getenv("PAWNKIT_PERFORMANCE_BUDGET") != "1" {
		t.Skip("set PAWNKIT_PERFORMANCE_BUDGET=1 to run performance budgets")
	}
	server, doc, text := performanceLSPServer(t, 50_000)
	editOffset := strings.LastIndex(string(text), "return 0")
	line := strings.Count(string(text[:editOffset]), "\n")
	durations := make([]time.Duration, 0, 3)
	var peakBytes uint64
	var peakAllocations uint64

	for iteration := range 3 {
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
			t.Fatal(err)
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		if err := server.didChange(params); err != nil {
			t.Fatal(err)
		}
		current := server.fullReadyDocument(doc.URI)
		durations = append(durations, time.Since(started))
		runtime.ReadMemStats(&after)
		if current == nil || current.Version != version || current.Analysis == nil {
			t.Fatalf("version %d was not analyzed", version)
		}
		peakBytes = max(peakBytes, after.TotalAlloc-before.TotalAlloc)
		peakAllocations = max(peakAllocations, after.Mallocs-before.Mallocs)
	}

	slices.Sort(durations)
	median := durations[len(durations)/2]
	t.Logf("50K incremental diagnostics: median %s, peak %d MB, peak %d allocations", median, peakBytes/(1024*1024), peakAllocations)
	if median > 2*time.Second {
		t.Errorf("median latency %s exceeds 2s regression budget", median)
	}
	if peakBytes > 768*1024*1024 {
		t.Errorf("allocated %d MB exceeds 768 MB regression budget", peakBytes/(1024*1024))
	}
	if peakAllocations > 2_000_000 {
		t.Errorf("%d allocations exceed 2,000,000 regression budget", peakAllocations)
	}
}

func benchmarkIncrementalDidChange(b *testing.B, lines int, full bool) {
	b.Helper()
	server, doc, text := benchmarkLSPServer(b, lines)
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

		runBenchmarkChange(b, server, doc.URI, version, params, full)
		if !full {
			b.StopTimer()
			server.fullReadyDocument(doc.URI)
			b.StartTimer()
		}
	}
}

func benchmarkLSPServer(b *testing.B, lines int) (*server, *document, []byte) {
	b.Helper()
	server, doc, text := performanceLSPServer(b, lines)
	b.SetBytes(int64(len(text)))
	return server, doc, text
}

func performanceLSPServer(tb testing.TB, lines int) (*server, *document, []byte) {
	tb.Helper()
	uri := coresource.FileURI("benchmark.pwn")
	text := benchmarkGamemode(lines)
	buffer := coresource.NewTextBuffer(text)
	doc := &document{
		URI: uri.String(), Path: "benchmark.pwn", Text: text, Buffer: buffer, Version: 1,
		Index: coresource.NewBufferedLineIndex(buffer),
		ready: closedChannel(),
	}
	server := &server{
		documents:  map[string]*document{doc.URI: doc},
		snapshot:   query.New(query.Document{URI: uri, Buffer: buffer, Version: 1}),
		parseCache: lintproject.NewParseCache(),
		tokenCache: preprocess.NewTokenCache(),
		rules:      lintrules.Default(),
		workspaces: make(map[string]*workspaceIndex),
	}
	if err := server.publish(context.Background(), doc, server.snapshot); err != nil {
		tb.Fatal(err)
	}
	return server, doc, text
}

func runBenchmarkChange(b *testing.B, server *server, uri string, version int, params []byte, full bool) {
	b.Helper()
	if err := server.didChange(params); err != nil {
		b.Fatal(err)
	}
	current := server.readyDocument(uri)
	if full {
		current = server.fullReadyDocument(uri)
	}
	if current == nil || current.Version != version || current.Analysis == nil {
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
