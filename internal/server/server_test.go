package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func newTestServer(t *testing.T, sch schema.Schema) (*httptest.Server, *bytes.Buffer) {
	t.Helper()

	var logs bytes.Buffer

	srv := &server.Server{
		Schema: sch,
		Logger: &logs,
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts, &logs
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

	var logs bytes.Buffer

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

	var logs bytes.Buffer

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

func TestServerRunReturnsListenFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	srv := &server.Server{
		Addr:   "127.0.0.1:99999", // out-of-range port; net.Listen rejects it
		Logger: &logs,
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
	_ = ln.Close()

	var logs bytes.Buffer

	srv := &server.Server{
		Addr:   addr,
		Logger: &logs,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Run(ctx) }()

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err //nolint:wrapcheck // probe helper, caller treats any error as "not ready"
	}

	return http.DefaultClient.Do(req) //nolint:wrapcheck // same as above
}
