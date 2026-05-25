package dialect_test

import (
	"testing"

	"github.com/mickamy/adms/internal/dialect"
)

func TestPostgresName(t *testing.T) {
	t.Parallel()

	if got := dialect.Postgres().Name(); got != "postgres" {
		t.Errorf("Name() = %q, want %q", got, "postgres")
	}
}

func TestPostgresQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"col", `"col"`},
		{"camelCase", `"camelCase"`},
		{`weird"name`, `"weird""name"`},
		{"with space", `"with space"`},
	}

	d := dialect.Postgres()
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := d.Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPostgresPlaceholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		i    int
		want string
	}{
		{1, "$1"},
		{2, "$2"},
		{10, "$10"},
	}

	d := dialect.Postgres()
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := d.Placeholder(tt.i); got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.i, got, tt.want)
			}
		})
	}
}

func TestPostgresSupports(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	if !d.SupportsILIKE() {
		t.Error("SupportsILIKE() = false, want true")
	}

	if !d.SupportsReturning() {
		t.Error("SupportsReturning() = false, want true")
	}
}

func TestPostgresJSONAgg(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	if got, want := d.JSONAgg("x", ""), "json_agg(x)"; got != want {
		t.Errorf("JSONAgg(x, \"\") = %q, want %q", got, want)
	}

	if got, want := d.JSONAgg("x", "y ASC"), "json_agg(x ORDER BY y ASC)"; got != want {
		t.Errorf("JSONAgg(x, y ASC) = %q, want %q", got, want)
	}
}

func TestPostgresJSONObject(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()

	if got, want := d.JSONObject(nil), "json_build_object()"; got != want {
		t.Errorf("JSONObject(nil) = %q, want %q", got, want)
	}

	got := d.JSONObject([]string{"'id'", `"u"."id"`, "'name'", `"u"."name"`})
	want := `json_build_object('id', "u"."id", 'name', "u"."name")`
	if got != want {
		t.Errorf("JSONObject(...) = %q, want %q", got, want)
	}
}

func TestPostgresEmptyJSONArray(t *testing.T) {
	t.Parallel()

	if got, want := dialect.Postgres().EmptyJSONArray(), "'[]'::json"; got != want {
		t.Errorf("EmptyJSONArray() = %q, want %q", got, want)
	}
}

func TestPostgresStringLiteral(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()

	tests := []struct {
		in, want string
	}{
		{"id", "'id'"},
		{"o'brien", "'o''brien'"},
		{`a\b`, `'a\b'`},
		{``, `''`},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := d.StringLiteral(tt.in); got != tt.want {
				t.Errorf("StringLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPostgresContainmentExpr(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()

	tests := []struct {
		name       string
		col        string
		val        string
		columnType string
		contained  bool
		wantSQL    string
		wantErr    bool
	}{
		{
			name: "jsonb cs", col: `"tags"`, val: "$1",
			columnType: "jsonb", wantSQL: `"tags" @> $1::jsonb`,
		},
		{
			name: "jsonb cd", col: `"tags"`, val: "$1",
			columnType: "jsonb", contained: true,
			wantSQL: `"tags" <@ $1::jsonb`,
		},
		{
			name: "json upcast to jsonb", col: `"payload"`, val: "$1",
			columnType: "json", wantSQL: `"payload"::jsonb @> $1::jsonb`,
		},
		{
			name: "text array", col: `"roles"`, val: "$1",
			columnType: "text[]", wantSQL: `"roles" @> $1::text[]`,
		},
		{
			name: "integer array contained", col: `"ids"`, val: "$1",
			columnType: "integer[]", contained: true,
			wantSQL: `"ids" <@ $1::integer[]`,
		},
		{
			name: "numeric array preserves precision", col: `"scores"`, val: "$1",
			columnType: "numeric(10,2)[]", wantSQL: `"scores" @> $1::numeric(10,2)[]`,
		},
		{
			name: "case insensitive type", col: `"tags"`, val: "$1",
			columnType: "JSONB", wantSQL: `"tags" @> $1::jsonb`,
		},
		{
			name: "unsupported column type errors", col: `"name"`, val: "$1",
			columnType: "text", wantErr: true,
		},
		{
			name: "empty type errors", col: `"x"`, val: "$1",
			columnType: "", wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := d.ContainmentExpr(tt.col, tt.val, tt.columnType, tt.contained)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ContainmentExpr(%q) = %q, want error", tt.columnType, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ContainmentExpr(%q) returned unexpected error: %v", tt.columnType, err)
				return
			}
			if got != tt.wantSQL {
				t.Errorf("ContainmentExpr(%q) = %q, want %q", tt.columnType, got, tt.wantSQL)
			}
		})
	}
}
