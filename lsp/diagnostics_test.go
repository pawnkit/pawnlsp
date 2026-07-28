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
