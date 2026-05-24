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

func TestPost_RejectsBodyOver10MiB(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	// http.MaxBytesReader fires MaxBytesError once the underlying stream
	// exceeds the cap, so a body just past the limit is enough.
	big := strings.Repeat("a", (10<<20)+1)

	resp := httpRequest(t, http.MethodPost, ts.URL+"/users", big)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "body-too-large") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "body-too-large")
	}
}

func newMySQLTestServer(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	var logs syncBuffer

	srv, err := server.NewWithIntrospector(
		config.Config{
			Driver:       database.DriverMySQL,
			Timeout:      time.Second,
			DefaultLimit: 100,
			MaxLimit:     1000,
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

func assertProblemType(t *testing.T, resp *http.Response, wantSuffix string) {
	t.Helper()

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, wantSuffix) {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, wantSuffix)
	}
}

func httpRequestWithHeaders(t *testing.T, method, url, body string, headers map[string]string) *http.Response {
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

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	return resp
}

func TestWrite_RepresentationUnsupportedOnMySQL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"POST", http.MethodPost, "/users", `{"id":1,"name":"alice"}`},
		{"PATCH", http.MethodPatch, "/users?id=eq.1", `{"name":"alice2"}`},
		{"DELETE", http.MethodDelete, "/users?id=eq.1", ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := newMySQLTestServer(t, usersSchema())

			resp := httpRequestWithHeaders(t, tt.method, ts.URL+tt.path, tt.body,
				map[string]string{"Prefer": "return=representation"})
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want 501", resp.StatusCode)
			}

			assertProblemType(t, resp, "unsupported")
		})
	}
}

func TestPatch_RejectsBodyOver10MiB(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	big := strings.Repeat("a", (10<<20)+1)

	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users?id=eq.1", big)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}

	assertProblemType(t, resp, "body-too-large")
}

func TestPatch_RejectsInvalidQuery(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users?id=bogus.42", `{"name":"x"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	assertProblemType(t, resp, "invalid-query")
}

func TestPatch_RejectsUnknownColumnInBody(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users?id=eq.1", `{"ghost":"value"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	assertProblemType(t, resp, "invalid-body")
}

func TestPatch_RejectsUnknownColumnInFilter(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	// Valid SET body, but the filter references an unknown column. build.Update
	// surfaces a *build.FilterError; the handler must map that to invalid-query.
	resp := httpRequest(t, http.MethodPatch, ts.URL+"/users?ghost=eq.1", `{"name":"alice2"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	assertProblemType(t, resp, "invalid-query")
}

func TestDelete_RejectsInvalidQuery(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodDelete, ts.URL+"/users?id=bogus.42", "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	assertProblemType(t, resp, "invalid-query")
}

func TestDelete_RejectsUnknownColumnInFilter(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpRequest(t, http.MethodDelete, ts.URL+"/users?ghost=eq.1", "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("ghost")) {
		t.Errorf("body = %s, want it to mention column name", body)
	}
}

func TestParseInsertBody_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"whitespace only", "   "},
		{"invalid JSON array", "[not-json"},
		{"null inside array", "[null]"},
		{"null root", "null"},
		{"invalid JSON object", "{not-json"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := server.ParseInsertBody([]byte(tt.body)); err == nil {
				t.Errorf("expected error, got nil for %q", tt.body)
			}
		})
	}
}

func TestParseInsertBody_PreservesNumberPrecision(t *testing.T) {
	t.Parallel()

	// 12345678901234567890 exceeds 2^53; decoding via float64 would round it.
	body := []byte(`{"id": 12345678901234567890, "amount": 1.234567890123456789}`)

	rows, err := server.ParseInsertBody(body)
	if err != nil {
		t.Fatalf("ParseInsertBody: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	idNum, ok := rows[0]["id"].(json.Number)
	if !ok {
		t.Fatalf("rows[0][\"id\"] = %T, want json.Number", rows[0]["id"])
	}

	if string(idNum) != "12345678901234567890" {
		t.Errorf("id = %q, want preserved digits", string(idNum))
	}

	amountNum, ok := rows[0]["amount"].(json.Number)
	if !ok {
		t.Fatalf("rows[0][\"amount\"] = %T, want json.Number", rows[0]["amount"])
	}

	if string(amountNum) != "1.234567890123456789" {
		t.Errorf("amount = %q, want preserved decimals", string(amountNum))
	}
}

func TestParseUpdateBody_PreservesNumberPrecision(t *testing.T) {
	t.Parallel()

	body := []byte(`{"count": 9999999999999999999}`)

	set, err := server.ParseUpdateBody(body)
	if err != nil {
		t.Fatalf("ParseUpdateBody: %v", err)
	}

	n, ok := set["count"].(json.Number)
	if !ok {
		t.Fatalf("set[\"count\"] = %T, want json.Number", set["count"])
	}

	if string(n) != "9999999999999999999" {
		t.Errorf("count = %q, want preserved digits", string(n))
	}
}

func TestParseUpdateBody_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"invalid JSON", "{not-json"},
		{"null root", "null"},
		{"empty object", "{}"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := server.ParseUpdateBody([]byte(tt.body)); err == nil {
				t.Errorf("expected error, got nil for %q", tt.body)
			}
		})
	}
}
