package database

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"

	"github.com/mickamy/adms/internal/dialect"
)

func openMySQL(dsn string) (*DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	return &DB{DB: conn, Dialect: dialect.MySQL()}, nil
}
