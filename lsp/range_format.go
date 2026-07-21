package lsp

import (
	"encoding/json"

	"github.com/pawnkit/pawnfmt"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

type formattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

func (s *server) rangeFormatting(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range   lspRange          `json:"range"`
		Options formattingOptions `json:"options"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil {
		return s.respond(id, []textEdit{})
	}
	index := coresource.NewLineIndex(string(doc.Text))
	start, err := index.Offset(coresource.Position{Line: params.Range.Start.Line, Character: params.Range.Start.Character}, coresource.UTF16)
	if err != nil {
		return s.respondError(id, -32602, err.Error())
	}
	end, err := index.Offset(coresource.Position{Line: params.Range.End.Line, Character: params.Range.End.Character}, coresource.UTF16)
	if err != nil {
		return s.respondError(id, -32602, err.Error())
	}
	return s.formatSelectedRange(id, doc, int(start), int(end), params.Options)
}

func (s *server) onTypeFormatting(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position          `json:"position"`
		Options  formattingOptions `json:"options"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, []textEdit{})
	}
	return s.formatSelectedRange(id, doc, int(offset), int(offset), params.Options)
}

func (s *server) formatSelectedRange(id json.RawMessage, doc *document, start, end int, options formattingOptions) error {
	result, err := pawnfmt.FormatRange(doc.Text, start, end, pawnfmt.Options{
		TabSize: options.TabSize, UseTabs: !options.InsertSpaces,
	})
	if err != nil {
		return s.respondError(id, -32603, err.Error())
	}
	suffixLength := len(doc.Text) - result.FormattedRange.End
	formattedEnd := len(result.Source) - suffixLength
	if formattedEnd < result.FormattedRange.Start {
		return s.respondError(id, -32603, "formatter returned an invalid range")
	}
	replacement := result.Source[result.FormattedRange.Start:formattedEnd]
	original := doc.Text[result.FormattedRange.Start:result.FormattedRange.End]
	if string(replacement) == string(original) {
		return s.respond(id, []textEdit{})
	}
	return s.respond(id, []textEdit{{
		Range: offsetRange(doc.Text, result.FormattedRange.Start, result.FormattedRange.End), NewText: string(replacement),
	}})
}
