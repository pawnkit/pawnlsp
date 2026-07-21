package lsp

import (
	"encoding/json"
	"strings"

	parser "github.com/pawnkit/pawn-parser"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func (s *server) inlayHints(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	if doc == nil || doc.Analysis == nil || doc.Analysis.Parse == nil {
		return s.respond(id, []any{})
	}
	index := coresource.NewLineIndex(string(doc.Text))
	start, err := index.Offset(coresource.Position{Line: params.Range.Start.Line, Character: params.Range.Start.Character}, coresource.UTF16)
	if err != nil {
		return s.respond(id, []any{})
	}
	end, err := index.Offset(coresource.Position{Line: params.Range.End.Line, Character: params.Range.End.Character}, coresource.UTF16)
	if err != nil {
		return s.respond(id, []any{})
	}
	hints := make([]map[string]any, 0)
	walkSyntax(doc.Analysis.Parse.Syntax(), func(node parser.SyntaxNode) {
		call, ok := parser.AsCall(node)
		if !ok {
			return
		}
		rng := node.Range()
		if rng.End < int(start) || rng.Start > int(end) {
			return
		}
		function, ok := call.Function()
		if !ok {
			return
		}
		signature, ok := s.callSignature(doc, function.Text())
		if !ok {
			return
		}
		arguments := call.Arguments()
		argumentIndex := 0
		for arguments.Next() && argumentIndex < len(signature.Parameters) {
			argument := arguments.Node()
			name := parameterName(signature.Parameters[argumentIndex])
			argumentIndex++
			if name == "" || argument.Kind() == parser.KindArgumentName || argument.Text() == name {
				continue
			}
			hints = append(hints, map[string]any{
				"position":     offsetPosition(doc.Text, argument.Range().Start),
				"label":        name + ":",
				"kind":         2,
				"paddingRight": true,
			})
		}
	})
	return s.respond(id, hints)
}

func parameterName(parameter string) string {
	parameter, _, _ = strings.Cut(parameter, "=")
	parameter = strings.TrimSpace(parameter)
	parameter = strings.TrimPrefix(parameter, "const ")
	parameter = strings.TrimPrefix(parameter, "&")
	if colon := strings.LastIndexByte(parameter, ':'); colon >= 0 {
		parameter = parameter[colon+1:]
	}
	if bracket := strings.IndexByte(parameter, '['); bracket >= 0 {
		parameter = parameter[:bracket]
	}
	return strings.TrimSuffix(strings.TrimSpace(parameter), "...")
}
