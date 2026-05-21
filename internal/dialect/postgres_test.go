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
