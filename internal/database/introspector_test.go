package database_test

import (
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/database"
)

func TestDriverIntrospector(t *testing.T) {
	t.Parallel()

	for _, drv := range []database.Driver{database.DriverPostgres, database.DriverMySQL} {
		t.Run(string(drv), func(t *testing.T) {
			t.Parallel()

			intro, err := drv.Introspector()
			if err != nil {
				t.Fatalf("Driver(%q).Introspector(): %v", drv, err)
			}

			if intro == nil {
				t.Errorf("Driver(%q).Introspector() = nil, want non-nil", drv)
			}
		})
	}
}

func TestDriverIntrospectorUnknown(t *testing.T) {
	t.Parallel()

	_, err := database.Driver("oracle").Introspector()
	if err == nil {
		t.Fatal("Driver(\"oracle\").Introspector(): error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "unknown driver") {
		t.Errorf("error = %q, want substring %q", err, "unknown driver")
	}
}
