package lsp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pawnkit/pawn-analysis/preprocess"
	"github.com/pawnkit/pawn-api/pawnapi"
)

func (s *server) signatureHelp(id, raw json.RawMessage) error {
	var params textPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, nil)
	}
	name, opening, ok := callAt(doc.Text, int(offset))
	if !ok {
		return s.respond(id, nil)
	}
	signature, ok := callSignature(doc, name)
	if !ok {
		return s.respond(id, nil)
	}
	active := activeParameter(doc.Text, opening+1, int(offset))
	if len(signature.Parameters) > 0 && active >= len(signature.Parameters) {
		active = len(signature.Parameters) - 1
	}
	information := map[string]any{"label": signature.Label}
	if signature.Documentation != "" {
		information["documentation"] = map[string]any{"kind": "markdown", "value": signature.Documentation}
	}
	if len(signature.Parameters) > 0 {
		parameters := make([]map[string]any, 0, len(signature.Parameters))
		for _, parameter := range signature.Parameters {
			parameters = append(parameters, map[string]any{"label": parameter})
		}
		information["parameters"] = parameters
	}
	return s.respond(id, map[string]any{
		"signatures":      []any{information},
		"activeSignature": 0,
		"activeParameter": active,
	})
}

type signatureInformation struct {
	Label         string
	Documentation string
	Parameters    []string
}

func callSignature(doc *document, name string) (signatureInformation, bool) {
	if doc == nil || doc.Analysis == nil {
		return signatureInformation{}, false
	}
	if table := navigationTable(doc.Analysis); table != nil {
		for _, item := range table.Symbols {
			if item.Name == name && item.Kind.IsCallable() {
				declaration := localDeclaration(doc.Analysis, item)
				return signatureInformation{Label: declaration, Parameters: declarationParameters(declaration)}, true
			}
		}
	}
	if doc.Analysis.Preprocess != nil {
		if macro, ok := doc.Analysis.Preprocess.Macros[name]; ok && macro.Kind == preprocess.MacroFunctionLike {
			label := macroSignature(macro)
			return signatureInformation{Label: label, Parameters: declarationParameters(label)}, true
		}
	}
	entry, ok := apiEntry(doc.Names, name)
	if !ok || entry.Signature == nil {
		return signatureInformation{}, false
	}
	parameters := make([]string, 0, len(entry.Signature.Parameters))
	for _, parameter := range entry.Signature.Parameters {
		parameters = append(parameters, apiParameter(parameter))
	}
	return signatureInformation{
		Label: apiDeclaration(entry), Documentation: apiDocumentation(entry), Parameters: parameters,
	}, true
}

func apiParameter(parameter pawnapi.Parameter) string {
	value := parameter.Name
	if parameter.Tag != "" {
		value = parameter.Tag + ":" + value
	}
	if parameter.Reference {
		value = "&" + value
	}
	var dimensions strings.Builder
	for _, size := range parameter.ArrayDimensions {
		if size > 0 {
			dimensions.WriteByte('[')
			dimensions.WriteString(strconv.Itoa(size))
			dimensions.WriteByte(']')
		} else {
			dimensions.WriteString("[]")
		}
	}
	value += dimensions.String()
	if parameter.Variadic {
		value += "..."
	}
	if parameter.Default != nil {
		value += " = " + parameter.Default.String()
	}
	if parameter.Const {
		value = "const " + value
	}
	return value
}

func callAt(text []byte, offset int) (string, int, bool) {
	if offset < 0 || offset > len(text) {
		return "", 0, false
	}
	depth := 0
	for index := offset - 1; index >= 0; index-- {
		switch text[index] {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			end := index
			for end > 0 && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\n' || text[end-1] == '\r') {
				end--
			}
			start := end
			for start > 0 && identifierByte(text[start-1]) {
				start--
			}
			if start == end {
				return "", 0, false
			}
			return string(text[start:end]), index, true
		}
	}
	return "", 0, false
}

func activeParameter(text []byte, start, end int) int {
	if start < 0 || end > len(text) || start > end {
		return 0
	}
	active, depth := 0, 0
	var quote byte
	for index := start; index < end; index++ {
		value := text[index]
		if quote != 0 {
			if value == quote && (index == start || text[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch value {
		case '\'', '"':
			quote = value
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				active++
			}
		}
	}
	return active
}

func declarationParameters(declaration string) []string {
	opening := strings.IndexByte(declaration, '(')
	closing := strings.LastIndexByte(declaration, ')')
	if opening < 0 || closing <= opening+1 {
		return nil
	}
	text := declaration[opening+1 : closing]
	parameters := make([]string, 0)
	start, depth := 0, 0
	for index := range len(text) {
		switch text[index] {
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parameters = append(parameters, strings.TrimSpace(text[start:index]))
				start = index + 1
			}
		}
	}
	parameters = append(parameters, strings.TrimSpace(text[start:]))
	return parameters
}
