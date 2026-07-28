package lsp

import (
	"strings"
	"testing"

	analysis "github.com/pawnkit/pawn-analysis"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func TestInactiveBranchUsesInactiveSemanticTokens(t *testing.T) {
	text := []byte("#define FEATURE\n#if !defined FEATURE\nstock Hidden() {}\n#endif\nstock Visible() {}\n")
	result := analysis.Analyze(text, analysis.Options{URI: coresource.FileURI("main.pwn")})
	doc := &document{Text: text, Analysis: result}
	hidden := coresource.Offset(strings.Index(string(text), "Hidden"))
	visible := coresource.Offset(strings.Index(string(text), "Visible"))

	var hiddenInactive, visibleInactive bool
	for _, item := range collectSemanticTokens(doc) {
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
