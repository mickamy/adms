package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/server"
)

func Run(args []string, _, stderr io.Writer) int {
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

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, sch, err := startup(sigCtx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}
	defer func() { _ = db.Close() }()

	srv := &server.Server{
		Addr:    cfg.Listen,
		Schema:  sch,
		DB:      db.DB,
		Dialect: db.Dialect,
		Logger:  stderr,
	}

	if err := srv.Run(sigCtx); err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	return exit.OK
}

// startup opens the database, pings it, and introspects the schema, all
// bounded by cfg.Timeout. Returns the open DB so the caller can hand it to the
// long-running server (and Close it on exit).
func startup(ctx context.Context, cfg config.Config) (*database.DB, schema.Schema, error) {
	startupCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	db, err := database.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, schema.Schema{}, fmt.Errorf("open db: %w", err)
	}

	if err := db.PingContext(startupCtx); err != nil {
		_ = db.Close()

		return nil, schema.Schema{}, fmt.Errorf("ping: %w", err)
	}

	intro, err := pickIntrospector(cfg.Driver)
	if err != nil {
		_ = db.Close()

		return nil, schema.Schema{}, err
	}

	sch, err := intro.Introspect(startupCtx, db.DB, cfg.AllowedSchemas)
	if err != nil {
		_ = db.Close()

		return nil, schema.Schema{}, fmt.Errorf("introspect: %w", err)
	}

	return db, filterAllowedTables(sch, cfg.AllowedTables), nil
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
