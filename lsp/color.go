package lsp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

var pawnColorPattern = regexp.MustCompile(`(?i)\b0x([0-9a-f]{8}|[0-9a-f]{6})\b`)

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
		value, err := strconv.ParseUint(string(doc.text()[match[2]:match[3]]), 16, 32)
		if err != nil {
			continue
		}
		alpha := uint64(255)
		if match[3]-match[2] == 8 {
			alpha = value & 0xff
			value >>= 8
		}
		items = append(items, map[string]any{
			"range": offsetRange(doc.text(), match[0], match[1]),
			"color": lspColor{
				Red: float64((value>>16)&0xff) / 255, Green: float64((value>>8)&0xff) / 255,
				Blue: float64(value&0xff) / 255, Alpha: float64(alpha) / 255,
			},
		})
	}
	return s.respond(id, items)
}

func (s *server) colorPresentation(id, raw json.RawMessage) error {
	var params struct {
		Color lspColor `json:"color"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	red, green, blue, alpha := colorByte(params.Color.Red), colorByte(params.Color.Green), colorByte(params.Color.Blue), colorByte(params.Color.Alpha)
	label := fmt.Sprintf("0x%02X%02X%02X", red, green, blue)
	if alpha != 255 {
		label += fmt.Sprintf("%02X", alpha)
	}
	return s.respond(id, []map[string]any{{"label": label, "textEdit": textEdit{Range: params.Range, NewText: label}}})
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
