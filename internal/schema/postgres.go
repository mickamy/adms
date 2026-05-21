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

	for i := range tables {
		t := &tables[i]

		t.Columns, err = pgListColumns(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list columns for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.PrimaryKey, err = pgListPrimaryKey(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list pk for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.ForeignKeys, err = pgListForeignKeys(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list fks for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.ReferencedBy, err = pgListReferencedBy(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list referenced_by for %s.%s: %w", t.Schema, t.Name, err)
		}
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
		var t Table
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

func pgListColumns(ctx context.Context, db *sql.DB, schema, name string) ([]Column, error) {
	const query = `
		SELECT
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
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum
	`

	rows, err := db.QueryContext(ctx, query, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cols []Column

	for rows.Next() {
		var (
			c   Column
			def sql.NullString
		)

		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &def, &c.IsGenerated, &c.IsIdentity, &c.Comment); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		if def.Valid {
			c.Default = &def.String
		}

		cols = append(cols, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return cols, nil
}

func pgListPrimaryKey(ctx context.Context, db *sql.DB, schema, name string) ([]string, error) {
	const query = `
		SELECT a.attname
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
		WHERE i.indisprimary AND n.nspname = $1 AND c.relname = $2
		ORDER BY array_position(i.indkey, a.attnum)
	`

	rows, err := db.QueryContext(ctx, query, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pk []string

	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		pk = append(pk, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return pk, nil
}

func pgListForeignKeys(ctx context.Context, db *sql.DB, schema, name string) ([]ForeignKey, error) {
	const query = `
		SELECT
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
		WHERE con.contype = 'f' AND n.nspname = $1 AND c.relname = $2
		ORDER BY con.conname, ord
	`

	return pgScanFKs(ctx, db, query, schema, name)
}

func pgListReferencedBy(ctx context.Context, db *sql.DB, schema, name string) ([]ForeignKey, error) {
	const query = `
		SELECT
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
		WHERE con.contype = 'f' AND rn.nspname = $1 AND rc.relname = $2
		ORDER BY con.conname, ord
	`

	return pgScanFKs(ctx, db, query, schema, name)
}

func pgScanFKs(ctx context.Context, db *sql.DB, query, schema, name string) ([]ForeignKey, error) {
	rows, err := db.QueryContext(ctx, query, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		order  []string
		byName = make(map[string]*ForeignKey)
	)

	for rows.Next() {
		var (
			cname, linkedSchema, linkedName, col, refCol string
			ord                                          int
		)

		if err := rows.Scan(&cname, &linkedSchema, &linkedName, &col, &refCol, &ord); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		key := linkedSchema + "\x00" + cname

		fk, ok := byName[key]
		if !ok {
			fk = &ForeignKey{Table: pgQualify(linkedSchema, linkedName)}
			byName[key] = fk
			order = append(order, key)
		}

		fk.Columns = append(fk.Columns, col)
		fk.References = append(fk.References, refCol)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	out := make([]ForeignKey, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}

	return out, nil
}

func pgQualify(schema, name string) string {
	if schema == "" || schema == "public" {
		return name
	}

	return schema + "." + name
}
