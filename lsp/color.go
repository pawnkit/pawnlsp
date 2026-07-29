package lsp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	coresource "github.com/pawnkit/pawnkit-core/source"
)

var (
	pawnColorPattern         = regexp.MustCompile(`(?i)\b0x([0-9a-f]{8}|[0-9a-f]{6})\b`)
	embeddedPawnColorPattern = regexp.MustCompile(`(?i)\{([0-9a-f]{6})\}`)
)

type lspColor struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

func (s *server) documentColors(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.document(params.TextDocument.URI)
	if doc == nil {
		return s.respond(id, []any{})
	}
	items := make([]map[string]any, 0)
	for _, match := range pawnColorPattern.FindAllSubmatchIndex(doc.text(), -1) {
		items = appendColor(items, doc.text(), match[0], match[1], match[2], match[3])
	}
	for _, span := range pawnStringRanges(doc.text()) {
		for _, match := range embeddedPawnColorPattern.FindAllSubmatchIndex(doc.text()[span[0]:span[1]], -1) {
			items = appendColor(
				items, doc.text(),
				span[0]+match[0], span[0]+match[1],
				span[0]+match[2], span[0]+match[3],
			)
		}
	}
	return s.respond(id, items)
}

func appendColor(items []map[string]any, source []byte, start, end, valueStart, valueEnd int) []map[string]any {
	value, err := strconv.ParseUint(string(source[valueStart:valueEnd]), 16, 32)
	if err != nil {
		return items
	}
	alpha := uint64(255)
	if valueEnd-valueStart == 8 {
		alpha = value & 0xff
		value >>= 8
	}
	return append(items, map[string]any{
		"range": offsetRange(source, start, end),
		"color": lspColor{
			Red: float64((value>>16)&0xff) / 255, Green: float64((value>>8)&0xff) / 255,
			Blue: float64(value&0xff) / 255, Alpha: float64(alpha) / 255,
		},
	})
}

func (s *server) colorPresentation(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Color lspColor `json:"color"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	red, green, blue, alpha := colorByte(params.Color.Red), colorByte(params.Color.Green), colorByte(params.Color.Blue), colorByte(params.Color.Alpha)
	label := fmt.Sprintf("0x%02X%02X%02X", red, green, blue)
	if s.embeddedColorRange(params.TextDocument.URI, params.Range) {
		label = fmt.Sprintf("{%02X%02X%02X}", red, green, blue)
	} else if alpha != 255 {
		label += fmt.Sprintf("%02X", alpha)
	}
	return s.respond(id, []map[string]any{{"label": label, "textEdit": textEdit{Range: params.Range, NewText: label}}})
}

func (s *server) embeddedColorRange(uri string, rng lspRange) bool {
	doc := s.document(uri)
	if doc == nil {
		return false
	}
	index := doc.lineIndex()
	start, startErr := index.Offset(coresource.Position{Line: rng.Start.Line, Character: rng.Start.Character}, coresource.UTF16)
	end, endErr := index.Offset(coresource.Position{Line: rng.End.Line, Character: rng.End.Character}, coresource.UTF16)
	if startErr != nil || endErr != nil {
		return false
	}
	return embeddedPawnColorPattern.Match(doc.text()[start:end])
}

func pawnStringRanges(source []byte) [][2]int {
	const (
		code = iota
		stringLiteral
		lineComment
		blockComment
	)
	state := code
	start := 0
	var ranges [][2]int
	for index := 0; index < len(source); index++ {
		switch state {
		case code:
			switch {
			case source[index] == '"':
				start, state = index, stringLiteral
			case index+1 < len(source) && source[index] == '/' && source[index+1] == '/':
				index, state = index+1, lineComment
			case index+1 < len(source) && source[index] == '/' && source[index+1] == '*':
				index, state = index+1, blockComment
			}
		case stringLiteral:
			if source[index] == '"' && !escapedStringQuote(source, index, start) {
				ranges = append(ranges, [2]int{start, index + 1})
				state = code
			}
		case lineComment:
			if source[index] == '\n' {
				state = code
			}
		case blockComment:
			if index+1 < len(source) && source[index] == '*' && source[index+1] == '/' {
				index, state = index+1, code
			}
		}
	}
	return ranges
}

func escapedStringQuote(source []byte, offset, start int) bool {
	escapes := 0
	for index := offset - 1; index > start && (source[index] == '\\' || source[index] == '^'); index-- {
		escapes++
	}
	return escapes%2 != 0
}

func colorByte(value float64) int {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 255
	}
	return int(value*255 + 0.5)
}
