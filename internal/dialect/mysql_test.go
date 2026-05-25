package dialect_test

import (
	"testing"

	"github.com/mickamy/adms/internal/dialect"
)

func TestMySQLName(t *testing.T) {
	t.Parallel()

	if got := dialect.MySQL().Name(); got != "mysql" {
		t.Errorf("Name() = %q, want %q", got, "mysql")
	}
}

func TestMySQLQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"col", "`col`"},
		{"camelCase", "`camelCase`"},
		{"weird`name", "`weird``name`"},
		{"with space", "`with space`"},
	}

	d := dialect.MySQL()
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := d.Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMySQLPlaceholder(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	for i := 1; i <= 3; i++ {
		if got := d.Placeholder(i); got != "?" {
			t.Errorf("Placeholder(%d) = %q, want %q", i, got, "?")
		}
	}
}

func TestMySQLSupports(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	if d.SupportsILIKE() {
		t.Error("SupportsILIKE() = true, want false")
	}

	if d.SupportsReturning() {
		t.Error("SupportsReturning() = true, want false")
	}
}

func TestMySQLJSONAgg(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	if got, want := d.JSONAgg("x", ""), "JSON_ARRAYAGG(x)"; got != want {
		t.Errorf("JSONAgg(x, \"\") = %q, want %q", got, want)
	}

	if got, want := d.JSONAgg("x", "y ASC"), "JSON_ARRAYAGG(x)"; got != want {
		t.Errorf("JSONAgg(x, y ASC) = %q, want %q", got, want)
	}
}

func TestMySQLJSONObject(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()

	if got, want := d.JSONObject(nil), "JSON_OBJECT()"; got != want {
		t.Errorf("JSONObject(nil) = %q, want %q", got, want)
	}

	got := d.JSONObject([]string{"'id'", "`u`.`id`", "'name'", "`u`.`name`"})
	want := "JSON_OBJECT('id', `u`.`id`, 'name', `u`.`name`)"
	if got != want {
		t.Errorf("JSONObject(...) = %q, want %q", got, want)
	}
}

func TestMySQLEmptyJSONArray(t *testing.T) {
	t.Parallel()

	if got, want := dialect.MySQL().EmptyJSONArray(), "JSON_ARRAY()"; got != want {
		t.Errorf("EmptyJSONArray() = %q, want %q", got, want)
	}
}

func TestMySQLStringLiteral(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()

	tests := []struct {
		name, in, want string
	}{
		{"plain", "id", "'id'"},
		{"single quote doubled", "o'brien", "'o''brien'"},
		{"backslash doubled", `a\b`, `'a\\b'`},
		{"backslash before quote", `a\'b`, `'a\\''b'`},
		{"empty", "", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := d.StringLiteral(tt.in); got != tt.want {
				t.Errorf("StringLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMySQLContainmentExpr(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()

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
			name: "json cs", col: "`tags`", val: "?",
			columnType: "json", wantSQL: "JSON_CONTAINS(`tags`, ?)",
		},
		{
			name: "json cd swaps arguments", col: "`tags`", val: "?",
			columnType: "json", contained: true,
			wantSQL: "JSON_CONTAINS(?, `tags`)",
		},
		{
			name: "case insensitive type", col: "`tags`", val: "?",
			columnType: "JSON", wantSQL: "JSON_CONTAINS(`tags`, ?)",
		},
		{
			name: "non-json column errors", col: "`name`", val: "?",
			columnType: "varchar(255)", wantErr: true,
		},
		{
			name: "arrays unsupported on mysql", col: "`tags`", val: "?",
			columnType: "text[]", wantErr: true,
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
