package lsp

import (
	"encoding/json"
	"testing"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-analysis/sema"
	coresource "github.com/pawnkit/pawnkit-core/source"
	lintdiagnostic "github.com/pawnkit/pawnlint/pkg/diagnostic"
	"github.com/pawnkit/pawnlint/pkg/lint"
	"github.com/pawnkit/pawnlint/pkg/rules"
)

func TestEmptyDiagnosticsEncodeAsArray(t *testing.T) {
	body, err := json.Marshal(map[string]any{"items": dedupeDiagnostics(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"items":[]}` {
		t.Fatalf("response = %s", body)
	}
}

func TestPreserveDiagnosticsShiftsOnlyUnaffectedFindings(t *testing.T) {
	items := lint.NewEngine(rules.Default()).LintFile(
		"test.pwn", []byte("main() { new a; a = a; new b; b = b; }\n"), lint.ProjectAnalysis,
		map[string]lintdiagnostic.Severity{"self-assignment": lintdiagnostic.SeverityWarning}, nil, nil,
	)
	if len(items) != 2 {
		t.Fatalf("lint findings = %#v, want two findings", items)
	}
	first, second := items[0], items[1]
	if first.RuleID != "self-assignment" || second.RuleID != "self-assignment" {
		t.Fatalf("lint findings = %#v", items)
	}
	insertAt := second.Range.Start.Offset - 1
	got := preserveDiagnostics(items, &preprocess.CompatibleEdit{
		Before: preprocess.ByteRange{Start: insertAt, End: insertAt},
		After:  preprocess.ByteRange{Start: insertAt, End: insertAt + 2},
	})
	if len(got) != 2 {
		t.Fatalf("preserved = %#v, want two findings", got)
	}
	if got[0].Range.Start.Offset != first.Range.Start.Offset || got[0].Range.End.Offset != first.Range.End.Offset {
		t.Fatalf("before finding = %#v", got[0])
	}
	if got[1].Range.Start.Offset != second.Range.Start.Offset+2 || got[1].Range.End.Offset != second.Range.End.Offset+2 {
		t.Fatalf("after finding = %#v", got[1])
	}
	if got := preserveDiagnostics(items, &preprocess.CompatibleEdit{
		Before: preprocess.ByteRange{Start: second.Range.Start.Offset, End: second.Range.End.Offset},
		After:  preprocess.ByteRange{Start: second.Range.Start.Offset, End: second.Range.Start.Offset},
	}); len(got) != 1 {
		t.Fatalf("overlapping findings = %#v, want one finding", got)
	}
}

func TestPreserveDiagnosticsRejectsWholeDocumentChanges(t *testing.T) {
	items := []lintdiagnostic.Diagnostic{{RuleID: "old"}}
	if got := preserveDiagnostics(items, nil); got != nil {
		t.Fatalf("preserved = %#v, want nil", got)
	}
}

func TestPreserveAnalysisShiftsRootFindings(t *testing.T) {
	result := analysis.Analyze([]byte("main() { Missing(); }\n"), analysis.Options{
		URI:   coresource.FileURI("test.pwn"),
		Names: sema.MapResolver{},
	})
	var originalStart coresource.Offset
	for _, item := range result.Diagnostics {
		if item.Primary.File == result.File {
			originalStart = item.Primary.Start
			break
		}
	}
	if originalStart == 0 {
		t.Fatalf("analysis diagnostics = %#v, want a root-file finding", result.Diagnostics)
	}

	got := preserveAnalysis(result, &preprocess.CompatibleEdit{
		Before: preprocess.ByteRange{Start: 0, End: 0},
		After:  preprocess.ByteRange{Start: 0, End: 2},
	})
	if got == nil || len(got.Diagnostics) == 0 {
		t.Fatalf("preserved analysis = %#v, want root finding", got)
	}
	if got.Diagnostics[0].Primary.Start != originalStart+2 {
		t.Fatalf("preserved start = %d, want %d", got.Diagnostics[0].Primary.Start, originalStart+2)
	}
}

func TestDedupeDiagnosticsIgnoresProducerCode(t *testing.T) {
	rng := lspRange{Start: position{Line: 4, Character: 2}, End: position{Line: 4, Character: 8}}
	items := []lspDiagnostic{
		{Code: "unreachable-code", Severity: 2, Range: rng, Message: "unreachable statement"},
		{Code: "pawn-analysis:sema/unreachable", Severity: 2, Range: rng, Message: "unreachable statement"},
		{Code: "other-rule", Severity: 2, Range: rng, Message: "a different finding"},
	}

	got := dedupeDiagnostics(items)
	if len(got) != 2 {
		t.Fatalf("deduplicated diagnostics = %#v, want two findings", got)
	}
	if got[0].Code != "unreachable-code" || got[1].Code != "other-rule" {
		t.Fatalf("deduplicated diagnostics = %#v", got)
	}
}

func TestDedupeDiagnosticsKeepsDifferentSeverities(t *testing.T) {
	rng := lspRange{Start: position{Line: 1, Character: 0}, End: position{Line: 1, Character: 3}}
	items := []lspDiagnostic{
		{Code: "warning-rule", Severity: 2, Range: rng, Message: "same text"},
		{Code: "error-rule", Severity: 1, Range: rng, Message: "same text"},
	}

	if got := dedupeDiagnostics(items); len(got) != len(items) {
		t.Fatalf("deduplicated diagnostics = %#v, want both severities", got)
	}
}

func TestMacroInvocationIndexFindsContainingSpan(t *testing.T) {
	index := macroInvocationIndex{
		spans:  []macroInvocationSpan{{start: 10, end: 20}, {start: 15, end: 17}, {start: 30, end: 40}},
		maxEnd: []int{20, 20, 40},
	}
	for _, test := range []struct {
		start int
		end   int
		want  bool
	}{
		{start: 10, end: 20, want: true},
		{start: 16, end: 18, want: true},
		{start: 30, end: 35, want: true},
		{start: 5, end: 10, want: false},
		{start: 21, end: 22, want: false},
	} {
		if got := index.contains(test.start, test.end); got != test.want {
			t.Errorf("contains(%d, %d) = %t, want %t", test.start, test.end, got, test.want)
		}
	}
}
