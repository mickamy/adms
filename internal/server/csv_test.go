package server_test

import (
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/server"
)

func TestWantsCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		accept string
		want   bool
	}{
		{"text/csv", true},
		{"text/csv; charset=utf-8", true},
		{"application/json, text/csv;q=0.9", true},
		{"  text/csv  ", true},
		{"application/json", false},
		{"*/*", false},
		{"", false},
		{"text/csvx", false},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			t.Parallel()

			if got := server.WantsCSV(tt.accept); got != tt.want {
				t.Errorf("WantsCSV(%q) = %v, want %v", tt.accept, got, tt.want)
			}
		})
	}
}

func TestCSVCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"embed json", json.RawMessage(`{"a":1}`), `{"a":1}`},
		{"binary base64", []byte{0xff, 0xfe}, "//4="},
		{"int64", int64(42), "42"},
		{"float64", 3.5, "3.5"},
		{"bool", true, "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := server.CSVCell(tt.in); got != tt.want {
				t.Errorf("CSVCell(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWriteCSV(t *testing.T) {
	t.Parallel()

	cols := []string{"id", "name", "posts"}
	rows := []map[string]any{
		{"id": int64(1), "name": "Alice", "posts": json.RawMessage(`[{"t":"hi"}]`)},
		{"id": int64(2), "name": "Bob, \"Jr.\"\nX", "posts": nil},
	}

	rec := httptest.NewRecorder()
	if err := server.WriteCSV(rec, cols, rows); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	got, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}

	// Column order follows cols; embeds become their JSON text; NULL is an
	// empty cell; commas, quotes, and newlines round-trip through RFC 4180
	// quoting.
	want := [][]string{
		{"id", "name", "posts"},
		{"1", "Alice", `[{"t":"hi"}]`},
		{"2", "Bob, \"Jr.\"\nX", ""},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("WriteCSV =\n%#v\nwant\n%#v", got, want)
	}
}

func TestWriteCSV_EmptyRowsStillWritesHeader(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if err := server.WriteCSV(rec, []string{"id", "name"}, []map[string]any{}); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	if got := strings.TrimRight(rec.Body.String(), "\n"); got != "id,name" {
		t.Errorf("header = %q, want %q", got, "id,name")
	}
}
