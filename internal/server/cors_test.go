package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

const corsTestOrigin = "https://app.example.com"

func newCorsHandler(t *testing.T, origins []string, next http.Handler) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(server.Cors(origins, next))
	t.Cleanup(ts.Close)

	return ts
}

// doCorsRequest issues the request and returns the response. The caller MUST
// `defer resp.Body.Close()` — bodyclose lints individual callers, so the
// helper cannot own the close.
func doCorsRequest(t *testing.T, ts *httptest.Server, method, path string, headers map[string]string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	return resp
}

func TestCors_EmptyOriginsIsNoOp(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, nil, ok)

	resp := doCorsRequest(t, ts, http.MethodGet, "/", map[string]string{"Origin": corsTestOrigin})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty (middleware should be a no-op)", got)
	}

	if got := resp.Header.Get("Vary"); strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, should not contain Origin when middleware is no-op", got)
	}
}

func TestCors_RequestWithoutOriginPassesThrough(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, []string{corsTestOrigin}, ok)

	resp := doCorsRequest(t, ts, http.MethodGet, "/", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty when request has no Origin", got)
	}

	if got := resp.Header.Get("Vary"); got != "" {
		t.Errorf("Vary = %q, want empty when request has no Origin", got)
	}
}

func TestCors_AllowedOriginEchoesACAO(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, []string{corsTestOrigin}, ok)

	resp := doCorsRequest(t, ts, http.MethodGet, "/", map[string]string{"Origin": corsTestOrigin})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != corsTestOrigin {
		t.Errorf("ACAO = %q, want %q", got, corsTestOrigin)
	}

	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "Content-Range" {
		t.Errorf("Expose-Headers = %q, want %q", got, "Content-Range")
	}

	if got := resp.Header.Values("Vary"); len(got) != 1 || got[0] != "Origin" {
		t.Errorf("Vary = %q, want [\"Origin\"]", got)
	}
}

func TestCors_DisallowedOriginGetsVaryButNoACAO(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, []string{corsTestOrigin}, ok)

	resp := doCorsRequest(t, ts, http.MethodGet, "/", map[string]string{"Origin": "https://evil.example.com"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (server still responds; browser blocks)", resp.StatusCode)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty for disallowed origin", got)
	}

	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "" {
		t.Errorf("Expose-Headers = %q, want empty for disallowed origin", got)
	}

	if got := resp.Header.Values("Vary"); len(got) != 1 || got[0] != "Origin" {
		t.Errorf("Vary = %q, want [\"Origin\"] (set even for disallowed origin, for cache correctness)", got)
	}
}

func TestCors_PreflightShortCircuits(t *testing.T) {
	t.Parallel()

	var nextCalled atomic.Bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled.Store(true)
	})

	ts := newCorsHandler(t, []string{corsTestOrigin}, next)

	resp := doCorsRequest(t, ts, http.MethodOptions, "/users", map[string]string{
		"Origin":                        corsTestOrigin,
		"Access-Control-Request-Method": "POST",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	if nextCalled.Load() {
		t.Error("next handler should not be invoked on preflight short-circuit")
	}

	headers := map[string]string{
		"Access-Control-Allow-Origin":   corsTestOrigin,
		"Access-Control-Allow-Methods":  "GET, POST, PATCH, DELETE, OPTIONS",
		"Access-Control-Allow-Headers":  "Authorization, Content-Type, Prefer",
		"Access-Control-Expose-Headers": "Content-Range",
		"Access-Control-Max-Age":        "86400",
	}

	for name, want := range headers {
		if got := resp.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if got := resp.Header.Values("Vary"); len(got) != 1 || got[0] != "Origin" {
		t.Errorf("Vary = %q, want [\"Origin\"] on preflight 204", got)
	}
}

func TestCors_OptionsWithoutPreflightFallsThrough(t *testing.T) {
	t.Parallel()

	var nextCalled atomic.Bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled.Store(true)

		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, []string{corsTestOrigin}, next)

	resp := doCorsRequest(t, ts, http.MethodOptions, "/", map[string]string{"Origin": corsTestOrigin})
	defer func() { _ = resp.Body.Close() }()

	if !nextCalled.Load() {
		t.Error("plain OPTIONS without Access-Control-Request-Method should pass to next handler")
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (next handler)", resp.StatusCode)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != corsTestOrigin {
		t.Errorf("ACAO = %q, want %q (still set even for non-preflight OPTIONS)", got, corsTestOrigin)
	}

	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "Content-Range" {
		t.Errorf("Expose-Headers = %q, want %q", got, "Content-Range")
	}
}

func TestCors_MultipleAllowedOrigins(t *testing.T) {
	t.Parallel()

	origins := []string{
		"https://app.example.com",
		"https://admin.example.com",
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := newCorsHandler(t, origins, ok)

	for _, o := range origins {
		t.Run(o, func(t *testing.T) {
			t.Parallel()

			resp := doCorsRequest(t, ts, http.MethodGet, "/", map[string]string{"Origin": o})
			defer func() { _ = resp.Body.Close() }()

			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != o {
				t.Errorf("ACAO = %q, want %q", got, o)
			}
		})
	}
}

func TestServerAppliesCORSOrigins(t *testing.T) {
	// End-to-end: build a Server with CORS + auth and confirm preflight
	// OPTIONS bypasses bearer auth, while real GET still requires the token.
	t.Parallel()

	const token = "tk"

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
			AuthToken:    token,
			CORSOrigins:  []string{corsTestOrigin},
		},
		stubDB,
		stubIntrospector{schema: schema.Schema{}},
		nil,
	)
	if err != nil {
		t.Fatalf("server.NewWithIntrospector: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	t.Run("preflight bypasses auth", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, ts.URL+"/users", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		req.Header.Set("Origin", corsTestOrigin)
		req.Header.Set("Access-Control-Request-Method", "GET")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("preflight status = %d, want 204 (auth should not block)", resp.StatusCode)
		}
	})

	t.Run("real request still requires token", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		req.Header.Set("Origin", corsTestOrigin)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401 (no bearer token)", resp.StatusCode)
		}

		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != corsTestOrigin {
			t.Errorf("ACAO = %q, want %q on auth-rejected response", got, corsTestOrigin)
		}
	})
}
