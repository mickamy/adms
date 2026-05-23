package server_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func usersSchema() schema.Schema {
	return schema.Schema{
		Tables: []schema.Table{
			{
				Schema:     "public",
				Name:       "users",
				PrimaryKey: []string{"id"},
				Columns: []schema.Column{
					{Name: "id"},
					{Name: "name"},
				},
			},
		},
	}
}

func TestRead_UnknownTableReturnsProblem(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, schema.Schema{})

	resp := httpGet(t, ts.URL+"/ghost")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "unknown-table") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "unknown-table")
	}

	if p.Status != http.StatusNotFound {
		t.Errorf("Problem.Status = %d, want %d", p.Status, http.StatusNotFound)
	}

	if !strings.Contains(p.Detail, "ghost") {
		t.Errorf("Problem.Detail = %q, want it to mention table name", p.Detail)
	}
}

func TestRead_InvalidQueryReturnsProblem(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpGet(t, ts.URL+"/users?id=bogus.42")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.HasSuffix(p.Type, "invalid-query") {
		t.Errorf("Problem.Type = %q, want suffix %q", p.Type, "invalid-query")
	}
}

func TestRead_UnknownColumnReturnsProblem(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, usersSchema())

	resp := httpGet(t, ts.URL+"/users?ghost=eq.1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var p server.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !strings.Contains(p.Detail, "ghost") {
		t.Errorf("Problem.Detail = %q, want it to mention column name", p.Detail)
	}
}

func TestNormalizeScanValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want any
	}{
		{"nil passes through", nil, nil},
		{"int64 passes through", int64(42), int64(42)},
		{"string passes through", "hello", "hello"},
		{"valid UTF-8 bytes become string", []byte("hello"), "hello"},
		{"invalid UTF-8 bytes preserved as []byte", []byte{0xff, 0xfe, 0xfd}, []byte{0xff, 0xfe, 0xfd}},
		{"empty bytes become empty string", []byte{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := server.NormalizeScanValue(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
