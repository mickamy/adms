package database_test

import (
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/dialect"
)

func TestDriverDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		drv  database.Driver
		want dialect.Dialect
	}{
		{"postgres", database.DriverPostgres, dialect.Postgres()},
		{"mysql", database.DriverMySQL, dialect.MySQL()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.drv.Dialect()
			if err != nil {
				t.Fatalf("Driver(%q).Dialect(): %v", tt.drv, err)
			}

			if got.Name() != tt.want.Name() {
				t.Errorf("Driver(%q).Dialect().Name() = %q, want %q", tt.drv, got.Name(), tt.want.Name())
			}
		})
	}
}

func TestDriverDialectUnknown(t *testing.T) {
	t.Parallel()

	_, err := database.Driver("oracle").Dialect()
	if err == nil {
		t.Fatal("Driver(\"oracle\").Dialect(): error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "unknown driver") {
		t.Errorf("error = %q, want substring %q", err, "unknown driver")
	}
}
