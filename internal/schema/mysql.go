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

	index := make(map[tableKey]*Table, len(tables))
	for i := range tables {
		index[tableKey{tables[i].Schema, tables[i].Name}] = &tables[i]
	}

	if err := mysqlAttachColumns(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach columns: %w", err)
	}

	if err := mysqlAttachPrimaryKeys(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach primary keys: %w", err)
	}

	if err := mysqlAttachForeignKeys(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach foreign keys: %w", err)
	}

	if err := mysqlAttachReferencedBy(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach referenced_by: %w", err)
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

func mysqlAttachColumns(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	if len(schemas) == 0 {
		return nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	//nolint:gosec // placeholders is a fixed list of "?" derived from len(schemas), not user input
	query := `
		SELECT
			table_schema,
			table_name,
			column_name,
			column_type,
			is_nullable,
			column_default,
			extra,
			column_comment
		FROM information_schema.columns
		WHERE table_schema IN (` + placeholders + `)
		ORDER BY table_schema, table_name, ordinal_position
	`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			schemaName, tableName string
			c                     Column
			nullable, extra       string
			def                   sql.NullString
		)

		if err := rows.Scan(&schemaName, &tableName, &c.Name, &c.Type, &nullable, &def, &extra, &c.Comment); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		c.Nullable = strings.EqualFold(nullable, "YES")
		if def.Valid {
			c.Default = &def.String
		}

		upper := strings.ToUpper(extra)
		c.IsGenerated = strings.Contains(upper, "VIRTUAL GENERATED") || strings.Contains(upper, "STORED GENERATED")
		c.IsIdentity = strings.Contains(upper, "AUTO_INCREMENT")

		if t, ok := index[tableKey{schemaName, tableName}]; ok {
			t.Columns = append(t.Columns, c)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	return nil
}

func mysqlAttachPrimaryKeys(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	if len(schemas) == 0 {
		return nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	//nolint:gosec // placeholders is a fixed list of "?" derived from len(schemas), not user input
	query := `
		SELECT table_schema, table_name, column_name
		FROM information_schema.key_column_usage
		WHERE constraint_name = 'PRIMARY'
		  AND table_schema IN (` + placeholders + `)
		ORDER BY table_schema, table_name, ordinal_position
	`

	rows, err := db.QueryContext(ctx, query, args...)
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

func mysqlAttachForeignKeys(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	if len(schemas) == 0 {
		return nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	query := `
		SELECT
			table_schema,
			table_name,
			constraint_name,
			referenced_table_schema,
			referenced_table_name,
			column_name,
			referenced_column_name,
			ordinal_position
		FROM information_schema.key_column_usage
		WHERE referenced_table_name IS NOT NULL
		  AND table_schema IN (` + placeholders + `)
		ORDER BY table_schema, table_name, constraint_name, ordinal_position
	`

	return mysqlAttachFKs(ctx, db, query, args, index, fkDirectionForward)
}

func mysqlAttachReferencedBy(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	if len(schemas) == 0 {
		return nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	query := `
		SELECT
			referenced_table_schema,
			referenced_table_name,
			constraint_name,
			table_schema,
			table_name,
			column_name,
			referenced_column_name,
			ordinal_position
		FROM information_schema.key_column_usage
		WHERE referenced_table_name IS NOT NULL
		  AND referenced_table_schema IN (` + placeholders + `)
		ORDER BY referenced_table_schema, referenced_table_name, constraint_name, ordinal_position
	`

	return mysqlAttachFKs(ctx, db, query, args, index, fkDirectionReverse)
}

func mysqlAttachFKs(ctx context.Context, db *sql.DB, query string, args []any,
	index map[tableKey]*Table, direction fkDirection,
) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return attachFKs(rows, mysqlQualify, index, direction)
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
