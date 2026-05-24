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

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

// maxRequestBodyBytes caps how much of a write body we read into memory. 10
// MiB is enough for sizable bulk inserts but stops a hostile client from
// exhausting the server with a single PATCH/POST. Phase 6 can revisit this
// once we understand real-world body sizes.
const maxRequestBodyBytes = 10 << 20

type preferReturn int

const (
	preferReturnMinimal preferReturn = iota
	preferReturnRepresentation
)

type preferDirective struct {
	Return     preferReturn
	CountExact bool
}

// parsePrefer decodes the PostgREST-style Prefer header. Unknown tokens are
// ignored so the request still succeeds — the directive structure tracks
// only the subset adms acts on.
func parsePrefer(h http.Header) preferDirective {
	p := preferDirective{Return: preferReturnMinimal}

	for _, v := range h.Values("Prefer") {
		for item := range strings.SplitSeq(v, ",") {
			switch strings.TrimSpace(item) {
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

func (s *Server) insert(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
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

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "insert") {
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

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact, http.StatusCreated)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
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

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "update") {
		return
	}

	stmt, args, err := build.Update(table, set, q, s.dialect, wantRet)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-body", "Invalid body", err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact, http.StatusOK)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	table, ok := s.resolveWriteTarget(w, r)
	if !ok {
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

	pref := parsePrefer(r.Header)
	wantRet := pref.Return == preferReturnRepresentation

	if !s.requireReturningSupport(w, r, wantRet, "delete") {
		return
	}

	stmt, args, err := build.Delete(table, q, s.dialect, wantRet)
	if err != nil {
		writeProblem(w, r, s.logger, http.StatusBadRequest,
			"invalid-query", "Invalid query", err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	s.executeWrite(ctx, w, r, stmt, args, wantRet, pref.CountExact, http.StatusOK)
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeProblem(w, r, s.logger, http.StatusRequestEntityTooLarge,
				"body-too-large", "Body too large",
				fmt.Sprintf("request body exceeded %d bytes", maxRequestBodyBytes))

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
func parseInsertBody(body []byte) ([]map[string]any, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errors.New("request body is empty")
	}

	if trimmed[0] == '[' {
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
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
	if err := json.Unmarshal(body, &row); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}

	if row == nil {
		return nil, errors.New("body is null")
	}

	return []map[string]any{row}, nil
}

func parseUpdateBody(body []byte) (map[string]any, error) {
	var set map[string]any
	if err := json.Unmarshal(body, &set); err != nil {
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

// executeWrite runs the statement and writes the response. When wantRet is
// true the response body is the JSON array of rows; otherwise the response
// is a status-only ack (201 for POST, 204 for PATCH/DELETE) and the affected
// row count rides Content-Range when count=exact was requested.
func (s *Server) executeWrite(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	stmt string,
	args []any,
	wantRet bool,
	countExact bool,
	createdStatus int,
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
		w.WriteHeader(createdStatus)

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

	// PostgREST: POST → 201, PATCH/DELETE → 204 No Content for return=minimal.
	if createdStatus == http.StatusCreated {
		w.WriteHeader(http.StatusCreated)

		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	writeProblem(w, r, s.logger, http.StatusInternalServerError,
		"db-error", "Database error",
		"the database refused or failed to execute the statement")
}
