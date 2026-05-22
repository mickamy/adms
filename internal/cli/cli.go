package cli

import (
	"context"
	"errors"
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

func Run(args []string, stdout, stderr io.Writer) int {
	path, err := resolveConfigPath(args)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)
		PrintUsage(stderr)

		return exit.Usage
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Usage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	db, err := database.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(stderr, "adms: ping failed: %v\n", err)

		return exit.Error
	}

	intro, err := pickIntrospector(cfg.Driver)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	sch, err := intro.Introspect(ctx, db.DB, cfg.AllowedSchemas)
	if err != nil {
		fmt.Fprintf(stderr, "adms: introspect failed: %v\n", err)

		return exit.Error
	}

	sch = filterAllowedTables(sch, cfg.AllowedTables)

	printSummary(stdout, cfg, sch)

	return exit.OK
}

func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, "adms — PostgREST-style HTTP API server for PostgreSQL and MySQL")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "USAGE:")
	fmt.Fprintln(w, "  adms [config-file]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  With no argument, adms looks for adms.yaml, adms.yml, then adms.toml")
	fmt.Fprintln(w, "  in the current directory. Pass a path to use a specific config file.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Strings in the config file are expanded from the environment:")
	fmt.Fprintln(w, `  both ${VAR} and $VAR are substituted (unset variables become "").`)
	fmt.Fprintln(w, "  Literal $ cannot be escaped, so store values containing $ in an")
	fmt.Fprintln(w, `  environment variable (e.g., dsn: "${ADMS_DSN}").`)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "FLAGS:")
	fmt.Fprintln(w, "  --version, -v   Print adms version")
	fmt.Fprintln(w, "  --help, -h      Show this help")
}

func resolveConfigPath(args []string) (string, error) {
	switch len(args) {
	case 0:
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}

		path, err := config.Detect(cwd)
		if err != nil {
			if errors.Is(err, config.ErrNoConfigFound) {
				return "", errors.New("no config file found in current directory " +
					"(looked for adms.yaml, adms.yml, adms.toml)")
			}

			return "", fmt.Errorf("detect config: %w", err)
		}

		return path, nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("too many arguments (got %d, want at most 1)", len(args))
	}
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
			displayed = []string{"(no tables found)"}
		}
	}

	fmt.Fprintf(w, "ok: connected, introspected %d tables in schema(s) %s\n",
		len(sch.Tables), strings.Join(displayed, ", "))

	if len(displayed) == 1 && displayed[0] == "(no tables found)" {
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
