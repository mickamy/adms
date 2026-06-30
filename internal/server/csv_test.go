package server_test

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/server"
)

// failingWriter is an http.ResponseWriter whose body writes always fail,
// so writeCSV's flush surfaces the error.
type failingWriter struct{}

func (failingWriter) Header() http.Header       { return http.Header{} }
func (failingWriter) WriteHeader(int)           {}
func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

// errMarshaler is a TextMarshaler that always fails, exercising csvCell's
// fallback when MarshalText errors.
type errMarshaler struct{}

func (errMarshaler) MarshalText() ([]byte, error) { return nil, errors.New("nope") }

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
		// time.Time is a TextMarshaler: RFC 3339, matching the JSON response.
		{"time", time.Date(2023, 10, 12, 15, 4, 5, 0, time.UTC), "2023-10-12T15:04:05Z"},
		// A TextMarshaler that errors falls back to fmt's default.
		{"marshal error fallback", errMarshaler{}, "{}"},
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

func TestWriteCSV_PropagatesWriteError(t *testing.T) {
	t.Parallel()

	err := server.WriteCSV(failingWriter{}, []string{"id"}, []map[string]any{{"id": int64(1)}})
	if err == nil {
		t.Fatal("WriteCSV: want error from a failing writer, got nil")
	}
}
