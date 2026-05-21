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
