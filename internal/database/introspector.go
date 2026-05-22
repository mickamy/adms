package database

import (
	"fmt"

	"github.com/mickamy/adms/internal/schema"
)

// Introspector returns the schema introspector that matches the driver, or an
// error if the driver is unknown.
func (d Driver) Introspector() (schema.Introspector, error) {
	switch d {
	case DriverPostgres:
		return schema.PostgresIntrospector(), nil
	case DriverMySQL:
		return schema.MySQLIntrospector(), nil
	default:
		return nil, fmt.Errorf("unknown driver: %q", d)
	}
}
