package lsp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pawnkit/pawn-analysis/symbol"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

func (s *server) prepareRename(id, raw json.RawMessage) error {
	var params textPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, nil)
	}
	name, start, end := identifierAt(doc.Text, int(offset))
	if _, _, ok := s.renameTarget(doc, offset, name); !ok {
		return s.respond(id, nil)
	}
	return s.respond(id, map[string]any{
		"range": offsetRange(doc.Text, start, end), "placeholder": name,
	})
}

func (s *server) rename(id, raw json.RawMessage) error {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position `json:"position"`
		NewName  string   `json:"newName"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	if !validPawnIdentifier(params.NewName) {
		return fmt.Errorf("invalid Pawn identifier %q", params.NewName)
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return errors.New("symbol is not available")
	}
	name, _, _ := identifierAt(doc.Text, int(offset))
	item, global, ok := s.renameTarget(doc, offset, name)
	if !ok {
		return fmt.Errorf("%q cannot be renamed", name)
	}
	if _, exists := apiEntry(doc.Names, params.NewName); exists {
		return fmt.Errorf("%q is an API symbol", params.NewName)
	}
	changes := make(map[string][]textEdit)
	if global {
		if workspaceDeclarationCount(s.workspaceOccurrences(params.NewName)) > 0 {
			return fmt.Errorf("%q is already declared in this workspace", params.NewName)
		}
		for _, occurrence := range s.workspaceOccurrences(name) {
			uri := occurrence.uri.String()
			changes[uri] = append(changes[uri], textEdit{
				Range: offsetRange(occurrence.text, int(occurrence.span.Start), int(occurrence.span.End)), NewText: params.NewName,
			})
		}
	} else {
		table := navigationTable(doc.Analysis)
		if existing, found := table.Lookup(item.Scope, params.NewName); found && existing.ID != item.ID {
			return fmt.Errorf("%q is already declared in this scope", params.NewName)
		}
		uri, text := spanDocument(doc, item.Span)
		changes[uri] = append(changes[uri], textEdit{
			Range: offsetRange(text, int(item.Span.Start), int(item.Span.End)), NewText: params.NewName,
		})
		for _, reference := range table.References {
			if reference.Resolved != item.ID {
				continue
			}
			uri, text := spanDocument(doc, reference.Span)
			changes[uri] = append(changes[uri], textEdit{
				Range: offsetRange(text, int(reference.Span.Start), int(reference.Span.End)), NewText: params.NewName,
			})
		}
	}
	if len(changes) == 0 {
		return fmt.Errorf("no references found for %q", name)
	}
	return s.respond(id, map[string]any{"changes": changes})
}

func (s *server) renameTarget(doc *document, offset coresource.Offset, name string) (symbol.Symbol, bool, bool) {
	if doc == nil || doc.Analysis == nil || name == "" {
		return symbol.Symbol{}, false, false
	}
	if _, ok := apiEntry(doc.Names, name); ok {
		return symbol.Symbol{}, false, false
	}
	if doc.Analysis.Preprocess != nil {
		if _, ok := doc.Analysis.Preprocess.Macros[name]; ok {
			return symbol.Symbol{}, false, false
		}
	}
	table := navigationTable(doc.Analysis)
	item, ok := symbolAt(table, doc.Analysis.File, offset)
	if ok {
		scope, found := table.Scope(item.Scope)
		return item, found && scope.Kind == symbol.ScopeFile, true
	}
	occurrences := s.workspaceOccurrences(name)
	if workspaceDeclarationCount(occurrences) != 1 {
		return symbol.Symbol{}, false, false
	}
	return symbol.Symbol{Name: name}, true, true
}

func workspaceDeclarationCount(items []workspaceOccurrence) int {
	count := 0
	for _, item := range items {
		if item.declaration {
			count++
		}
	}
	return count
}

func validPawnIdentifier(name string) bool {
	if name == "" || !identifierStartByte(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !identifierByte(name[index]) {
			return false
		}
	}
	return true
}

func identifierStartByte(value byte) bool {
	return value == '_' || value == '@' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
