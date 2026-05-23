package query

import (
	"errors"
	"fmt"
	"strings"
)

// parseSelect parses a select expression such as "col1,col2", "*", or with
// relation embedding like "*,posts(id,title)" / "author:users(id,name)".
// Embedding may currently be only one level deep — the caller (build layer)
// is responsible for rejecting nested embeds it cannot translate.
func parseSelect(s string) ([]SelectItem, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	parts, err := splitTopLevel(s, ',')
	if err != nil {
		return nil, fmt.Errorf("invalid select %q: %w", s, err)
	}

	items := make([]SelectItem, 0, len(parts))
	for _, part := range parts {
		item, err := parseSelectItem(part)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, nil
}

// parseSelectItem parses a single select element. Forms accepted:
//   - "col"              → SelectItem{Column: "col"}
//   - "*"                → SelectItem{Column: "*"}
//   - "rel(inner)"       → SelectItem{Embed: {Relation: "rel", Items: inner}}
//   - "alias:rel(inner)" → SelectItem{Alias: "alias", Embed: {Relation: "rel", Items: inner}}
//
// Column-level aliases ("alias:col" without parens) are reserved for relation
// embeddings and rejected here.
func parseSelectItem(s string) (SelectItem, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SelectItem{}, errors.New("empty select item")
	}

	var alias string
	if i := strings.Index(s, ":"); i >= 0 {
		rest := strings.TrimSpace(s[i+1:])
		if !strings.Contains(rest, "(") {
			return SelectItem{}, fmt.Errorf(
				"aliased column %q is not supported yet (alias requires a relation embedding)", s)
		}

		alias = strings.TrimSpace(s[:i])
		if alias == "" {
			return SelectItem{}, fmt.Errorf("empty alias in %q", s)
		}

		s = rest
	}

	if openIdx := strings.Index(s, "("); openIdx >= 0 {
		if !strings.HasSuffix(s, ")") {
			return SelectItem{}, fmt.Errorf("invalid embedded select %q: unmatched parentheses", s)
		}

		relation := strings.TrimSpace(s[:openIdx])
		if relation == "" {
			return SelectItem{}, fmt.Errorf("empty relation name in %q", s)
		}

		if strings.ContainsAny(relation, " \t():,") {
			return SelectItem{}, fmt.Errorf("invalid relation name %q", relation)
		}

		innerItems, err := parseSelect(s[openIdx+1 : len(s)-1])
		if err != nil {
			return SelectItem{}, err
		}

		return SelectItem{
			Alias: alias,
			Embed: &Embed{
				Relation: relation,
				Items:    innerItems,
			},
		}, nil
	}

	if alias != "" {
		return SelectItem{}, fmt.Errorf("alias %q requires a relation embedding", alias)
	}

	if strings.ContainsAny(s, "():,") {
		return SelectItem{}, fmt.Errorf("invalid select item %q", s)
	}

	return SelectItem{Column: s}, nil
}
