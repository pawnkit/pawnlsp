package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintproject "github.com/pawnkit/pawnlint/pkg/project"
)

type testIncludeResolver struct {
	content []byte
	uri     string
}

func (r testIncludeResolver) Resolve(_, _ string, _ bool) ([]byte, string, bool) {
	return r.content, r.uri, true
}

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

func TestAnalysisSourceAtMapsExpandedInclude(t *testing.T) {
	result := analysis.Analyze([]byte("#include <helper>\nmain() { return Shared; }\n"), analysis.Options{
		URI: coresource.FileURI("/project/main.pwn"),
		Includes: testIncludeResolver{
			content: []byte("new Shared = 1;\n"),
			uri:     coresource.FileURI("/project/helper.inc").String(),
		},
		RetainExpanded: true,
	})
	if result.ExpandedSymbols == nil {
		t.Fatal("expanded symbols missing")
	}
	for _, item := range result.ExpandedSymbols.Symbols {
		if item.Name != "Shared" {
			continue
		}
		uri, text, ok := analysisSourceAt(result, item.Span.File)
		if !ok || uri.String() != coresource.FileURI("/project/helper.inc").String() || !strings.Contains(string(text), "new Shared") {
			t.Fatalf("include source = %q %q %v; file=%d files=%#v", uri, text, ok, item.Span.File, result.Preprocess.Files)
		}
		return
	}
	t.Fatal("included declaration missing")
}

func TestWorkspaceDiagnosticURIExcludesToolchainFiles(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "dependencies", "library", "api.inc"),
		filepath.Join(root, "server", "dependencies", "library", "api.inc"),
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

func TestWorkspaceEntryKeepsUnrelatedProgramsOutOfGraph(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.pwn")
	unrelated := filepath.Join(root, "filterscripts", "other.pwn")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("#error unrelated program\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, graph, err := buildWorkspaceIndex(context.Background(), root, entry, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if graph == nil {
		t.Fatal("entry graph was not built")
	}
	if len(files) != 1 {
		t.Fatalf("standalone files = %d, want 1", len(files))
	}
	for _, finding := range graph.Diagnostics {
		if strings.Contains(finding.Message, "unrelated program") {
			t.Fatalf("unrelated program was analysed: %+v", finding)
		}
	}
}

func TestWorkspacePathKeyAcceptsBothSeparators(t *testing.T) {
	slashed := workspacePathKey(`C:/project/src/main.pwn`)
	backslashed := workspacePathKey(`C:\project\src\main.pwn`)
	if slashed != backslashed {
		t.Fatalf("path keys differ: %q != %q", slashed, backslashed)
	}
}

func TestStandaloneWorkspaceDiagnosticsSkipIncludes(t *testing.T) {
	uri := coresource.FileURI(filepath.Join(t.TempDir(), "guarded.inc"))
	result := analysis.Analyze([]byte("#error parent include not loaded\n"), analysis.Options{URI: uri})
	if diagnostics := standaloneWorkspaceDiagnosticItems(uri, result); len(diagnostics) != 0 || diagnostics == nil {
		t.Fatalf("include diagnostics = %#v", diagnostics)
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
	}, includes, nil, nil, nil)
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

func TestWorkspaceEntryReusesPreviousAnalysis(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "main.pwn")
	first := []byte("stock First(value) { return value + 1; }\nstock Second(value) { return value + 2; }\n")
	previous, err := analyzeWorkspaceEntry(
		context.Background(), root, entry, map[string][]byte{workspacePathKey(entry): first},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	second := []byte("stock First(value) { return value + 3; }\nstock Second(value) { return value + 2; }\n")
	start := bytes.Index(first, []byte("1"))
	if start < 0 {
		t.Fatal("edit target not found")
	}
	current, err := analyzeWorkspaceEntryWithEdit(
		context.Background(), root, entry, map[string][]byte{workspacePathKey(entry): second},
		nil, nil, nil, previous, &preprocess.CompatibleEdit{
			Before: preprocess.ByteRange{Start: start, End: start + 1},
			After:  preprocess.ByteRange{Start: start, End: start + 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if current.Reuse.Declarations == 0 {
		t.Fatal("unchanged declaration was not reused")
	}
}

func TestWorkspaceIndexReusesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "first.inc"),
		filepath.Join(root, "second.inc"),
	}
	for index, path := range paths {
		content := []byte("stock First() {}\n")
		if index == 1 {
			content = []byte("stock Second() {}\n")
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	first, snapshot, previousPaths, err := analyzeWorkspaceFilesWithSnapshot(
		context.Background(), root, "", nil, nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, repeatedSnapshot, repeatedPaths, err := analyzeWorkspaceFilesWithSnapshot(
		context.Background(), root, "", nil, nil, nil, nil, snapshot, previousPaths,
	)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedSnapshot != snapshot || !sameWorkspacePaths(previousPaths, repeatedPaths) {
		t.Fatal("unchanged workspace snapshot was not retained")
	}
	if first[coresource.FileURI(paths[0])] != repeated[coresource.FileURI(paths[0])] {
		t.Fatal("unchanged workspace completion was rebuilt")
	}

	changed := []byte("stock Changed() {}\n")
	if err := os.WriteFile(paths[1], changed, 0o600); err != nil {
		t.Fatal(err)
	}
	second, nextSnapshot, nextPaths, err := analyzeWorkspaceFilesWithSnapshot(
		context.Background(), root, "", nil, nil, nil, nil, snapshot, previousPaths,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !sameWorkspacePaths(previousPaths, nextPaths) {
		t.Fatal("workspace file set changed unexpectedly")
	}
	if nextSnapshot == snapshot {
		t.Fatal("changed file did not create a new snapshot")
	}
	firstChanged := first[coresource.FileURI(paths[1])]
	secondChanged := second[coresource.FileURI(paths[1])]
	if firstChanged == nil || secondChanged == nil {
		t.Fatal("changed file result missing")
	}
	if firstChanged == secondChanged {
		t.Fatal("changed file analysis was reused")
	}
}

func TestActiveProjectOwnsNestedDependency(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dependencies", "library", "include.inc")
	parent := &document{Root: root, Entry: filepath.Join(root, "main.pwn")}
	s := &server{
		documents:  map[string]*document{"main": parent},
		workspaces: map[string]*workspaceIndex{root: {root: root}},
	}
	if selected := s.activeProjectDocument(path); selected != parent {
		t.Fatalf("selected = %p, want parent project %p", selected, parent)
	}
}

func TestCloseRemovesUnusedWorkspace(t *testing.T) {
	root := t.TempDir()
	uri := coresource.FileURI(filepath.Join(root, "main.pwn")).String()
	cancelled := false
	s := &server{
		documents: map[string]*document{uri: {URI: uri, Root: root}},
		workspaces: map[string]*workspaceIndex{root: {
			root: root,
			cancel: func() {
				cancelled = true
			},
		}},
	}
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.didClose(params); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("workspace analysis was not cancelled")
	}
	if len(s.workspaces) != 0 {
		t.Fatalf("workspaces = %d, want 0", len(s.workspaces))
	}
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
		if finding.Code == "pawn-analysis:preprocess/include-cycle" {
			t.Errorf("unexpected include cycle: %s", finding.Message)
		}
		if finding.Code == "pawn-analysis:sema/argument-count" && strings.Contains(finding.Message, `"format"`) {
			t.Errorf("unexpected format diagnostic: %s", finding.Message)
		}
		if finding.Code == "pawn-analysis:sema/argument-count" && strings.Contains(finding.Message, `"SendClientMessage"`) {
			t.Errorf("unexpected SendClientMessage diagnostic: %s", finding.Message)
		}
	}
}

func TestRealProjectProtocolResults(t *testing.T) {
	root := os.Getenv("PAWN_REAL_PROJECT_DIR")
	if root == "" {
		t.Skip("PAWN_REAL_PROJECT_DIR is not set")
	}
	entry := filepath.Join(root, "src", "safw.pwn")
	text, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	uri := coresource.FileURI(entry).String()
	var input bytes.Buffer
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1, "text": string(text)},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/semanticTokens/full", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}})
	dependency := filepath.Join(root, "server", "dependencies", "amx_assembly", "amx.inc")
	dependencyText, err := os.ReadFile(dependency)
	if err != nil {
		t.Fatal(err)
	}
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{
			"uri": coresource.FileURI(dependency).String(), "version": 1, "text": string(dependencyText),
		},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "workspace/diagnostic", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(dependency).String()},
	}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "workspace/diagnostic", "params": map[string]any{}})
	frame(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"not loaded", `"format" expects 4 arguments`, `"SendClientMessage" expects 3 arguments`,
		"include cycle detected", "include cycle:", "Unsupported `cellbits`", "include target not found: core",
	} {
		if strings.Contains(output.String(), value) {
			t.Errorf("protocol output contains %q", value)
		}
	}
	reader := bufio.NewReader(bytes.NewReader(output.Bytes()))
	foundInactive := false
	cleared := map[string]bool{
		coresource.FileURI(filepath.Join(root, "src", "modules", "core", "api", "index.inc")).String():     false,
		coresource.FileURI(filepath.Join(root, "src", "modules", "core", "logger", "logger.inc")).String(): false,
	}
	for {
		body, err := readFrame(reader)
		if err != nil {
			break
		}
		var response struct {
			ID     int `json:"id"`
			Result struct {
				Data  []int `json:"data"`
				Items []struct {
					URI   string `json:"uri"`
					Items []any  `json:"items"`
				} `json:"items"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &response) != nil {
			continue
		}
		if response.ID == 2 {
			for _, report := range response.Result.Items {
				if strings.Contains(filepath.ToSlash(report.URI), "/dependencies/") {
					t.Errorf("workspace response included dependency %s", report.URI)
				}
				if _, ok := cleared[report.URI]; ok && len(report.Items) == 0 {
					cleared[report.URI] = true
				}
			}
			continue
		}
		if response.ID != 3 {
			continue
		}
		line := 0
		for index := 0; index+4 < len(response.Result.Data); index += 5 {
			line += response.Result.Data[index]
			if line >= 429 && line <= 445 && response.Result.Data[index+3] == semanticInactive {
				foundInactive = true
				break
			}
		}
	}
	if !foundInactive {
		t.Fatal("semantic response did not dim the inactive YSF block")
	}
	for uri, ok := range cleared {
		if !ok {
			t.Errorf("workspace response did not clear %s", uri)
		}
	}
}

func TestRealProjectWorkspaceDiagnosticLatency(t *testing.T) {
	root := os.Getenv("PAWN_REAL_PROJECT_DIR")
	if root == "" {
		t.Skip("PAWN_REAL_PROJECT_DIR is not set")
	}
	entry := filepath.Join(root, "src", "safw.pwn")
	text, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Run(inputReader, outputWriter)
		_ = outputWriter.Close()
	}()
	var requests bytes.Buffer
	frame(t, &requests, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	frame(t, &requests, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": coresource.FileURI(entry).String(), "version": 1, "text": string(text)},
	}})
	frame(t, &requests, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "workspace/diagnostic", "params": map[string]any{}})

	started := time.Now()
	written := make(chan error, 1)
	go func() {
		_, err := inputWriter.Write(requests.Bytes())
		written <- err
	}()
	reader := bufio.NewReader(outputReader)
	for {
		body, err := readFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(body, &response) == nil && response.ID == 2 {
			break
		}
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	t.Logf("workspace diagnostics took %s", elapsed)
	if elapsed > 15*time.Second {
		t.Errorf("workspace diagnostics exceeded 15 seconds: %s", elapsed)
	}
	go func() {
		_, _ = io.Copy(io.Discard, outputReader)
	}()
	var exit bytes.Buffer
	frame(t, &exit, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	_, _ = inputWriter.Write(exit.Bytes())
	_ = inputWriter.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRealProjectIncrementalAnalysisLatency(t *testing.T) {
	root := os.Getenv("PAWN_REAL_PROJECT_DIR")
	if root == "" {
		t.Skip("PAWN_REAL_PROJECT_DIR is not set")
	}
	entry := filepath.Join(root, "src", "safw.pwn")
	text, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	includes, _, projectRoot, resolvedEntry := loadProjectContext(entry)
	tokenCache := preprocess.NewTokenCache()
	previous, err := analyzeWorkspaceEntry(
		context.Background(), projectRoot, resolvedEntry,
		map[string][]byte{workspacePathKey(entry): text}, includes, nil, tokenCache, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	edit := bytes.LastIndex(text, []byte("return 0;"))
	if edit < 0 {
		t.Fatal("local edit target not found")
	}
	position := edit + len("return 0")
	changed := make([]byte, 0, len(text)+4)
	changed = append(changed, text[:position]...)
	changed = append(changed, " + 0"...)
	changed = append(changed, text[position:]...)
	stages := make(map[analysis.Stage]time.Duration)

	started := time.Now()
	current, err := analysis.AnalyzeContext(context.Background(), changed, analysis.Options{
		URI: coresource.FileURI(resolvedEntry),
		Includes: workspaceOverlayResolver{
			base: includes, open: map[string][]byte{workspacePathKey(entry): changed},
		},
		RetainExpanded: true, Revision: projectRoot, MaxOutputTokens: analysisOutputTokenLimit,
		Previous: previous, TokenCache: tokenCache, ReuseCompatibleExpansion: true,
		Trace: func(event analysis.TraceEvent) {
			stages[event.Stage] += event.Duration
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	t.Logf(
		"incremental workspace analysis took %s; %d expanded tokens; reused %d declarations, %d tag checks, %d CFGs",
		elapsed, len(current.Preprocess.ExpandedTokens),
		current.Reuse.Declarations, current.Reuse.Tags, current.Reuse.ControlFlow,
	)
	for stage, duration := range stages {
		t.Logf("%s: %s", stage, duration)
	}
	if current.Reuse.ControlFlow == 0 {
		t.Fatal("incremental analysis did not reuse control flow")
	}
	if !current.Reuse.CompatibleExpansion {
		t.Fatal("incremental analysis did not reuse the dependency graph")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("incremental analysis exceeded 2 seconds: %s", elapsed)
	}
	cache := lintproject.NewParseCache()
	lintStarted := time.Now()
	_, err = lintDocument(context.Background(), &document{
		Path: entry, Root: projectRoot, Entry: resolvedEntry, Text: changed,
	}, cache, current)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cold editor lint took %s", time.Since(lintStarted))
	lintStarted = time.Now()
	_, err = lintDocument(context.Background(), &document{
		Path: entry, Root: projectRoot, Entry: resolvedEntry, Text: changed,
	}, cache, current)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("warm editor lint took %s", time.Since(lintStarted))
}

func BenchmarkRealProjectWarmLint(b *testing.B) {
	root := os.Getenv("PAWN_REAL_PROJECT_DIR")
	if root == "" {
		b.Skip("PAWN_REAL_PROJECT_DIR is not set")
	}
	entry := filepath.Join(root, "src", "safw.pwn")
	text, err := os.ReadFile(entry)
	if err != nil {
		b.Fatal(err)
	}
	includes, _, projectRoot, resolvedEntry := loadProjectContext(entry)
	graph, err := analyzeWorkspaceEntry(
		context.Background(), projectRoot, resolvedEntry,
		map[string][]byte{workspacePathKey(entry): text}, includes, nil,
		preprocess.NewTokenCache(), nil,
	)
	if err != nil {
		b.Fatal(err)
	}
	document := &document{
		Path: entry, Root: projectRoot, Entry: resolvedEntry, Text: text,
	}
	cache := lintproject.NewParseCache()
	if _, err := lintDocument(context.Background(), document, cache, graph); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := lintDocument(context.Background(), document, cache, graph); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReusableWorkspaceGraphSurvivesPendingRefresh(t *testing.T) {
	previous := &analysis.Result{}
	pending := &workspaceIndex{previous: previous}
	if got := reusableWorkspaceGraph(pending); got != previous {
		t.Fatalf("graph = %p, want previous %p", got, previous)
	}

	current := &analysis.Result{}
	pending.graph = current
	if got := reusableWorkspaceGraph(pending); got != current {
		t.Fatalf("graph = %p, want current %p", got, current)
	}
}
