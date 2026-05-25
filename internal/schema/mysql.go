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

	if err := mysqlAttachIndexes(ctx, db, allowedSchemas, index); err != nil {
		return Schema{}, fmt.Errorf("attach indexes: %w", err)
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

	return attachFKs(rows, index, direction)
}

// mysqlAttachIndexes loads index columns from information_schema.statistics
// and stream-aggregates them via the shared attachIndexes helper. Rows
// with NULL column_name (functional / expression indexes in MySQL 8) are
// filtered out at SQL level for the same reason pgAttachIndexes drops
// expression entries. The boolean polarity is rendered through CASE so
// the driver does not have to guess the type of a comparison expression,
// and INDEX_TYPE is lower-cased to match the values pg_am.amname uses.
// MySQL has no first-class partial indexes, so where_expr is always ”.
func mysqlAttachIndexes(ctx context.Context, db *sql.DB, schemas []string, index map[tableKey]*Table) error {
	if len(schemas) == 0 {
		return nil
	}

	placeholders, args := mysqlInPlaceholders(schemas)
	//nolint:gosec // placeholders is a fixed list of "?" derived from len(schemas), not user input
	query := `
		SELECT
			table_schema,
			table_name,
			index_name,
			CASE WHEN non_unique = 0 THEN TRUE ELSE FALSE END AS is_unique,
			column_name,
			LOWER(index_type) AS method,
			'' AS where_expr
		FROM information_schema.statistics
		WHERE table_schema IN (` + placeholders + `)
		  AND column_name IS NOT NULL
		ORDER BY table_schema, table_name, index_name, seq_in_index
	`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return attachIndexes(rows, index)
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
