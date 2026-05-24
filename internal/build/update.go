package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

// Update compiles an UPDATE into a SQL statement and bound args.
//
// set holds the columns to update; column names are validated against
// the table. q.Filter feeds the WHERE clause: a nil Filter omits WHERE
// entirely so the statement would touch every row. The HTTP layer must
// reject unfiltered PATCH requests (PostgREST returns 400 on
// unfiltered writes) — Update itself is filter-agnostic so other
// callers (e.g., admin tooling) can opt into bulk updates.
//
// withReturning appends "RETURNING *" when the dialect supports it.
func Update(
	t *schema.Table,
	set map[string]any,
	q query.Query,
	d dialect.Dialect,
	withReturning bool,
) (string, []any, error) {
	if len(set) == 0 {
		return "", nil, errors.New("update: set is empty")
	}

	cols := sortedKeys(set)
	colSet := columnSet(t)

	for _, c := range cols {
		if _, ok := colSet[c]; !ok {
			return "", nil, fmt.Errorf("unknown column %q on table %q", c, t.Name)
		}
	}

	var b strings.Builder

	args := make([]any, 0, len(cols))

	fmt.Fprintf(&b, "UPDATE %s SET ", qualifiedTable(t, d))

	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}

		args = append(args, set[c])
		fmt.Fprintf(&b, "%s = %s", d.Quote(c), d.Placeholder(len(args)))
	}

	if q.Filter != nil {
		where, err := buildWhere(q.Filter, t, d, colSet, &args)
		if err != nil {
			return "", nil, &FilterError{Err: err}
		}

		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	if withReturning && d.SupportsReturning() {
		b.WriteString(" RETURNING *")
	}

	return b.String(), args, nil
}
