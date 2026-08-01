package lsp

import (
	"encoding/json"
	"testing"
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
