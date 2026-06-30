package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/logger"
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
		writeProblem(w, r, http.StatusNotFound,
			"unknown-table", "Unknown table",
			fmt.Sprintf("table %q is not exposed by this server", tableName))

		return
	}

	q, err := query.Parse(r.URL.Query())
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return
	}

	stmt, args, embedAliases, err := build.Select(q, table, s.tableIndex, s.dialect, s.defaultLimit, s.maxLimit)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return
	}

	queryCtx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	// stmt is constructed by build.Select with identifiers validated against the
	// introspected schema; values are passed as placeholder args, not interpolated.
	rows, err := s.db.QueryContext(queryCtx, stmt, args...) //nolint:gosec // see comment above
	if err != nil {
		logger.Error(r.Context(), "query",
			"method", r.Method, "path", r.URL.EscapedPath(), "err", err.Error())
		writeProblem(w, r, http.StatusInternalServerError,
			"db-error", "Database error", "the database refused or failed to execute the query")

		return
	}
	defer func() { _ = rows.Close() }()

	cols, result, err := scanRows(rows, embedAliases)
	if err != nil {
		logger.Error(r.Context(), "encode rows",
			"method", r.Method, "path", r.URL.EscapedPath(), "err", err.Error())
		writeProblem(w, r, http.StatusInternalServerError,
			"db-error", "Database error", "failed to read rows from the database")

		return
	}

	if wantsCSV(r.Header.Get("Accept")) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", tableName+".csv"))

		if err := writeCSV(w, cols, result); err != nil {
			logger.Error(r.Context(), "encode csv",
				"method", r.Method, "path", r.URL.EscapedPath(), "err", err.Error())
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		logger.Error(r.Context(), "encode response",
			"method", r.Method, "path", r.URL.EscapedPath(), "err", err.Error())
	}
}

// scanRows materializes the result set into ordered column names plus a
// slice of row maps. The column order is the SQL projection order, which
// the CSV encoder relies on for a stable header row.
func scanRows(rows *sql.Rows, embedAliases []string) ([]string, []map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("columns: %w", err)
	}

	embedSet := make(map[string]struct{}, len(embedAliases))
	for _, a := range embedAliases {
		embedSet[a] = struct{}{}
	}

	// Reuse scan buffers across rows: rows.Scan overwrites values[i] in place.
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))

	for i := range values {
		ptrs[i] = &values[i]
	}

	result := make([]map[string]any, 0)

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, fmt.Errorf("scan: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			if _, isEmbed := embedSet[col]; isEmbed {
				row[col] = decodeEmbedValue(values[i])
				continue
			}

			row[col] = normalizeScanValue(values[i])
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate: %w", err)
	}

	return cols, result, nil
}

// decodeEmbedValue wraps the embed payload in a json.RawMessage so the
// row's JSON encoder emits the nested object/array verbatim. Decoding into
// `any` would coerce BIGINTs to float64 and silently lose precision.
func decodeEmbedValue(v any) any {
	if v == nil {
		return nil
	}

	var src []byte
	switch x := v.(type) {
	case []byte:
		src = x
	case string:
		src = []byte(x)
	default:
		return v
	}

	if len(src) == 0 {
		return nil
	}

	// database/sql reuses scan buffers across iterations, so copy before
	// retaining.
	raw := make([]byte, len(src))
	copy(raw, src)

	return json.RawMessage(raw)
}

// normalizeScanValue turns driver []byte (e.g., MySQL text columns) into a
// JSON-friendly value: UTF-8 valid → string (a fresh allocation), otherwise
// a copied []byte so encoding/json emits a safe base64 form and the scan
// buffer reuse does not corrupt it.
func normalizeScanValue(v any) any {
	if b, ok := v.([]byte); ok {
		if utf8.Valid(b) {
			return string(b)
		}

		return bytes.Clone(b)
	}

	return v
}
