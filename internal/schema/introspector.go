package schema

import (
	"context"
	"database/sql"
)

type Introspector interface {
	Introspect(ctx context.Context, db *sql.DB, allowedSchemas []string) (Schema, error)
}
