package query

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Parse converts url.Values into a Query. It only enforces syntactic
// shape: identifier validation against the schema and limit clamping
// are the caller's responsibility.
//
// Reserved keys: select, order, limit, offset, and, or. Every other
// key is treated as a column filter; multiple values for the same
// column are combined with logical AND, the same way multiple distinct
// filter keys are combined.
func Parse(values url.Values) (Query, error) {
	var q Query

	s, err := singleValue(values, "select")
	if err != nil {
		return Query{}, err
	}
	if s != "" {
		items, err := parseSelect(s)
		if err != nil {
			return Query{}, err
		}
		q.Select = items
	}

	s, err = singleValue(values, "order")
	if err != nil {
		return Query{}, err
	}
	if s != "" {
		items, err := parseOrder(s)
		if err != nil {
			return Query{}, err
		}
		q.Order = items
	}

	s, err = singleValue(values, "limit")
	if err != nil {
		return Query{}, err
	}
	if s != "" {
		n, err := parseNonNegativeInt(s)
		if err != nil {
			return Query{}, fmt.Errorf("invalid limit %q: %w", s, err)
		}
		q.Limit = &n
	}

	s, err = singleValue(values, "offset")
	if err != nil {
		return Query{}, err
	}
	if s != "" {
		n, err := parseNonNegativeInt(s)
		if err != nil {
			return Query{}, fmt.Errorf("invalid offset %q: %w", s, err)
		}
		q.Offset = &n
	}

	nodes, err := parseFilters(values)
	if err != nil {
		return Query{}, err
	}

	switch len(nodes) {
	case 0:
	case 1:
		q.Filter = nodes[0]
	default:
		q.Filter = FilterGroup{Op: LogicalAnd, Nodes: nodes}
	}

	return q, nil
}

// singleValue returns the sole value for key, or "" if key is absent.
// It errors when the key appears more than once; this protects against
// ambiguous requests like "?limit=10&limit=20".
func singleValue(values url.Values, key string) (string, error) {
	vs := values[key]
	if len(vs) > 1 {
		return "", fmt.Errorf("%s may appear at most once, got %d values", key, len(vs))
	}

	if len(vs) == 0 {
		return "", nil
	}

	return vs[0], nil
}

func parseFilters(values url.Values) ([]FilterNode, error) {
	var nodes []FilterNode
	for _, key := range sortedKeys(values) {
		switch key {
		case "select", "order", "limit", "offset":
			continue
		case "and", "or":
			op := LogicalAnd
			if key == "or" {
				op = LogicalOr
			}

			for _, v := range values[key] {
				g, err := parseGroup(v, op)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, g)
			}
		default:
			for _, v := range values[key] {
				p, err := parsePredicate(key, v)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, p)
			}
		}
	}

	return nodes, nil
}

func sortedKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseNonNegativeInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %w", err)
	}

	if n < 0 {
		return 0, errors.New("must be non-negative")
	}

	return n, nil
}

func parseOrder(s string) ([]OrderItem, error) {
	parts := strings.Split(s, ",")
	items := make([]OrderItem, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty order item in %q", s)
		}

		col := p
		desc := false
		if i := strings.LastIndex(p, "."); i >= 0 {
			switch p[i+1:] {
			case "asc":
				col = strings.TrimSpace(p[:i])
			case "desc":
				col = strings.TrimSpace(p[:i])
				desc = true
			default:
				return nil, fmt.Errorf("invalid order direction in %q: want asc or desc", p)
			}

			if strings.Contains(col, ".") {
				return nil, fmt.Errorf("invalid order column %q: contains '.'", col)
			}
		}

		if col == "" {
			return nil, fmt.Errorf("empty order column in %q", p)
		}

		items = append(items, OrderItem{Column: col, Desc: desc})
	}

	return items, nil
}

// parsePredicate parses a single "column = value" pair where value is
// "op.literal", "not.op.literal", "in.(v1,v2,...)", or "is.null|true|false".
func parsePredicate(column, value string) (Predicate, error) {
	column = strings.TrimSpace(column)
	if column == "" {
		return Predicate{}, errors.New("invalid filter: empty column name")
	}

	not := false
	raw := value

	if strings.HasPrefix(raw, "not.") {
		not = true
		raw = raw[len("not."):]
	}

	opStr, val, ok := strings.Cut(raw, ".")
	if !ok {
		return Predicate{}, fmt.Errorf("invalid filter %q=%q: missing operator separator", column, value)
	}

	op, err := parseOperator(opStr)
	if err != nil {
		return Predicate{}, fmt.Errorf("invalid filter %q=%q: %w", column, value, err)
	}

	switch op {
	case OpIs:
		switch val {
		case "null", "true", "false":
		default:
			return Predicate{}, fmt.Errorf("invalid is value %q: want null/true/false", val)
		}
	case OpIn:
		if !strings.HasPrefix(val, "(") || !strings.HasSuffix(val, ")") {
			return Predicate{}, fmt.Errorf("invalid in value %q: want (v1,v2,...)", val)
		}
		val = val[1 : len(val)-1]
		if val == "" {
			return Predicate{}, errors.New("invalid in value: list is empty")
		}
	case OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpLike, OpILike:
		// scalar operators take the value as-is
	}

	return Predicate{
		Column: column,
		Op:     op,
		Value:  val,
		Not:    not,
	}, nil
}

// parseGroup parses an "(...)" body of a top-level or= or and= group.
func parseGroup(s string, op LogicalOp) (FilterGroup, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return FilterGroup{}, fmt.Errorf("invalid group %q: want (...)", s)
	}

	return parseGroupBody(s[1:len(s)-1], op)
}

func parseGroupBody(inner string, op LogicalOp) (FilterGroup, error) {
	parts, err := splitTopLevel(inner, ',')
	if err != nil {
		return FilterGroup{}, fmt.Errorf("invalid group: %w", err)
	}

	nodes := make([]FilterNode, 0, len(parts))
	for _, part := range parts {
		n, err := parseFilterElement(part)
		if err != nil {
			return FilterGroup{}, err
		}
		nodes = append(nodes, n)
	}

	return FilterGroup{Op: op, Nodes: nodes}, nil
}

// parseFilterElement parses one comma-separated element inside a group
// body: either a nested "or=(...)" / "and=(...)" group or a flat
// "column.op.value" predicate.
func parseFilterElement(s string) (FilterNode, error) {
	s = strings.TrimSpace(s)

	if rest, ok := strings.CutPrefix(s, "or=("); ok {
		if !strings.HasSuffix(rest, ")") {
			return nil, fmt.Errorf("invalid nested group %q", s)
		}
		return parseGroupBody(rest[:len(rest)-1], LogicalOr)
	}

	if rest, ok := strings.CutPrefix(s, "and=("); ok {
		if !strings.HasSuffix(rest, ")") {
			return nil, fmt.Errorf("invalid nested group %q", s)
		}
		return parseGroupBody(rest[:len(rest)-1], LogicalAnd)
	}

	col, rest, ok := strings.Cut(s, ".")
	if !ok {
		return nil, fmt.Errorf("invalid filter element %q", s)
	}
	return parsePredicate(col, rest)
}

// splitTopLevel splits s by sep while respecting paren nesting.
// "a,b,(c,d)" with sep=',' yields ["a", "b", "(c,d)"].
func splitTopLevel(s string, sep rune) ([]string, error) {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unmatched ')' in %q", s)
			}
		}

		if r == sep && depth == 0 {
			parts = append(parts, s[start:i])
			start = i + len(string(sep))
		}
	}

	if depth != 0 {
		return nil, fmt.Errorf("unmatched '(' in %q", s)
	}

	parts = append(parts, s[start:])
	return parts, nil
}
