package build

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/schema"
)

// Insert compiles an INSERT into a SQL statement and bound args.
//
// Every row in rows must share the same set of keys. Allowing missing
// keys would silently NULL-fill columns in a bulk insert and surprise
// callers, so the builder rejects rows with mismatched key sets.
//
// Column names are validated against the table's column set. The
// builder does not pre-screen generated / identity columns; if the
// caller supplies a value the database will surface the appropriate
// error.
//
// withReturning appends "RETURNING *" when the dialect supports it.
// Dialects without RETURNING (e.g., MySQL) ignore the flag; callers
// that need the inserted rows there must run a follow-up SELECT or
// reject the request.
func Insert(
	t *schema.Table,
	rows []map[string]any,
	d dialect.Dialect,
	withReturning bool,
) (string, []any, error) {
	if len(rows) == 0 {
		return "", nil, errors.New("insert: rows is empty")
	}

	cols := sortedKeys(rows[0])
	if len(cols) == 0 {
		return "", nil, errors.New("insert: row has no columns")
	}

	colSet := columnSet(t)
	for _, c := range cols {
		if _, ok := colSet[c]; !ok {
			return "", nil, fmt.Errorf("unknown column %q on table %q", c, t.Name)
		}
	}

	first := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		first[c] = struct{}{}
	}

	for i := 1; i < len(rows); i++ {
		if len(rows[i]) != len(cols) {
			return "", nil, fmt.Errorf(
				"insert: row %d has %d keys, want %d (rows must share columns)",
				i, len(rows[i]), len(cols))
		}

		for k := range rows[i] {
			if _, ok := first[k]; !ok {
				return "", nil, fmt.Errorf(
					"insert: row %d has unexpected column %q (rows must share columns)",
					i, k)
			}
		}
	}

	var b strings.Builder

	fmt.Fprintf(&b, "INSERT INTO %s (", qualifiedTable(t, d))

	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(d.Quote(c))
	}

	b.WriteString(") VALUES ")

	args := make([]any, 0, len(cols)*len(rows))

	for ri, row := range rows {
		if ri > 0 {
			b.WriteString(", ")
		}

		b.WriteByte('(')

		for ci, c := range cols {
			if ci > 0 {
				b.WriteString(", ")
			}

			args = append(args, row[c])
			b.WriteString(d.Placeholder(len(args)))
		}

		b.WriteByte(')')
	}

	if withReturning && d.SupportsReturning() {
		b.WriteString(" RETURNING *")
	}

	return b.String(), args, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
