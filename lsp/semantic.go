package lsp

import (
	"encoding/json"
	"sort"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/symbol"
	"github.com/pawnkit/pawn-api/pawnapi"
	"github.com/pawnkit/pawn-parser/token"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

var (
	semanticTokenTypes     = []string{"function", "variable", "parameter", "enum", "type", "macro", "inactive"}
	semanticTokenModifiers = []string{"declaration", "readonly", "deprecated", "defaultLibrary"}
)

const (
	semanticFunction = iota
	semanticVariable
	semanticParameter
	semanticEnum
	semanticType
	semanticMacro
	semanticInactive
)

const (
	modifierDeclaration = 1 << iota
	modifierReadonly
	modifierDeprecated
	modifierDefaultLibrary
)

type semanticToken struct {
	start     coresource.Offset
	end       coresource.Offset
	tokenType int
	modifiers int
}

func (s *server) semanticTokens(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil || doc.Analysis == nil {
		return s.respond(id, map[string]any{"data": []int{}})
	}
	return s.respond(id, map[string]any{"data": encodeSemanticTokens(doc, collectSemanticTokens(doc, s.workspaceGraph(doc)))})
}

func collectSemanticTokens(doc *document, graph *analysis.Result) []semanticToken {
	table := navigationTable(doc.Analysis)
	if table == nil {
		return nil
	}
	inactive := inactiveBranchRanges(doc, graph)
	tokens := make([]semanticToken, 0, len(table.Symbols)+len(table.References))
	seen := make(map[[2]coresource.Offset]bool)
	add := func(span coresource.Span, tokenType, modifiers int) {
		if span.File != doc.Analysis.File || span.Start >= span.End {
			return
		}
		if tokenType != semanticInactive && insideRanges(span.Start, span.End, inactive) {
			return
		}
		key := [2]coresource.Offset{span.Start, span.End}
		if seen[key] {
			return
		}
		seen[key] = true
		tokens = append(tokens, semanticToken{start: span.Start, end: span.End, tokenType: tokenType, modifiers: modifiers})
	}
	if doc.Analysis.Preprocess != nil {
		for _, item := range doc.Analysis.Preprocess.OriginalTokens {
			if item.Kind == token.EOF {
				continue
			}
			start, end := coresource.Offset(item.Start.Offset), coresource.Offset(item.End.Offset)
			if insideRanges(start, end, inactive) {
				add(coresource.Span{File: doc.Analysis.File, Start: start, End: end}, semanticInactive, 0)
			}
		}
	}
	for _, item := range table.Symbols {
		tokenType, modifiers := semanticSymbol(item)
		add(item.Span, tokenType, modifiers|modifierDeclaration)
	}
	for _, reference := range table.References {
		if reference.Resolved != 0 {
			if item, ok := table.Symbol(reference.Resolved); ok {
				tokenType, modifiers := semanticSymbol(item)
				add(reference.Span, tokenType, modifiers)
			}
			continue
		}
		if entry, ok := apiEntry(doc.Names, reference.Name); ok {
			tokenType, modifiers := semanticAPI(entry)
			add(reference.Span, tokenType, modifiers)
			continue
		}
		if doc.Analysis.Preprocess != nil {
			if _, ok := doc.Analysis.Preprocess.Macros[reference.Name]; ok {
				add(reference.Span, semanticMacro, modifierReadonly)
			}
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].start == tokens[j].start {
			return tokens[i].end < tokens[j].end
		}
		return tokens[i].start < tokens[j].start
	})
	return tokens
}

type semanticRange struct {
	start coresource.Offset
	end   coresource.Offset
}

func inactiveBranchRanges(doc *document, graph *analysis.Result) []semanticRange {
	if doc == nil || doc.Analysis == nil || doc.Analysis.Preprocess == nil {
		return nil
	}
	result := doc.Analysis
	var file uint32
	if graph != nil && graph.Preprocess != nil {
		for index, source := range graph.Preprocess.Files {
			if source.URI == doc.URI {
				result = graph
				file = uint32(index)
				break
			}
		}
	}
	var ranges []semanticRange
	for _, branch := range result.Preprocess.Branches {
		if branch.File != file || branch.Active || branch.BodySpan.Start >= branch.BodySpan.End {
			continue
		}
		ranges = append(ranges, semanticRange{
			start: coresource.Offset(branch.BodySpan.Start),
			end:   coresource.Offset(branch.BodySpan.End),
		})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	merged := ranges[:0]
	for _, item := range ranges {
		if len(merged) == 0 || item.start > merged[len(merged)-1].end {
			merged = append(merged, item)
			continue
		}
		if item.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = item.end
		}
	}
	return merged
}

func insideRanges(start, end coresource.Offset, ranges []semanticRange) bool {
	index := sort.Search(len(ranges), func(i int) bool { return ranges[i].end > start })
	return index < len(ranges) && start >= ranges[index].start && end <= ranges[index].end
}

func semanticSymbol(item symbol.Symbol) (int, int) {
	switch {
	case item.Kind.IsCallable():
		return semanticFunction, 0
	case item.Kind == symbol.KindParameter:
		return semanticParameter, 0
	case item.Kind == symbol.KindEnum:
		return semanticEnum, 0
	case item.Kind == symbol.KindConstant || item.IsConst:
		return semanticVariable, modifierReadonly
	default:
		return semanticVariable, 0
	}
}

func semanticAPI(entry pawnapi.Entry) (int, int) {
	modifiers := modifierDefaultLibrary
	if entry.Deprecated != nil {
		modifiers |= modifierDeprecated
	}
	switch entry.Kind {
	case pawnapi.KindNative, pawnapi.KindCallback, pawnapi.KindFunction:
		return semanticFunction, modifiers
	case pawnapi.KindTag:
		return semanticType, modifiers
	case pawnapi.KindConstant, pawnapi.KindDefine:
		return semanticVariable, modifiers | modifierReadonly
	default:
		return semanticVariable, modifiers
	}
}

func encodeSemanticTokens(doc *document, tokens []semanticToken) []int {
	index := doc.lineIndex()
	data := make([]int, 0, len(tokens)*5)
	previousLine, previousCharacter := 0, 0
	for _, token := range tokens {
		start, err := index.Position(token.start, coresource.UTF16)
		if err != nil {
			continue
		}
		end, err := index.Position(token.end, coresource.UTF16)
		if err != nil || start.Line != end.Line {
			continue
		}
		deltaLine := start.Line - previousLine
		deltaCharacter := start.Character
		if deltaLine == 0 {
			deltaCharacter -= previousCharacter
		}
		data = append(data, deltaLine, deltaCharacter, end.Character-start.Character, token.tokenType, token.modifiers)
		previousLine, previousCharacter = start.Line, start.Character
	}
	return data
}
