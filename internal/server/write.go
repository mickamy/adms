package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

type preferReturn int

const (
	preferReturnMinimal preferReturn = iota
	preferReturnRepresentation
)

type preferDirective struct {
	Return     preferReturn
	CountExact bool
}

// parsePrefer decodes the PostgREST-style Prefer header. Per RFC 7240 the
// preference tokens are case-insensitive, so each directive is lowercased
// before matching. Unknown tokens are ignored so the request still
// succeeds; the directive structure tracks only the subset adms acts on.
func parsePrefer(h http.Header) preferDirective {
	p := preferDirective{Return: preferReturnMinimal}

	for _, v := range h.Values("Prefer") {
		for item := range strings.SplitSeq(v, ",") {
			switch strings.ToLower(strings.TrimSpace(item)) {
			case "return=representation":
				p.Return = preferReturnRepresentation
			case "return=minimal":
				p.Return = preferReturnMinimal
			case "count=exact":
				p.CountExact = true
			}
		}
	}

	return p
}

// Handler ordering note: validate Prefer / filter / read_only gates BEFORE
// reading the request body. http.MaxBytesReader still allocates up to
// s.maxBodyBytes on a fast client, so rejecting on headers / query first
// keeps a flood of unsupported requests from forcing the server to pull
// the whole body for every request.

func (s *Server) insert(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
		return
	}

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "insert") {
		return
	}

	body, ok := s.readJSONBody(w, r)
	if !ok {
		return
	}

	rows, err := parseInsertBody(body)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", err.Error())

		return
	}

	stmt, args, err := build.Insert(table, rows, s.dialect, wantRet)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact,
		http.StatusCreated, http.StatusCreated)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
		return
	}

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "update") {
		return
	}

	q, ok := s.parseFilter(w, r)
	if !ok {
		return
	}

	if q.Filter == nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"unfiltered-write", "Unfiltered write",
			"PATCH requires at least one filter to avoid updating every row")

		return
	}

	body, ok := s.readJSONBody(w, r)
	if !ok {
		return
	}

	set, err := parseUpdateBody(body)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", err.Error())

		return
	}

	stmt, args, err := build.Update(table, set, q, s.dialect, wantRet)
	if err != nil {
		typeSuffix, title := "invalid-body", "Invalid body"
		if _, ok := errors.AsType[*build.FilterError](err); ok {
			typeSuffix, title = "invalid-query", "Invalid query"
		}

		writeProblem(w, r, s.logger, http.StatusBadRequest,
			typeSuffix, title, err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact,
		http.StatusOK, http.StatusNoContent)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
		return
	}

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "delete") {
		return
	}

	q, ok := s.parseFilter(w, r)
	if !ok {
		return
	}

	if q.Filter == nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"unfiltered-write", "Unfiltered write",
			"DELETE requires at least one filter to avoid removing every row")

		return
	}

	stmt, args, err := build.Delete(table, q, s.dialect, wantRet)
	if err != nil {
		typeSuffix, title := "invalid-body", "Invalid body"
		if _, ok := errors.AsType[*build.FilterError](err); ok {
			typeSuffix, title = "invalid-query", "Invalid query"
		}

		writeProblem(w, r, s.logger, http.StatusBadRequest,
			typeSuffix, title, err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact,
		http.StatusOK, http.StatusNoContent)
}

func (s *Server) resolveWriteTarget(w http.ResponseWriter, r *http.Request) (*schema.Table, bool) {
	if s.readOnly {
		writeProblem(w, r, s.logger, http.StatusForbidden,
			"read-only", "Read-only",
			"this server is running in read-only mode")

		return nil, false
	}

	name := r.PathValue("table")

	t, ok := s.tableIndex[name]
	if !ok {
		writeProblem(w, r, s.logger, http.StatusNotFound,
			"unknown-table", "Unknown table",
			fmt.Sprintf("table %q is not exposed by this server", name))

		return nil, false
	}

	return t, true
}

func (s *Server) parseFilter(w http.ResponseWriter, r *http.Request) (query.Query, bool) {
	q, err := query.Parse(r.URL.Query())
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return query.Query{}, false
	}

	return q, true
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBodyBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeProblem(w, r, s.logger, http.StatusRequestEntityTooLarge,
				"body-too-large", "Body too large",
				fmt.Sprintf("request body exceeded %d bytes", s.maxBodyBytes))

			return nil, false
		}

		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", "failed to read request body")

		return nil, false
	}

	if len(bytes.TrimSpace(body)) == 0 {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", "request body is empty")

		return nil, false
	}

	return body, true
}

// requireReturningSupport rejects representation requests on dialects that
// cannot emit RETURNING. The MySQL handler-side workaround (pre/post SELECT)
// is deferred to a later phase.
func (s *Server) requireReturningSupport(w http.ResponseWriter, r *http.Request, want bool, op string) bool {
	if !want || s.dialect.SupportsReturning() {
		return true
	}

	writeProblem(w, r, s.logger, http.StatusNotImplemented,
		"unsupported", "Unsupported feature",
		fmt.Sprintf("dialect %q does not support return=representation on %s yet",
			s.dialect.Name(), op))

	return false
}

// parseInsertBody accepts either a JSON object (single row) or a JSON array
// of objects (bulk). Empty arrays and null bodies are rejected so the SQL
// builder never sees a degenerate input.
//
// Numbers are decoded as json.Number (via UseNumber) rather than float64 so
// large integers and high-precision decimals round-trip to the driver
// without lossy float coercion; database/sql converts json.Number to a SQL
// string parameter and the DB casts it deterministically.
func parseInsertBody(body []byte) ([]map[string]any, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errors.New("request body is empty")
	}

	if trimmed[0] == '[' {
		var rows []map[string]any
		if err := decodeJSONWithNumber(body, &rows); err != nil {
			return nil, fmt.Errorf("invalid JSON array: %w", err)
		}

		if len(rows) == 0 {
			return nil, errors.New("body array is empty")
		}

		for i, r := range rows {
			if r == nil {
				return nil, fmt.Errorf("row %d is null", i)
			}
		}

		return rows, nil
	}

	var row map[string]any
	if err := decodeJSONWithNumber(body, &row); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}

	if row == nil {
		return nil, errors.New("body is null")
	}

	return []map[string]any{row}, nil
}

func parseUpdateBody(body []byte) (map[string]any, error) {
	var set map[string]any
	if err := decodeJSONWithNumber(body, &set); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}

	if set == nil {
		return nil, errors.New("PATCH body must be a JSON object")
	}

	if len(set) == 0 {
		return nil, errors.New("PATCH body has no columns to update")
	}

	return set, nil
}

// decodeJSONWithNumber decodes body into target with UseNumber() set so JSON
// numbers materialize as json.Number instead of float64 (see parseInsertBody
// for the precision rationale). After the first value is consumed, a probe
// Decode must return io.EOF; anything else (a second JSON value, trailing
// junk, etc.) is rejected so callers like `{"a":1} junk` or
// `{"a":1}{"b":2}` cannot slip past the handler. dec.More() is defined
// against the current array/object state, so it is unreliable for the
// top-level trailing-data check.
func decodeJSONWithNumber(body []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("decode: unexpected data after JSON value")
	}

	return nil
}

// executeWrite runs the statement and writes the response. The two status
// arguments separate the response codes for the two branches: representation
// (success body) and minimal (no body). PostgREST conventions:
//
//	POST   → representation 201, minimal 201
//	PATCH  → representation 200, minimal 204
//	DELETE → representation 200, minimal 204
//
// Splitting the two avoids overloading a single "createdStatus" value as
// both a success code and a marker for the POST → 201 minimal case.
func (s *Server) executeWrite(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	stmt string,
	args []any,
	wantRet bool,
	countExact bool,
	representationStatus int,
	minimalStatus int,
) {
	if wantRet {
		// stmt is constructed by build.* with identifiers validated against the
		// introspected schema; values are passed as placeholder args.
		rows, err := s.db.QueryContext(ctx, stmt, args...)
		if err != nil {
			s.writeDBError(w, r, err, "query")

			return
		}
		defer func() { _ = rows.Close() }()

		result, err := rowsToJSON(rows, nil)
		if err != nil {
			s.writeDBError(w, r, err, "encode rows")

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Range", contentRangeReturned(len(result)))
		w.WriteHeader(representationStatus)

		if err := json.NewEncoder(w).Encode(result); err != nil {
			fmt.Fprintf(s.logger, "adms: encode rows %s %s: %v\n",
				r.Method, r.URL.EscapedPath(), err)
		}

		return
	}

	res, err := s.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		s.writeDBError(w, r, err, "exec")

		return
	}

	if countExact {
		if n, err := res.RowsAffected(); err == nil {
			w.Header().Set("Content-Range", fmt.Sprintf("*/%d", n))
		}
	}

	w.WriteHeader(minimalStatus)
}

func contentRangeReturned(n int) string {
	if n == 0 {
		return fmt.Sprintf("*/%d", n)
	}

	return fmt.Sprintf("0-%d/%d", n-1, n)
}

func (s *Server) writeDBError(w http.ResponseWriter, r *http.Request, err error, action string) {
	fmt.Fprintf(s.logger, "adms: %s %s %s: %v\n",
		action, r.Method, r.URL.EscapedPath(), err)

	status, typeSuffix, title, detail := classifyDBError(err)
	writeProblem(w, r, s.logger, status, typeSuffix, title, detail)
}

// classifyDBError maps driver-typed errors to HTTP responses. Unique /
// foreign-key / not-null / check violations become 409 (the change
// conflicts with existing data); explicit data-type errors become 400.
// Anything we cannot classify falls back to 500 with a generic message so
// internal schema details don't leak through the response body.
func classifyDBError(err error) (status int, typeSuffix, title, detail string) {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch class := pgErr.Code[:2]; class {
		case "23": // integrity_constraint_violation
			return http.StatusConflict, "constraint-violation", "Constraint violation",
				"the request conflicts with a database constraint"
		case "22": // data_exception
			return http.StatusBadRequest, "invalid-data", "Invalid data",
				"a value in the request cannot be coerced to the column's type"
		}
	}

	if myErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		switch myErr.Number {
		case 1062, // ER_DUP_ENTRY
			1216, 1217, 1451, 1452: // foreign-key violations
			return http.StatusConflict, "constraint-violation", "Constraint violation",
				"the request conflicts with a database constraint"
		case 1048: // ER_BAD_NULL_ERROR
			return http.StatusBadRequest, "invalid-data", "Invalid data",
				"a NOT NULL column was set to null"
		}
	}

	return http.StatusInternalServerError, "db-error", "Database error",
		"the database refused or failed to execute the statement"
}
