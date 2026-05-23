package database

import (
	"fmt"

	"github.com/mickamy/adms/internal/dialect"
)

// Dialect returns the SQL dialect that matches the driver, or an error if
// the driver is unknown.
func (d Driver) Dialect() (dialect.Dialect, error) {
	switch d {
	case DriverPostgres:
		return dialect.Postgres(), nil
	case DriverMySQL:
		return dialect.MySQL(), nil
	default:
		return nil, fmt.Errorf("unknown driver: %q", d)
	}
}
