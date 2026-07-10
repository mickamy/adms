package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/logger"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func newTestServer(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
		stubIntrospector{schema: sch},
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts
}

// captureLogs swaps the slog default for a fresh syncBuffer; callers must
// not run in parallel since the default is process-wide.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()

	var buf syncBuffer

	logger.Capture(t, &buf)

	return &buf
}

func findLogRecord(t *testing.T, raw, msg string) map[string]any {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}

		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		if rec["msg"] == msg {
			return rec
		}
	}

	t.Fatalf("no log record with msg=%q in:\n%s", msg, raw)

	return nil
}

// stubDB satisfies the non-nil DB precondition for tests that fail before
// any DB call.
var stubDB = &sql.DB{}

type stubIntrospector struct {
	schema schema.Schema
	err    error
}

func (s stubIntrospector) Introspect(_ context.Context, _ *sql.DB, _ []string) (schema.Schema, error) {
	return s.schema, s.err
}

// syncBuffer is a race-safe bytes.Buffer for cross-goroutine log capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p) //nolint:wrapcheck // bytes.Buffer.Write never returns a non-nil error.
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return resp
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/healthz")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain prefix", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if got := string(body); got != "ok\n" {
		t.Errorf("body = %q, want %q", got, "ok\n")
	}
}

func TestSchemaDump(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema:     "public",
				Name:       "users",
				PrimaryKey: []string{"id"},
				Columns: []schema.Column{
					{Name: "id", Type: "bigint", Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
		},
	}

	ts := newTestServer(t, sch)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got schema.Schema
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(got.Tables) != 1 || got.Tables[0].Name != "users" {
		t.Errorf("decoded schema = %+v, want one table named 'users'", got)
	}
}

func TestRootOnlyMatchesExactPath(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/does-not-exist")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

//nolint:paralleltest // captureLogs mutates the process-wide slog default.
func TestLoggingMiddlewareEmitsLine(t *testing.T) {
	logs := captureLogs(t)
	ts := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/healthz")
	_ = resp.Body.Close()

	rec := findLogRecord(t, logs.String(), "request")
	if rec["method"] != "GET" {
		t.Errorf("method = %v, want GET", rec["method"])
	}

	if rec["path"] != "/healthz" {
		t.Errorf("path = %v, want /healthz", rec["path"])
	}

	if rec["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want %d", rec["status"], http.StatusOK)
	}
}

//nolint:paralleltest // captureLogs mutates the process-wide slog default.
func TestRecovererTurnsPanicInto500(t *testing.T) {
	logs := captureLogs(t)

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	ts := httptest.NewServer(server.Recoverer(panicking))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	if !strings.Contains(logs.String(), "panic") {
		t.Errorf("log output = %q, want substring %q", logs.String(), "panic")
	}
}

//nolint:paralleltest // captureLogs mutates the process-wide slog default.
func TestStatusRecorderIgnoresDuplicateWriteHeader(t *testing.T) {
	logs := captureLogs(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.WriteHeader(http.StatusTeapot) // ignored; first one wins.
	})

	ts := httptest.NewServer(server.Logging(handler))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d (first WriteHeader wins)", resp.StatusCode, http.StatusNotFound)
	}

	rec := findLogRecord(t, logs.String(), "request")
	if rec["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want %d (first WriteHeader wins in access log)",
			rec["status"], http.StatusNotFound)
	}
}

func TestStatusRecorderUnwrapAllowsFlush(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = io.WriteString(w, "flushed")
	})

	ts := httptest.NewServer(server.Logging(handler))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (Flush should succeed via Unwrap)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(body) != "flushed" {
		t.Errorf("body = %q, want %q", string(body), "flushed")
	}
}

//nolint:paralleltest // captureLogs mutates the process-wide slog default.
func TestPanicProducesLoggedFiveHundred(t *testing.T) {
	logs := captureLogs(t)

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	// Mirror the router's wrapping: logging(recoverer(handler)).
	ts := httptest.NewServer(server.Logging(server.Recoverer(panicking)))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	out := logs.String()

	panicRec := findLogRecord(t, out, "panic")
	if panicRec["recover"] != "boom" {
		t.Errorf("recover = %v, want %q", panicRec["recover"], "boom")
	}

	reqRec := findLogRecord(t, out, "request")
	if reqRec["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("access log status = %v, want %d", reqRec["status"], http.StatusInternalServerError)
	}
}

func TestNewRejectsUnknownDriver(t *testing.T) {
	t.Parallel()

	_, err := server.New(
		config.Config{
			Timeout:      time.Second, // Driver left empty/unknown
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
	)
	if err == nil {
		t.Fatal("New() error = nil, want non-nil for unknown driver")
	}

	if !strings.Contains(err.Error(), "unknown driver") {
		t.Errorf("New() error = %q, want substring %q", err, "unknown driver")
	}
}

func TestNewRequiresDB(t *testing.T) {
	t.Parallel()

	_, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		nil,
		stubIntrospector{},
	)
	if err == nil {
		t.Fatal("NewWithIntrospector error = nil, want error for nil db")
	}

	if !strings.Contains(err.Error(), "db is required") {
		t.Errorf("error = %q, want substring %q", err.Error(), "db is required")
	}
}

func TestNewRejectsDefaultLimitExceedingMaxLimit(t *testing.T) {
	t.Parallel()

	_, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 500,
			MaxLimit:     100,
		},
		stubDB,
		stubIntrospector{},
	)
	if err == nil {
		t.Fatal("NewWithIntrospector error = nil, want error for default_limit > max_limit")
	}

	if !strings.Contains(err.Error(), "default_limit") {
		t.Errorf("error = %q, want substring %q", err.Error(), "default_limit")
	}
}

func TestNewRequiresPositiveTimeout(t *testing.T) {
	t.Parallel()

	_, err := server.NewWithIntrospector(
		config.Config{},
		stubDB,
		stubIntrospector{},
	)
	if err == nil {
		t.Fatal("NewWithIntrospector() error = nil, want non-nil when Timeout is zero")
	}

	if !strings.Contains(err.Error(), "timeout must be positive") {
		t.Errorf("NewWithIntrospector() error = %q, want substring %q", err, "timeout must be positive")
	}
}

func TestServerRunReturnsListenFailure(t *testing.T) {
	t.Parallel()

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Listen:       "127.0.0.1:99999", // out-of-range port; net.Listen rejects it
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
		stubIntrospector{},
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run() error = nil, want non-nil for invalid addr")
	}
}

//nolint:paralleltest // captureLogs mutates the process-wide slog default.
func TestServerRunGracefulShutdown(t *testing.T) {
	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	logs := captureLogs(t)

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
		stubIntrospector{},
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	waitForListener(t, addr, 2*time.Second)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}

	if !strings.Contains(logs.String(), "shutting down") {
		t.Errorf("log output = %q, want substring %q", logs.String(), "shutting down")
	}
}

func waitForListener(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		resp, err := httpProbe(t.Context(), "http://"+addr+"/healthz")
		if err == nil {
			_ = resp.Body.Close()

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("server did not start within %s: last err=%v", timeout, err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func httpProbe(ctx context.Context, url string) (*http.Response, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err //nolint:wrapcheck // probe helper, caller treats any error as "not ready"
	}

	return http.DefaultClient.Do(req) //nolint:wrapcheck // same as above
}

func TestFilterAllowedTables(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{Schema: "public", Name: "users"},
			{Schema: "public", Name: "posts"},
			{Schema: "internal", Name: "audit_log"},
		},
	}

	tests := []struct {
		name      string
		allowed   []string
		wantNames []string
	}{
		{"nil allow list keeps every table", nil, []string{"users", "posts", "audit_log"}},
		{"empty allow list keeps every table", []string{}, []string{"users", "posts", "audit_log"}},
		{"subset keeps only listed tables", []string{"users", "posts"}, []string{"users", "posts"}},
		{"single match keeps just one", []string{"users"}, []string{"users"}},
		{"no match yields empty schema", []string{"ghost"}, []string{}},
		{"all match keeps every table", []string{"users", "posts", "audit_log"}, []string{"users", "posts", "audit_log"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := server.FilterAllowedTables(sch, tt.allowed)

			gotNames := make([]string, 0, len(got.Tables))
			for _, table := range got.Tables {
				gotNames = append(gotNames, table.Name)
			}

			if !equalStringSlices(gotNames, tt.wantNames) {
				t.Errorf("names = %v, want %v", gotNames, tt.wantNames)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestServerAppliesAuthToken(t *testing.T) {
	t.Parallel()

	const token = "tk"

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
			Auth:         config.Auth{Mode: config.AuthModeStatic, Token: token},
		},
		stubDB,
		stubIntrospector{},
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	t.Run("schema dump requires auth", func(t *testing.T) {
		t.Parallel()

		resp := httpGet(t, ts.URL+"/")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("schema dump accepts valid token", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200 with valid token", resp.StatusCode)
		}
	})

	t.Run("healthz bypasses auth", func(t *testing.T) {
		t.Parallel()

		resp := httpGet(t, ts.URL+"/healthz")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200 (/healthz must bypass auth)", resp.StatusCode)
		}
	})
}

func TestServerSchemaAfterPrepare(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{Schema: "public", Name: "users", Columns: []schema.Column{{Name: "id"}}},
			{Schema: "public", Name: "posts", Columns: []schema.Column{{Name: "id"}}},
		},
	}

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
		stubIntrospector{schema: sch},
	)
	if err != nil {
		t.Fatalf("NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	got := srv.Schema()
	if len(got.Tables) != 2 || got.Tables[0].Name != "users" || got.Tables[1].Name != "posts" {
		t.Errorf("Schema() = %+v, want users + posts", got)
	}

	// Second Prepare is a no-op because of sync.Once; Schema stays valid.
	if err := srv.Prepare(t.Context()); err != nil {
		t.Errorf("Prepare second call: %v", err)
	}
}

func TestPrepareRejectsDuplicateTableNames(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{Schema: "public", Name: "users", Columns: []schema.Column{{Name: "id"}}},
			{Schema: "internal", Name: "users", Columns: []schema.Column{{Name: "id"}}},
		},
	}

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		stubDB,
		stubIntrospector{schema: sch},
	)
	if err != nil {
		t.Fatalf("NewWithIntrospector: %v", err)
	}

	err = srv.Prepare(t.Context())
	if err == nil {
		t.Fatal("Prepare error = nil, want non-nil for duplicate table names")
	}

	if !strings.Contains(err.Error(), "duplicate table") {
		t.Errorf("Prepare error = %q, want substring %q", err.Error(), "duplicate table")
	}
}
