package schema

import (
	"context"
	"database/sql"
	"fmt"
)

type postgresIntrospector struct{}

func PostgresIntrospector() Introspector { return postgresIntrospector{} }

func (postgresIntrospector) Introspect(ctx context.Context, db *sql.DB, allowedSchemas []string) (Schema, error) {
	if len(allowedSchemas) == 0 {
		allowedSchemas = []string{"public"}
	}

	tables, err := pgListTables(ctx, db, allowedSchemas)
	if err != nil {
		return Schema{}, fmt.Errorf("list tables: %w", err)
	}

	index := make(map[tableKey]*Table, len(tables))
	for i := range tables {
		index[tableKey{tables[i].Schema, tables[i].Name}] = &tables[i]
	}

	if err := pgAttachColumns(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach columns: %w", err)
	}

	if err := pgAttachPrimaryKeys(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach primary keys: %w", err)
	}

	if err := pgAttachForeignKeys(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach foreign keys: %w", err)
	}

	if err := pgAttachReferencedBy(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach referenced_by: %w", err)
	}

	if err := pgAttachIndexes(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach indexes: %w", err)
	}

	return Schema{Tables: tables}, nil
}

func pgListTables(ctx context.Context, db *sql.DB, schemas []string) ([]Table, error) {
	const query = `
		SELECT n.nspname AS schema, c.relname AS name
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname = ANY($1)
		ORDER BY n.nspname, c.relname
	`

	rows, err := db.QueryContext(ctx, query, schemas)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []Table

	for rows.Next() {
		t := Table{
			PrimaryKey:   []string{},
			Columns:      []Column{},
			ForeignKeys:  []ForeignKey{},
			ReferencedBy: []ForeignKey{},
			Indexes:      []Index{},
		}
		if err := rows.Scan(&t.Schema, &t.Name); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		tables = append(tables, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return tables, nil
}

func pgAttachColumns(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	const query = `
		SELECT
			n.nspname,
			c.relname,
			a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid),
			a.attgenerated <> '',
			a.attidentity <> '',
			COALESCE(col_description(a.attrelid, a.attnum), '')
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname = ANY($1)
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY n.nspname, c.relname, a.attnum
	`

	rows, err := db.QueryContext(ctx, query, schemas)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			schemaName, tableName string
			c                     Column
			def                   sql.NullString
		)

		if err := rows.Scan(&schemaName, &tableName, &c.Name, &c.Type, &c.Nullable, &def,
			&c.IsGenerated, &c.IsIdentity, &c.Comment); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		if def.Valid {
			c.Default = &def.String
		}

		if t, ok := index[tableKey{schemaName, tableName}]; ok {
			t.Columns = append(t.Columns, c)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	return nil
}

func pgAttachPrimaryKeys(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	const query = `
		SELECT
			n.nspname,
			c.relname,
			a.attname
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
		WHERE i.indisprimary
		  AND c.relkind IN ('r', 'p')
		  AND n.nspname = ANY($1)
		ORDER BY n.nspname, c.relname, array_position(i.indkey, a.attnum)
	`

	rows, err := db.QueryContext(ctx, query, schemas)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var schemaName, tableName, col string
		if err := rows.Scan(&schemaName, &tableName, &col); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		if t, ok := index[tableKey{schemaName, tableName}]; ok {
			t.PrimaryKey = append(t.PrimaryKey, col)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	return nil
}

func pgAttachForeignKeys(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	const query = `
		SELECT
			n.nspname,
			c.relname,
			con.conname,
			rn.nspname,
			rc.relname,
			a.attname,
			fa.attname,
			ord
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS rk(attnum, ord2) ON ord = ord2
		JOIN pg_catalog.pg_attribute fa ON fa.attrelid = rc.oid AND fa.attnum = rk.attnum
		WHERE con.contype = 'f'
		  AND n.nspname = ANY($1)
		ORDER BY n.nspname, c.relname, con.conname, ord
	`

	return pgAttachFKs(ctx, db, query, schemas, index, fkDirectionForward)
}

func pgAttachReferencedBy(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	const query = `
		SELECT
			rn.nspname,
			rc.relname,
			con.conname,
			n.nspname,
			c.relname,
			a.attname,
			fa.attname,
			ord
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS rk(attnum, ord2) ON ord = ord2
		JOIN pg_catalog.pg_attribute fa ON fa.attrelid = rc.oid AND fa.attnum = rk.attnum
		WHERE con.contype = 'f'
		  AND rn.nspname = ANY($1)
		ORDER BY rn.nspname, rc.relname, con.conname, ord
	`

	return pgAttachFKs(ctx, db, query, schemas, index, fkDirectionReverse)
}

func pgAttachFKs(ctx context.Context, db *sql.DB, query string, schemas []string,
	index map[tableKey]*Table, direction fkDirection,
) error {
	rows, err := db.QueryContext(ctx, query, schemas)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return attachFKs(rows, index, direction)
}

// pgAttachIndexes loads non-expression index columns from pg_index plus
// the access method (pg_am.amname) and partial-index predicate
// (pg_get_expr on indpred). Rows are ordered by (schema, table, index
// name, column position) so the loop can stream-aggregate columns into
// a single Index per name without a per-key map. Expression / functional
// index entries (attnum = 0) are skipped because pg_attribute does not
// have a row for them; multi-column indexes that mix regular and
// expression columns will surface only their regular columns, which is
// the same trade-off `\d` makes.
func pgAttachIndexes(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	const query = `
		SELECT
			tn.nspname AS table_schema,
			t.relname AS table_name,
			c.relname AS index_name,
			i.indisunique AS is_unique,
			a.attname AS column_name,
			am.amname AS method,
			COALESCE(pg_get_expr(i.indpred, i.indrelid), '') AS where_expr
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class c ON c.oid = i.indexrelid
		JOIN pg_catalog.pg_am am ON am.oid = c.relam
		JOIN pg_catalog.pg_class t ON t.oid = i.indrelid
		JOIN pg_catalog.pg_namespace tn ON tn.oid = t.relnamespace
		JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE tn.nspname = ANY($1)
		  AND t.relkind IN ('r', 'p')
		  AND k.attnum > 0
		ORDER BY tn.nspname, t.relname, c.relname, k.ord
	`

	rows, err := db.QueryContext(ctx, query, schemas)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return attachIndexes(rows, index)
}

// attachIndexes stream-aggregates the (schema, table, index_name)
// groups in the rowset into one Index each. Callers must order rows by
// the same key tuple so a name change reliably marks a boundary.
// Method and Where are constant per index — both dialects emit the same
// values on every row of the group, so the first row's values are kept
// and subsequent ones merely contribute additional columns.
func attachIndexes(rows *sql.Rows, index map[tableKey]*Table) error {
	var (
		current    *Index
		currentKey tableKey
	)

	flush := func() {
		if current == nil {
			return
		}

		if t, ok := index[currentKey]; ok {
			t.Indexes = append(t.Indexes, *current)
		}

		current = nil
	}

	for rows.Next() {
		var (
			schemaName, tableName, indexName, columnName, method, whereExpr string
			isUnique                                                        bool
		)

		if err := rows.Scan(
			&schemaName, &tableName, &indexName, &isUnique, &columnName, &method, &whereExpr,
		); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		key := tableKey{schemaName, tableName}
		if current == nil || currentKey != key || current.Name != indexName {
			flush()

			current = &Index{
				Name:    indexName,
				Columns: []string{},
				Unique:  isUnique,
				Method:  method,
				Where:   whereExpr,
			}
			currentKey = key
		}

		current.Columns = append(current.Columns, columnName)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	flush()

	return nil
}
