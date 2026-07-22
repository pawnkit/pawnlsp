package lsp

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"sort"

	analysis "github.com/pawnkit/pawn-analysis"
	"github.com/pawnkit/pawn-analysis/symbol"
	coresource "github.com/pawnkit/pawnkit-core/source"
)

type callHierarchyData struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

func (s *server) prepareCallHierarchy(id, raw json.RawMessage) error {
	var params textPositionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	doc := s.readyDocument(params.TextDocument.URI)
	offset, ok := documentOffset(doc, params.Position)
	if !ok {
		return s.respond(id, []any{})
	}
	item, ok := symbolAt(navigationTable(doc.Analysis), doc.Analysis.File, offset)
	if !ok || !item.Kind.IsCallable() {
		return s.respond(id, []any{})
	}
	return s.respond(id, []any{callHierarchyItem(doc.URI, doc.Text, item)})
}

func (s *server) incomingCalls(id, raw json.RawMessage) error {
	data, err := callHierarchyParams(raw)
	if err != nil {
		return err
	}
	results := s.workspaceResults()
	declarations := workspaceCallableDeclarations(results, data.Name)
	if len(declarations) != 1 || declarations[0].uri.String() != data.URI {
		return s.respond(id, []any{})
	}

	type groupedCall struct {
		from   map[string]any
		ranges []lspRange
	}
	groups := make(map[string]*groupedCall)
	for uri, result := range results {
		table := navigationTable(result)
		if table == nil {
			continue
		}
		text := analysisSource(result)
		for _, reference := range table.References {
			if !reference.IsCall || reference.Name != data.Name {
				continue
			}
			caller, ok := enclosingCallable(table, reference.Scope)
			if !ok {
				continue
			}
			key := uri.String() + ":" + caller.Name
			group := groups[key]
			if group == nil {
				group = &groupedCall{from: callHierarchyItem(uri.String(), text, caller)}
				groups[key] = group
			}
			group.ranges = append(group.ranges, offsetRange(text, int(reference.Span.Start), int(reference.Span.End)))
		}
	}
	keys := sortedKeys(groups)
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, map[string]any{"from": groups[key].from, "fromRanges": groups[key].ranges})
	}
	return s.respond(id, items)
}

func (s *server) outgoingCalls(id, raw json.RawMessage) error {
	data, err := callHierarchyParams(raw)
	if err != nil {
		return err
	}
	results := s.workspaceResults()
	result := results[coresource.URI(data.URI)]
	if result == nil {
		return s.respond(id, []any{})
	}
	table := navigationTable(result)
	caller, ok := callableByName(table, data.Name)
	if !ok || caller.FuncScope == 0 {
		return s.respond(id, []any{})
	}

	type groupedCall struct {
		to     map[string]any
		ranges []lspRange
	}
	groups := make(map[string]*groupedCall)
	text := analysisSource(result)
	for _, reference := range table.References {
		if !reference.IsCall || !scopeWithin(table, reference.Scope, caller.FuncScope) {
			continue
		}
		declarations := workspaceCallableDeclarations(results, reference.Name)
		if len(declarations) != 1 {
			continue
		}
		target := declarations[0]
		key := target.uri.String() + ":" + target.symbol.Name
		group := groups[key]
		if group == nil {
			group = &groupedCall{to: callHierarchyItem(target.uri.String(), target.text, target.symbol)}
			groups[key] = group
		}
		group.ranges = append(group.ranges, offsetRange(text, int(reference.Span.Start), int(reference.Span.End)))
	}
	keys := sortedKeys(groups)
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, map[string]any{"to": groups[key].to, "fromRanges": groups[key].ranges})
	}
	return s.respond(id, items)
}

type callableDeclaration struct {
	uri    coresource.URI
	text   []byte
	symbol symbol.Symbol
}

func workspaceCallableDeclarations(results map[coresource.URI]*analysis.Result, name string) []callableDeclaration {
	byFile := make(map[coresource.URI]callableDeclaration)
	for uri, result := range results {
		table := navigationTable(result)
		if table == nil {
			continue
		}
		for _, item := range table.Symbols {
			scope, ok := table.Scope(item.Scope)
			if ok && scope.Kind == symbol.ScopeFile && item.Kind.IsCallable() && item.Name == name {
				current, exists := byFile[uri]
				if !exists || current.symbol.FuncScope == 0 && item.FuncScope != 0 {
					byFile[uri] = callableDeclaration{uri: uri, text: analysisSource(result), symbol: item}
				}
			}
		}
	}
	items := make([]callableDeclaration, 0, len(byFile))
	for _, item := range byFile {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].uri.String() < items[j].uri.String() })
	return items
}

func (s *server) workspaceResults() map[coresource.URI]*analysis.Result {
	s.mu.Lock()
	indexes := make([]*workspaceIndex, 0, len(s.workspaces))
	for _, index := range s.workspaces {
		indexes = append(indexes, index)
	}
	documents := make([]*document, 0, len(s.documents))
	for _, doc := range s.documents {
		documents = append(documents, doc)
	}
	s.mu.Unlock()
	results := make(map[coresource.URI]*analysis.Result)
	for _, index := range indexes {
		<-index.ready
		maps.Copy(results, index.files)
	}
	for _, doc := range documents {
		<-doc.ready
		if doc.Analysis != nil {
			results[coresource.URI(doc.URI)] = doc.Analysis
		}
	}
	return results
}

func callHierarchyParams(raw json.RawMessage) (callHierarchyData, error) {
	var params struct {
		Item struct {
			Data callHierarchyData `json:"data"`
		} `json:"item"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return callHierarchyData{}, err
	}
	return params.Item.Data, nil
}

func callHierarchyItem(uri string, text []byte, item symbol.Symbol) map[string]any {
	rng := offsetRange(text, int(item.Span.Start), int(item.Span.End))
	result := map[string]any{
		"name": item.Name, "kind": 12, "uri": uri, "range": rng, "selectionRange": rng,
		"data": callHierarchyData{Name: item.Name, URI: uri},
	}
	if path, err := coresource.URI(uri).Filename(); err == nil {
		result["detail"] = filepath.Base(path)
	}
	return result
}

func enclosingCallable(table *symbol.Table, scopeID symbol.ID) (symbol.Symbol, bool) {
	for scopeID != 0 {
		for _, item := range table.Symbols {
			if item.FuncScope == scopeID {
				return item, true
			}
		}
		scope, ok := table.Scope(scopeID)
		if !ok {
			break
		}
		scopeID = scope.Parent
	}
	return symbol.Symbol{}, false
}

func scopeWithin(table *symbol.Table, scopeID, parent symbol.ID) bool {
	for scopeID != 0 {
		if scopeID == parent {
			return true
		}
		scope, ok := table.Scope(scopeID)
		if !ok {
			return false
		}
		scopeID = scope.Parent
	}
	return false
}

func callableByName(table *symbol.Table, name string) (symbol.Symbol, bool) {
	if table == nil {
		return symbol.Symbol{}, false
	}
	for _, item := range table.Symbols {
		if item.Name == name && item.Kind.IsCallable() {
			return item, true
		}
	}
	return symbol.Symbol{}, false
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
