package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type mysqlIntrospector struct{}

func MySQLIntrospector() Introspector { return mysqlIntrospector{} }

func (mysqlIntrospector) Introspect(ctx context.Context, db *sql.DB, allowedSchemas []string) (Schema, error) {
	if len(allowedSchemas) == 0 {
		current, err := mysqlCurrentSchema(ctx, db)
		if err != nil {
			return Schema{}, fmt.Errorf("resolve current schema: %w", err)
		}

		allowedSchemas = []string{current}
	}

	tables, err := mysqlListTables(ctx, db, allowedSchemas)
	if err != nil {
		return Schema{}, fmt.Errorf("list tables: %w", err)
	}

	for i := range tables {
		t := &tables[i]

		t.Columns, err = mysqlListColumns(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list columns for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.PrimaryKey, err = mysqlListPrimaryKey(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list pk for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.ForeignKeys, err = mysqlListForeignKeys(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list fks for %s.%s: %w", t.Schema, t.Name, err)
		}

		t.ReferencedBy, err = mysqlListReferencedBy(ctx, db, t.Schema, t.Name)
		if err != nil {
			return Schema{}, fmt.Errorf("list referenced_by for %s.%s: %w", t.Schema, t.Name, err)
		}
	}

	return Schema{Tables: tables}, nil
}

func mysqlCurrentSchema(ctx context.Context, db *sql.DB) (string, error) {
	var name sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return "", fmt.Errorf("scan current schema: %w", err)
	}

	if !name.Valid {
		return "", errors.New("no default database selected; specify --allowed-schemas or include a database in the DSN")
	}

	return name.String, nil
}

func mysqlListTables(ctx context.Context, db *sql.DB, schemas []string) ([]Table, error) {
	if len(schemas) == 0 {
		return nil, nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	//nolint:gosec // placeholders is a fixed list of "?" derived from len(schemas), not user input
	query := `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_type = 'BASE TABLE'
		  AND table_schema IN (` + placeholders + `)
		ORDER BY table_schema, table_name
	`

	rows, err := db.QueryContext(ctx, query, args...)
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

func mysqlListColumns(ctx context.Context, db *sql.DB, schema, name string) ([]Column, error) {
	const query = `
		SELECT
			column_name,
			column_type,
			is_nullable,
			column_default,
			extra,
			column_comment
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`

	rows, err := db.QueryContext(ctx, query, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cols []Column

	for rows.Next() {
		var (
			c        Column
			nullable string
			def      sql.NullString
			extra    string
		)

		if err := rows.Scan(&c.Name, &c.Type, &nullable, &def, &extra, &c.Comment); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		c.Nullable = strings.EqualFold(nullable, "YES")
		if def.Valid {
			c.Default = &def.String
		}

		c.IsGenerated = strings.Contains(strings.ToUpper(extra), "GENERATED")
		c.IsIdentity = strings.Contains(strings.ToLower(extra), "auto_increment")

		cols = append(cols, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return cols, nil
}

func mysqlListPrimaryKey(ctx context.Context, db *sql.DB, schema, name string) ([]string, error) {
	const query = `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = ? AND table_name = ? AND constraint_name = 'PRIMARY'
		ORDER BY ordinal_position
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

func mysqlListForeignKeys(ctx context.Context, db *sql.DB, schema, name string) ([]ForeignKey, error) {
	const query = `
		SELECT
			constraint_name,
			referenced_table_schema,
			referenced_table_name,
			column_name,
			referenced_column_name,
			ordinal_position
		FROM information_schema.key_column_usage
		WHERE table_schema = ? AND table_name = ? AND referenced_table_name IS NOT NULL
		ORDER BY constraint_name, ordinal_position
	`

	return mysqlScanFKs(ctx, db, query, schema, name)
}

func mysqlListReferencedBy(ctx context.Context, db *sql.DB, schema, name string) ([]ForeignKey, error) {
	const query = `
		SELECT
			constraint_name,
			table_schema,
			table_name,
			column_name,
			referenced_column_name,
			ordinal_position
		FROM information_schema.key_column_usage
		WHERE referenced_table_schema = ? AND referenced_table_name = ?
		ORDER BY constraint_name, ordinal_position
	`

	return mysqlScanFKs(ctx, db, query, schema, name)
}

func mysqlScanFKs(ctx context.Context, db *sql.DB, query, schema, name string) ([]ForeignKey, error) {
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
			fk = &ForeignKey{Table: mysqlQualify(linkedSchema, linkedName)}
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
	for _, k := range order {
		out = append(out, *byName[k])
	}

	return out, nil
}

func mysqlInPlaceholders(values []string) (string, []any) {
	if len(values) == 0 {
		return "", nil
	}

	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}

	return strings.Repeat("?,", len(values)-1) + "?", args
}

func mysqlQualify(schema, name string) string {
	if schema == "" {
		return name
	}

	return schema + "." + name
}
