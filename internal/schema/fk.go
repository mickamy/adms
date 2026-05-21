package schema

import (
	"database/sql"
	"fmt"
)

// attachFKs consumes FK rows in
// (ownerSchema, ownerName, conname, linkedSchema, linkedName, col, refCol, ord) order,
// groups them per constrained, and appends the resulting ForeignKeys to the table
// identified by (ownerSchema, ownerName) in index. qualify renders a (schema, name)
// pair into the dialect's preferred notation for ForeignKey.Table.
func attachFKs(rows *sql.Rows, qualify func(schema, name string) string,
	index map[tableKey]*Table, direction fkDirection,
) error {
	type ownerKey struct {
		schema, name, conname, linkedSchema, linkedName string
	}

	type fkAccum struct {
		owner tableKey
		fk    *ForeignKey
	}

	accum := make(map[ownerKey]*fkAccum)
	order := make([]ownerKey, 0)

	for rows.Next() {
		var (
			ownerSchema, ownerName, cname, linkedSchema, linkedName, col, refCol string
			ord                                                                  int
		)

		if err := rows.Scan(&ownerSchema, &ownerName, &cname,
			&linkedSchema, &linkedName, &col, &refCol, &ord); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		key := ownerKey{ownerSchema, ownerName, cname, linkedSchema, linkedName}
		entry, exists := accum[key]
		if !exists {
			entry = &fkAccum{
				owner: tableKey{ownerSchema, ownerName},
				fk:    &ForeignKey{Table: qualify(linkedSchema, linkedName)},
			}
			accum[key] = entry
			order = append(order, key)
		}

		entry.fk.Columns = append(entry.fk.Columns, col)
		entry.fk.References = append(entry.fk.References, refCol)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	for _, key := range order {
		entry := accum[key]

		t, found := index[entry.owner]
		if !found {
			continue
		}

		switch direction {
		case fkDirectionForward:
			t.ForeignKeys = append(t.ForeignKeys, *entry.fk)
		case fkDirectionReverse:
			t.ReferencedBy = append(t.ReferencedBy, *entry.fk)
		}
	}

	return nil
}
