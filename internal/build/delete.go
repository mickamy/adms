package build

import (
	"fmt"
	"strings"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

// Delete compiles a DELETE into a SQL statement and bound args.
//
// q.Filter feeds the WHERE clause; a nil Filter omits WHERE entirely.
// The HTTP layer must reject unfiltered DELETE requests (PostgREST
// returns 400 on unfiltered writes) — Delete itself is
// filter-agnostic so other callers can opt into table-wide deletes.
//
// withReturning appends "RETURNING *" when the dialect supports it.
func Delete(
	t *schema.Table,
	q query.Query,
	d dialect.Dialect,
	withReturning bool,
) (string, []any, error) {
	var b strings.Builder

	var args []any

	fmt.Fprintf(&b, "DELETE FROM %s", qualifiedTable(t, d))

	if q.Filter != nil {
		colSet := columnSet(t)

		where, err := buildWhere(q.Filter, t, d, colSet, &args)
		if err != nil {
			return "", nil, err
		}

		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	if withReturning && d.SupportsReturning() {
		b.WriteString(" RETURNING *")
	}

	return b.String(), args, nil
}
