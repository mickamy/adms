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
