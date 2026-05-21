package database

import (
	"database/sql"
	"fmt"

	"github.com/mickamy/adms/internal/dialect"
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
)

type DB struct {
	*sql.DB

	Dialect dialect.Dialect
}

func Open(driver Driver, dsn string) (*DB, error) {
	switch driver {
	case DriverPostgres:
		return openPostgres(dsn)
	case DriverMySQL:
		return openMySQL(dsn)
	default:
		return nil, fmt.Errorf("unknown driver: %q", driver)
	}
}
