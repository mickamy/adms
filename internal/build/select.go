package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

// Select converts a parsed Query against the given table into a SELECT
// statement and bound arguments. It validates that every referenced
// identifier exists on the table (select / filter / order) before emitting
// SQL, so callers can rely on a fully sanitized statement.
//
// Limit handling: q.Limit is clamped to [1, maxLimit]; nil falls back to
// defaultLimit. Offset defaults to 0. Both are emitted as literal integers
// since the parser validates them as non-negative ints.
//
// defaultLimit and maxLimit must both be positive, and defaultLimit must
// not exceed maxLimit; otherwise Select returns an error without inspecting
// the rest of the query.
func Select(
	q query.Query,
	t *schema.Table,
	lookup SchemaLookup,
	d dialect.Dialect,
	defaultLimit, maxLimit int,
) (string, []any, []string, error) {
	if defaultLimit <= 0 {
		return "", nil, nil, fmt.Errorf("defaultLimit must be positive, got %d", defaultLimit)
	}

	if maxLimit <= 0 {
		return "", nil, nil, fmt.Errorf("maxLimit must be positive, got %d", maxLimit)
	}

	if defaultLimit > maxLimit {
		return "", nil, nil, fmt.Errorf("defaultLimit %d must not exceed maxLimit %d", defaultLimit, maxLimit)
	}

	columns := columnSet(t)

	cols, embedAliases, err := buildSelectClause(q.Select, t, lookup, d, columns)
	if err != nil {
		return "", nil, nil, err
	}

	var args []any
	var where string
	if q.Filter != nil {
		where, err = buildWhere(q.Filter, t, d, columns, &args)
		if err != nil {
			return "", nil, nil, err
		}
	}

	orderBy, err := buildOrderBy(q.Order, t, d, columns)
	if err != nil {
		return "", nil, nil, err
	}

	limit := defaultLimit
	if q.Limit != nil {
		limit = *q.Limit
	}

	if limit > maxLimit {
		limit = maxLimit
	}

	if limit < 1 {
		limit = 1
	}

	offset := 0
	if q.Offset != nil {
		offset = *q.Offset
	}

	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s", cols, qualifiedTable(t, d))

	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	if orderBy != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(orderBy)
	}

	fmt.Fprintf(&b, " LIMIT %d OFFSET %d", limit, offset)

	return b.String(), args, embedAliases, nil
}

func qualifiedTable(t *schema.Table, d dialect.Dialect) string {
	if t.Schema == "" {
		return d.Quote(t.Name)
	}

	return d.Quote(t.Schema) + "." + d.Quote(t.Name)
}

func buildSelectClause(
	items []query.SelectItem,
	t *schema.Table,
	lookup SchemaLookup,
	d dialect.Dialect,
	columns map[string]struct{},
) (string, []string, error) {
	if len(items) == 0 {
		return "*", nil, nil
	}

	// First pass: validate "*" usage and decide whether to seed `seen` with
	// the base-table columns so embed aliases cannot collide with them.
	var hasStar, hasNamedCol bool

	for _, it := range items {
		if it.Embed != nil {
			continue
		}

		if it.Column == "*" {
			if hasStar {
				return "", nil, errors.New("select '*' specified more than once")
			}

			hasStar = true
			continue
		}

		hasNamedCol = true
	}

	if hasStar && hasNamedCol {
		return "", nil, errors.New("select cannot mix '*' with named columns")
	}

	seen := make(map[string]struct{}, len(items))

	if hasStar {
		for _, c := range t.Columns {
			seen[c.Name] = struct{}{}
		}
	}

	// Second pass: emit SQL fragments and detect duplicates.
	parts := make([]string, 0, len(items))
	var embedAliases []string

	for _, it := range items {
		if it.Embed != nil {
			if lookup == nil {
				return "", nil, fmt.Errorf("embedded relation %q requires a schema lookup", it.Embed.Relation)
			}

			alias := it.Alias
			if alias == "" {
				alias = it.Embed.Relation
			}

			if _, dup := seen[alias]; dup {
				return "", nil, fmt.Errorf("duplicate select alias %q", alias)
			}

			seen[alias] = struct{}{}

			sub, err := buildEmbedSubquery(it, t, lookup, d)
			if err != nil {
				return "", nil, err
			}

			parts = append(parts, sub)
			embedAliases = append(embedAliases, alias)
			continue
		}

		if it.Column == "*" {
			parts = append(parts, "*")
			continue
		}

		if _, ok := columns[it.Column]; !ok {
			return "", nil, fmt.Errorf("unknown column %q on table %q", it.Column, t.Name)
		}

		if _, dup := seen[it.Column]; dup {
			return "", nil, fmt.Errorf("duplicate select column %q", it.Column)
		}

		seen[it.Column] = struct{}{}

		parts = append(parts, d.Quote(it.Column))
	}

	return strings.Join(parts, ", "), embedAliases, nil
}

func buildWhere(
	node query.FilterNode,
	t *schema.Table,
	d dialect.Dialect,
	columns map[string]struct{},
	args *[]any,
) (string, error) {
	switch n := node.(type) {
	case query.Predicate:
		return buildPredicate(n, t, d, columns, args)
	case query.FilterGroup:
		parts := make([]string, 0, len(n.Nodes))
		for _, child := range n.Nodes {
			s, err := buildWhere(child, t, d, columns, args)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}

		sep := " AND "
		if n.Op == query.LogicalOr {
			sep = " OR "
		}

		return "(" + strings.Join(parts, sep) + ")", nil
	default:
		return "", fmt.Errorf("internal: unknown filter node %T", node)
	}
}

func buildPredicate(
	p query.Predicate,
	t *schema.Table,
	d dialect.Dialect,
	columns map[string]struct{},
	args *[]any,
) (string, error) {
	if _, ok := columns[p.Column]; !ok {
		return "", fmt.Errorf("unknown column %q on table %q", p.Column, t.Name)
	}

	quoted := d.Quote(p.Column)

	var expr string
	var err error
	switch p.Op {
	case query.OpEq:
		expr = scalarOp(quoted, "=", p.Value, d, args)
	case query.OpNeq:
		expr = scalarOp(quoted, "<>", p.Value, d, args)
	case query.OpGt:
		expr = scalarOp(quoted, ">", p.Value, d, args)
	case query.OpGte:
		expr = scalarOp(quoted, ">=", p.Value, d, args)
	case query.OpLt:
		expr = scalarOp(quoted, "<", p.Value, d, args)
	case query.OpLte:
		expr = scalarOp(quoted, "<=", p.Value, d, args)
	case query.OpLike:
		expr = scalarOp(quoted, "LIKE", p.Value, d, args)
	case query.OpILike:
		expr = ilikeOp(quoted, p.Value, d, args)
	case query.OpIn:
		expr, err = inOp(quoted, p.Value, d, args)
	case query.OpIs:
		expr, err = isOp(quoted, p.Value)
	default:
		return "", fmt.Errorf("internal: unknown operator %v", p.Op)
	}

	if err != nil {
		return "", err
	}

	if p.Not {
		return "NOT (" + expr + ")", nil
	}

	return expr, nil
}

func scalarOp(quotedCol, op, value string, d dialect.Dialect, args *[]any) string {
	*args = append(*args, value)
	return fmt.Sprintf("%s %s %s", quotedCol, op, d.Placeholder(len(*args)))
}

func ilikeOp(quotedCol, value string, d dialect.Dialect, args *[]any) string {
	if d.SupportsILIKE() {
		return scalarOp(quotedCol, "ILIKE", value, d, args)
	}

	// Fallback for dialects without ILIKE (MySQL): compare lower-cased values.
	*args = append(*args, value)
	return fmt.Sprintf("LOWER(%s) LIKE LOWER(%s)", quotedCol, d.Placeholder(len(*args)))
}

func inOp(quotedCol, csv string, d dialect.Dialect, args *[]any) (string, error) {
	parts := strings.Split(csv, ",")
	placeholders := make([]string, 0, len(parts))
	for _, v := range parts {
		v = strings.TrimSpace(v)
		if v == "" {
			return "", fmt.Errorf("invalid in value: empty element in %q", csv)
		}

		*args = append(*args, v)
		placeholders = append(placeholders, d.Placeholder(len(*args)))
	}

	return fmt.Sprintf("%s IN (%s)", quotedCol, strings.Join(placeholders, ", ")), nil
}

func isOp(quotedCol, literal string) (string, error) {
	switch literal {
	case "null":
		return quotedCol + " IS NULL", nil
	case "true":
		return quotedCol + " IS TRUE", nil
	case "false":
		return quotedCol + " IS FALSE", nil
	default:
		return "", fmt.Errorf("internal: invalid is literal %q", literal)
	}
}

func buildOrderBy(
	items []query.OrderItem,
	t *schema.Table,
	d dialect.Dialect,
	columns map[string]struct{},
) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(items))
	for _, it := range items {
		if _, ok := columns[it.Column]; !ok {
			return "", fmt.Errorf("unknown column %q on table %q", it.Column, t.Name)
		}

		dir := "ASC"
		if it.Desc {
			dir = "DESC"
		}

		parts = append(parts, d.Quote(it.Column)+" "+dir)
	}

	return strings.Join(parts, ", "), nil
}

func columnSet(t *schema.Table) map[string]struct{} {
	set := make(map[string]struct{}, len(t.Columns))
	for _, c := range t.Columns {
		set[c.Name] = struct{}{}
	}

	return set
}
