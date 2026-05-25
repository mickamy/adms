package dialect

import (
	"fmt"
	"strings"
)

type mysqlDialect struct{}

func MySQL() Dialect { return mysqlDialect{} }

func (mysqlDialect) Name() string { return "mysql" }

func (mysqlDialect) Quote(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

func (mysqlDialect) Placeholder(_ int) string {
	return "?"
}

func (mysqlDialect) SupportsILIKE() bool { return false }

func (mysqlDialect) SupportsReturning() bool { return false }

// JSONAgg returns a JSON_ARRAYAGG expression. MySQL 8 does not accept
// ORDER BY inside JSON_ARRAYAGG, so the orderBy argument is accepted
// for API parity with the Postgres dialect but ignored — callers must
// sort the source query themselves.
func (mysqlDialect) JSONAgg(expr, _ string) string {
	return fmt.Sprintf("JSON_ARRAYAGG(%s)", expr)
}

func (mysqlDialect) JSONObject(pairs []string) string {
	return fmt.Sprintf("JSON_OBJECT(%s)", strings.Join(pairs, ", "))
}

func (mysqlDialect) EmptyJSONArray() string {
	return "JSON_ARRAY()"
}

// StringLiteral escapes both backslashes and single quotes. MySQL treats a
// backslash as an escape character by default (NO_BACKSLASH_ESCAPES off), so
// emitting "\'" inside a literal would otherwise be parsed as an escaped
// quote and break the surrounding statement.
func (mysqlDialect) StringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}

// ContainmentExpr maps PostgREST's cs/cd to MySQL JSON_CONTAINS. MySQL
// has no native array type, so only `json` columns are accepted; other
// types are rejected with an error so callers fail loudly instead of
// emitting nonsense SQL.
func (mysqlDialect) ContainmentExpr(col, val, columnType string, contained bool) (string, error) {
	t := strings.ToLower(strings.TrimSpace(columnType))
	if t != "json" {
		return "", fmt.Errorf("mysql: cs/cd not supported for column type %q (json only)", columnType)
	}

	if contained {
		return fmt.Sprintf("JSON_CONTAINS(%s, %s)", val, col), nil
	}

	return fmt.Sprintf("JSON_CONTAINS(%s, %s)", col, val), nil
}
