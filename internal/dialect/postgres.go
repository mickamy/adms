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
