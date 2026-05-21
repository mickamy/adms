package database_test

import (
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/database"
)

func TestOpenUnknownDriver(t *testing.T) {
	t.Parallel()

	_, err := database.Open("oracle", "irrelevant-dsn")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "unknown driver") {
		t.Errorf("Open() error = %q, want substring %q", err, "unknown driver")
	}
}
