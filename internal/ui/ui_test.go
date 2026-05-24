package ui_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/ui"
)

const apiOrigin = "http://localhost:7777"

func sampleSchema() schema.Schema {
	return schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "users",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "name", Type: "text"},
				},
				PrimaryKey: []string{"id"},
			},
			{
				Schema: "public",
				Name:   "posts",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "title", Type: "text"},
				},
				PrimaryKey: []string{"id"},
			},
		},
	}
}

func newTestUIServer(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	srv, err := ui.New(config.Config{UI: config.UIConfig{Listen: ":0"}}, sch, apiOrigin)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts
}

func TestIndexRendersSidebar(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`<meta name="adms-api-origin" content="http://localhost:7777">`,
		`href="/t/users"`,
		`href="/t/posts"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestTableViewRenders(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`data-col="id"`,
		`data-col="name"`,
		"PK: id",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestTableViewUnknownReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/ghost")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/__healthz")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := readAll(t, resp); got != "ok\n" {
		t.Errorf("body = %q, want %q", got, "ok\n")
	}
}

func TestNewRequiresAPIOrigin(t *testing.T) {
	t.Parallel()

	_, err := ui.New(config.Config{UI: config.UIConfig{Listen: ":0"}}, sampleSchema(), "")
	if err == nil {
		t.Fatal("ui.New error = nil, want non-nil for empty apiOrigin")
	}
}

func TestRunBindsAndShutsDown(t *testing.T) {
	t.Parallel()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: "127.0.0.1:0"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Run(ctx) }()

	// Give Run a moment to bind before we cancel; without this the listener
	// may not have entered Serve yet and we would not be testing the
	// graceful-shutdown path.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}
}

func TestRunListenFailure(t *testing.T) {
	t.Parallel()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: "127.0.0.1:99999"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run() error = nil, want non-nil for invalid addr")
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: ":0"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	waitForUI(t, addr, 2*time.Second)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return after context cancel")
	}
}

func waitForUI(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		probeCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)

		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+addr+"/__healthz", nil)
		resp, err := http.DefaultClient.Do(req)

		cancel()

		if err == nil {
			_ = resp.Body.Close()

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("UI server did not start within %s: last err=%v", timeout, err)
		}

		time.Sleep(20 * time.Millisecond)
	}
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

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return string(b)
}
