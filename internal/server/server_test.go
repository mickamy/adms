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
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func newTestServer(t *testing.T, sch schema.Schema) (*httptest.Server, *syncBuffer) {
	t.Helper()

	var logs syncBuffer

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		nil,
		stubIntrospector{schema: sch},
		&logs,
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts, &logs
}

type stubIntrospector struct {
	schema schema.Schema
	err    error
}

func (s stubIntrospector) Introspect(_ context.Context, _ *sql.DB, _ []string) (schema.Schema, error) {
	return s.schema, s.err
}

// syncBuffer wraps a bytes.Buffer with a mutex so concurrent writes from the
// server goroutine and reads from the test goroutine are race-safe under -race.
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

	ts, _ := newTestServer(t, schema.Schema{})

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

	ts, _ := newTestServer(t, sch)

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

	ts, _ := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/does-not-exist")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestLoggingMiddlewareEmitsLine(t *testing.T) {
	t.Parallel()

	ts, logs := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/healthz")
	_ = resp.Body.Close()

	out := logs.String()
	if !strings.Contains(out, "GET /healthz 200") {
		t.Errorf("log output = %q, want substring %q", out, "GET /healthz 200")
	}
}

func TestRecovererTurnsPanicInto500(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	ts := httptest.NewServer(server.Recoverer(&logs, panicking))
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

func TestStatusRecorderIgnoresDuplicateWriteHeader(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.WriteHeader(http.StatusTeapot) // ignored; first one wins.
	})

	ts := httptest.NewServer(server.Logging(&logs, handler))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d (first WriteHeader wins)", resp.StatusCode, http.StatusNotFound)
	}

	if !strings.Contains(logs.String(), " 404 ") {
		t.Errorf("log = %q, want substring %q", logs.String(), " 404 ")
	}
}

func TestServerWithNilLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		nil,
		stubIntrospector{},
		nil, // Logger left nil
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/healthz")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStatusRecorderUnwrapAllowsFlush(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = io.WriteString(w, "flushed")
	})

	ts := httptest.NewServer(server.Logging(&logs, handler))
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

func TestPanicProducesLoggedFiveHundred(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	// Mirror the router's wrapping: logging(recoverer(handler)).
	ts := httptest.NewServer(server.Logging(&logs, server.Recoverer(&logs, panicking)))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	out := logs.String()
	if !strings.Contains(out, "panic") {
		t.Errorf("log = %q, want substring %q", out, "panic")
	}

	if !strings.Contains(out, " 500 ") {
		t.Errorf("log = %q, want access-log line with 500", out)
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
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("New() error = nil, want non-nil for unknown driver")
	}

	if !strings.Contains(err.Error(), "unknown driver") {
		t.Errorf("New() error = %q, want substring %q", err, "unknown driver")
	}
}

func TestNewRequiresPositiveTimeout(t *testing.T) {
	t.Parallel()

	_, err := server.NewWithIntrospector(
		config.Config{},
		nil,
		stubIntrospector{},
		nil,
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

	var logs syncBuffer

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Listen:       "127.0.0.1:99999", // out-of-range port; net.Listen rejects it
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		nil,
		stubIntrospector{},
		&logs,
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run() error = nil, want non-nil for invalid addr")
	}
}

func TestServerRunGracefulShutdown(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	var logs syncBuffer

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
		},
		nil,
		stubIntrospector{},
		&logs,
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
