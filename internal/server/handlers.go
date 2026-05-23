package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/query"
)

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) schemaDump(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.schemaJSON)
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, ok := s.tableIndex[tableName]
	if !ok {
		writeProblem(w, r, s.logger, http.StatusNotFound,
			"unknown-table", "Unknown table",
			fmt.Sprintf("table %q is not exposed by this server", tableName))

		return
	}

	q, err := query.Parse(r.URL.Query())
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return
	}

	stmt, args, err := build.Select(q, table, s.dialect, s.defaultLimit, s.maxLimit)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return
	}

	// stmt is constructed by build.Select with identifiers validated against the
	// introspected schema; values are passed as placeholder args, not interpolated.
	rows, err := s.db.QueryContext(r.Context(), stmt, args...) //nolint:gosec // see comment above
	if err != nil {
		fmt.Fprintf(s.logger, "adms: query %s %s: %v\n", r.Method, r.URL.EscapedPath(), err)
		writeProblem(w, r, s.logger, http.StatusInternalServerError,
			"db-error", "Database error", "the database refused or failed to execute the query")

		return
	}
	defer func() { _ = rows.Close() }()

	result, err := rowsToJSON(rows)
	if err != nil {
		fmt.Fprintf(s.logger, "adms: encode rows %s %s: %v\n", r.Method, r.URL.EscapedPath(), err)
		writeProblem(w, r, s.logger, http.StatusInternalServerError,
			"db-error", "Database error", "failed to read rows from the database")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		fmt.Fprintf(s.logger, "adms: encode rows for %s %s: %v\n",
			r.Method, r.URL.EscapedPath(), err)
	}
}

func rowsToJSON(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}

	// Allocate scan buffers once. rows.Scan overwrites values[i] each iteration
	// and ptrs[i] keeps pointing at the same slot, so there is nothing to reset
	// between rows.
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))

	for i := range values {
		ptrs[i] = &values[i]
	}

	result := make([]map[string]any, 0)

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = normalizeScanValue(values[i])
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}

	return result, nil
}

// normalizeScanValue makes driver-returned values JSON-friendly. The MySQL
// driver returns text-typed columns as []byte; we convert valid UTF-8 byte
// slices to string so encoding/json emits a normal JSON string. Non-UTF-8
// payloads (binary / blob columns) are passed through as []byte so JSON
// encoding produces a safe base64 representation rather than corrupting the
// bytes by force-casting to string.
func normalizeScanValue(v any) any {
	if b, ok := v.([]byte); ok {
		if utf8.Valid(b) {
			return string(b)
		}

		return b
	}

	return v
}
