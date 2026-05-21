package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/schema"
)

func check(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exit.OK
		}

		fmt.Fprintf(stderr, "adms check: %v\n", err)

		return exit.Usage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	db, err := database.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: %v\n", err)

		return exit.Error
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(stderr, "adms check: ping failed: %v\n", err)

		return exit.Error
	}

	intro, err := pickIntrospector(cfg.Driver)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: %v\n", err)

		return exit.Error
	}

	sch, err := intro.Introspect(ctx, db.DB, cfg.AllowedSchemas)
	if err != nil {
		fmt.Fprintf(stderr, "adms check: introspect failed: %v\n", err)

		return exit.Error
	}

	sch = filterAllowedTables(sch, cfg.AllowedTables)

	printSummary(stdout, cfg, sch)

	return exit.OK
}

func printSummary(w io.Writer, cfg config.Config, sch schema.Schema) {
	counts := make(map[string]int)
	for _, tbl := range sch.Tables {
		counts[tbl.Schema]++
	}

	displayed := cfg.AllowedSchemas
	if len(displayed) == 0 {
		if len(counts) > 0 {
			displayed = make([]string, 0, len(counts))
			for s := range counts {
				displayed = append(displayed, s)
			}

			sort.Strings(displayed)
		} else {
			displayed = []string{"(driver default)"}
		}
	}

	fmt.Fprintf(w, "ok: connected, introspected %d tables in schema(s) %s\n",
		len(sch.Tables), strings.Join(displayed, ", "))

	if len(displayed) == 1 && displayed[0] == "(driver default)" {
		return
	}

	for _, s := range displayed {
		fmt.Fprintf(w, "  %s: %d tables\n", s, counts[s])
	}
}

func filterAllowedTables(sch schema.Schema, allowed []string) schema.Schema {
	if len(allowed) == 0 {
		return sch
	}

	allow := make(map[string]struct{}, len(allowed))
	for _, n := range allowed {
		allow[n] = struct{}{}
	}

	filtered := make([]schema.Table, 0, len(sch.Tables))
	for _, t := range sch.Tables {
		if _, ok := allow[t.Name]; ok {
			filtered = append(filtered, t)
		}
	}

	sch.Tables = filtered

	return sch
}

func pickIntrospector(d database.Driver) (schema.Introspector, error) {
	switch d {
	case database.DriverPostgres:
		return schema.PostgresIntrospector(), nil
	case database.DriverMySQL:
		return schema.MySQLIntrospector(), nil
	default:
		return nil, fmt.Errorf("unknown driver: %q", d)
	}
}
