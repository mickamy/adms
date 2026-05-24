package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mickamy/adms/internal/server"
)

func TestAuthBearer_EmptyTokenIsNoOp(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	ts := httptest.NewServer(server.AuthBearer(io.Discard, "", ok))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (auth disabled when token is empty)", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", string(body), "ok")
	}
}

func TestAuthBearer_AcceptsValidToken(t *testing.T) {
	t.Parallel()

	const token = "s3cret"

	var called atomic.Bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)

		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(server.AuthBearer(io.Discard, token, next))
	t.Cleanup(ts.Close)

	req := newRequest(t, ts.URL+"/")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if !called.Load() {
		t.Error("next handler was not invoked despite valid token")
	}
}

func TestAuthBearer_LowercaseSchemeAccepted(t *testing.T) {
	t.Parallel()

	const token = "s3cret"

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	ts := httptest.NewServer(server.AuthBearer(io.Discard, token, ok))
	t.Cleanup(ts.Close)

	req := newRequest(t, ts.URL+"/")
	req.Header.Set("Authorization", "bearer "+token) // scheme is case-insensitive per RFC 7235.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for lowercase scheme", resp.StatusCode)
	}
}

func TestAuthBearer_RejectsMissingHeader(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be invoked without credentials")
	})

	ts := httptest.NewServer(server.AuthBearer(io.Discard, "s3cret", next))
	t.Cleanup(ts.Close)

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_request"`) {
		t.Errorf("WWW-Authenticate = %q, want invalid_request error (RFC 6750 §3)", got)
	}

	assertProblemJSON(t, resp, "unauthenticated", http.StatusUnauthorized)
}

func TestAuthBearer_RejectsWrongToken(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be invoked with bad token")
	})

	ts := httptest.NewServer(server.AuthBearer(io.Discard, "s3cret", next))
	t.Cleanup(ts.Close)

	req := newRequest(t, ts.URL+"/")
	req.Header.Set("Authorization", "Bearer wrong")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
		t.Errorf("WWW-Authenticate = %q, want invalid_token error", got)
	}

	assertProblemJSON(t, resp, "unauthenticated", http.StatusUnauthorized)
}

func TestAuthBearer_RejectsWrongScheme(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{"basic scheme", "Basic dXNlcjpwYXNz"},
		{"no scheme", "s3cret"},
		{"extra fields", "Bearer s3cret extra"},
		{"empty value", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("next handler should not be invoked for malformed Authorization header")
			})

			ts := httptest.NewServer(server.AuthBearer(io.Discard, "s3cret", next))
			t.Cleanup(ts.Close)

			req := newRequest(t, ts.URL+"/")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}

			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}

			if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_request"`) {
				t.Errorf("WWW-Authenticate = %q, want invalid_request (malformed header)", got)
			}
		})
	}
}

func TestAuthBearer_HealthzBypassesAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"without trailing slash", "/healthz"},
		{"with trailing slash", "/healthz/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var called atomic.Bool
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called.Store(true)

				_, _ = io.WriteString(w, "ok")
			})

			ts := httptest.NewServer(server.AuthBearer(io.Discard, "s3cret", next))
			t.Cleanup(ts.Close)

			resp := httpGet(t, ts.URL+tc.path) // no Authorization header
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200 (%s must bypass auth)", resp.StatusCode, tc.path)
			}

			if !called.Load() {
				t.Errorf("%s did not reach the next handler", tc.path)
			}
		})
	}
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		wantToken string
		wantOK    bool
	}{
		{"canonical", "Bearer abc", "abc", true},
		{"lowercase scheme", "bearer abc", "abc", true},
		{"mixed case scheme", "BeArEr abc", "abc", true},
		{"extra whitespace", "Bearer    abc", "abc", true},
		{"basic scheme", "Basic abc", "", false},
		{"no scheme", "abc", "", false},
		{"only scheme", "Bearer", "", false},
		{"three fields", "Bearer abc def", "", false},
		{"empty", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := server.BearerToken(tc.input)
			if got != tc.wantToken || ok != tc.wantOK {
				t.Errorf("BearerToken(%q) = (%q, %v), want (%q, %v)",
					tc.input, got, ok, tc.wantToken, tc.wantOK)
			}
		})
	}
}

func newRequest(t *testing.T, url string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}

	return req
}

func assertProblemJSON(t *testing.T, resp *http.Response, wantTypeSuffix string, wantStatus int) {
	t.Helper()

	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	var got server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if want := server.ProblemTypePrefix + wantTypeSuffix; got.Type != want {
		t.Errorf("Problem.Type = %q, want %q", got.Type, want)
	}

	if got.Status != wantStatus {
		t.Errorf("Problem.Status = %d, want %d", got.Status, wantStatus)
	}
}
