package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/schema"
)

func check(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: %v\n", err)

		return exit.Usage
	}

	db, err := database.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: %v\n", err)

		return exit.Error
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(stderr, "adms check: ping failed: %v\n", err)

		return exit.Error
	}

	intro := pickIntrospector(cfg.Driver)

	sch, err := intro.Introspect(ctx, db.DB, cfg.AllowedSchemas)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: introspect failed: %v\n", err)

		return exit.Error
	}

	displayed := cfg.AllowedSchemas
	if len(displayed) == 0 {
		displayed = []string{"(driver default)"}
	}

	fmt.Fprintf(stdout, "ok: connected, introspected %d tables in schema(s) %s\n",
		len(sch.Tables), strings.Join(displayed, ", "))

	return exit.OK
}

func pickIntrospector(d database.Driver) schema.Introspector {
	switch d {
	case database.DriverPostgres:
		return schema.PostgresIntrospector()
	case database.DriverMySQL:
		return schema.MySQLIntrospector()
	default:
		panic(fmt.Sprintf("unknown driver: %q", d))
	}
}
