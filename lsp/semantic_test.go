package lsp

import (
	"strings"
	"testing"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/preprocess"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestInactiveBranchUsesInactiveSemanticTokens(t *testing.T) {
	text := []byte("#define FEATURE\n#if !defined FEATURE\nstock Hidden() {}\n#endif\nstock Visible() {}\n")
	result := analysis.Analyze(text, analysis.Options{URI: coresource.FileURI("main.pwn")})
	doc := &document{Text: text, Analysis: result}
	hidden := coresource.Offset(strings.Index(string(text), "Hidden"))
	visible := coresource.Offset(strings.Index(string(text), "Visible"))

	var hiddenInactive, visibleInactive bool
	for _, item := range collectSemanticTokens(doc, nil) {
		if item.start == hidden && item.tokenType == semanticInactive {
			hiddenInactive = true
		}
		if item.start == visible && item.tokenType == semanticInactive {
			visibleInactive = true
		}
	}
	if !hiddenInactive {
		t.Fatal("inactive function was not dimmed")
	}
	if visibleInactive {
		t.Fatal("active function was dimmed")
	}
}

func TestInactiveBranchUsesEntryIncludeState(t *testing.T) {
	include := []byte("#if !defined FEATURE\nstock Hidden() {}\n#endif\nstock Visible() {}\n")
	uri := coresource.FileURI("guarded.inc")
	graph := analysis.Analyze([]byte("#define FEATURE\n#include \"guarded.inc\"\n"), analysis.Options{
		URI:      coresource.FileURI("main.pwn"),
		Includes: preprocess.MapResolver{"guarded.inc": include},
	})
	result := analysis.Analyze(include, analysis.Options{URI: uri})
	doc := &document{URI: "guarded.inc", Text: include, Analysis: result}
	hidden := coresource.Offset(strings.Index(string(include), "Hidden"))
	visible := coresource.Offset(strings.Index(string(include), "Visible"))

	var hiddenInactive, visibleInactive bool
	for _, item := range collectSemanticTokens(doc, graph) {
		if item.start == hidden && item.tokenType == semanticInactive {
			hiddenInactive = true
		}
		if item.start == visible && item.tokenType == semanticInactive {
			visibleInactive = true
		}
	}
	if !hiddenInactive {
		t.Fatal("entry include state did not dim the inactive function")
	}
	if visibleInactive {
		t.Fatal("entry include state dimmed the active function")
	}
}
