package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func newReadOnlyTestServer(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	var logs syncBuffer

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverPostgres,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
			ReadOnly:     true,
		},
		stubDB,
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

	return ts
}

func httpRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, url, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	return resp
}

func TestParsePrefer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header []string
		want   server.PreferDirective
	}{
		{
			"absent header defaults to minimal",
			nil,
			server.PreferDirective{Return: server.PreferReturnMinimal},
		},
		{
			"return=representation",
			[]string{"return=representation"},
			server.PreferDirective{Return: server.PreferReturnRepresentation},
		},
		{
			"return=minimal overrides default explicitly",
			[]string{"return=minimal"},
			server.PreferDirective{Return: server.PreferReturnMinimal},
		},
		{
			"count=exact alone",
			[]string{"count=exact"},
			server.PreferDirective{Return: server.PreferReturnMinimal, CountExact: true},
		},
		{
			"combined in one header",
			[]string{"return=representation, count=exact"},
			server.PreferDirective{Return: server.PreferReturnRepresentation, CountExact: true},
		},
		{
			"unknown tokens are ignored",
			[]string{"resolution=merge-duplicates"},
			server.PreferDirective{Return: server.PreferReturnMinimal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := http.Header{}
			for _, v := range tt.header {
				h.Add("Prefer", v)
			}

			got := server.ParsePrefer(h)
			if got != tt.want {
				t.Errorf("ParsePrefer = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestContentRangeReturned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{0, "*/0"},
		{1, "0-0/1"},
		{3, "0-2/3"},
	}

	for _, tt := range tests {
		got := server.ContentRangeReturned(tt.n)
		if got != tt.want {
			t.Errorf("ContentRangeReturned(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestWrite_ReadOnlyForbids(t *testing.T) {
	t.Parallel()

	ts := newReadOnlyTestServer(t, usersSchema())

	cases := []struct {
		method string
		body   string
	}{
		{http.MethodPost, `{"id":1}`},
		{http.MethodPatch, `{"name":"x"}`},
		{http.MethodDelete, ""},
	}

	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			t.Parallel()

			resp := httpRequest(t, c.method, ts.URL+"/users?id=eq.1", c.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("status = %d, want 403", resp.StatusCode)
			}

			var p server.Problem
			if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if !strings.HasSuffix(p.Type, "read-only") {
				t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "read-only")
			}
		})
	}
}

func TestWrite_UnknownTable(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, schema.Schema{})

	cases := []struct {
		method string
		body   string
	}{
		{http.MethodPost, `{"id":1}`},
		{http.MethodPatch, `{"name":"x"}`},
		{http.MethodDelete, ""},
	}

	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			t.Parallel()

			resp := httpRequest(t, c.method, ts.URL+"/ghost?id=eq.1", c.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}

			var p server.Problem
			if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if !strings.HasSuffix(p.Type, "unknown-table") {
				t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "unknown-table")
			}
		})
	}
}

func TestPatch_RequiresFilter(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users", `{"name":"alice"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "unfiltered-write") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "unfiltered-write")
	}
}

func TestDelete_RequiresFilter(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodDelete, ts.URL+"/users", "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "unfiltered-write") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "unfiltered-write")
	}
}

func TestPost_RejectsEmptyBody(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPost, ts.URL+"/users", "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPost_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPost, ts.URL+"/users", "not json")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "invalid-body") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "invalid-body")
	}
}

func TestPost_RejectsEmptyArray(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPost, ts.URL+"/users", "[]")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPost_RejectsUnknownColumn(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPost, ts.URL+"/users", `{"ghost":"value"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("ghost")) {
		t.Errorf("body = %s, want it to mention column name", body)
	}
}

func TestPatch_RejectsEmptyObject(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users?id=eq.1", `{}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
