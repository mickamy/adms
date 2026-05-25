package dialect

import (
	"fmt"
	"strings"
)

type postgresDialect struct{}

func Postgres() Dialect { return postgresDialect{} }

func (postgresDialect) Name() string { return "postgres" }

func (postgresDialect) Quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (postgresDialect) Placeholder(i int) string {
	return fmt.Sprintf("$%d", i)
}

func (postgresDialect) SupportsILIKE() bool { return true }

func (postgresDialect) SupportsReturning() bool { return true }

func (postgresDialect) JSONAgg(expr, orderBy string) string {
	if orderBy == "" {
		return fmt.Sprintf("json_agg(%s)", expr)
	}

	return fmt.Sprintf("json_agg(%s ORDER BY %s)", expr, orderBy)
}

func (postgresDialect) JSONObject(pairs []string) string {
	return fmt.Sprintf("json_build_object(%s)", strings.Join(pairs, ", "))
}

func (postgresDialect) EmptyJSONArray() string {
	return "'[]'::json"
}

// StringLiteral assumes standard_conforming_strings is on (the default since
// PostgreSQL 9.1), so backslashes are literal and only single quotes need to
// be doubled.
func (postgresDialect) StringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ContainmentExpr maps PostgREST's cs/cd to Postgres `@>` / `<@`. For
// json columns the column is upcast to jsonb so the operator applies
// (plain json does not implement `@>`). For array columns the value is
// cast to the column's array type so that the same expression works for
// text[], integer[], numeric(10,2)[], etc.
func (postgresDialect) ContainmentExpr(col, val, columnType string, contained bool) (string, error) {
	op := "@>"
	if contained {
		op = "<@"
	}

	t := strings.ToLower(strings.TrimSpace(columnType))
	switch {
	case t == "jsonb":
		return fmt.Sprintf("%s %s %s::jsonb", col, op, val), nil
	case t == "json":
		return fmt.Sprintf("%s::jsonb %s %s::jsonb", col, op, val), nil
	case strings.HasSuffix(t, "[]"):
		return fmt.Sprintf("%s %s %s::%s", col, op, val, t), nil
	}

	return "", fmt.Errorf("postgres: cs/cd not supported for column type %q", columnType)
}
