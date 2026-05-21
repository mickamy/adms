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
