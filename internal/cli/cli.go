package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/logger"
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

	if err := resolveAuthToken(&cfg); err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Usage
	}

	logger.Init(stderr, cfg.LogLevel)

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}
	defer func() { _ = db.Close() }()

	if err := pingDB(sigCtx, db.DB, cfg.Timeout); err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	srv, err := server.New(cfg, db.DB)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	if err := srv.Run(sigCtx); err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

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

// resolveAuthToken fails fast if AuthTokenEnv is set but unresolved, so
// opting into auth cannot silently serve an open API.
func resolveAuthToken(cfg *config.Config) error {
	if cfg.AuthTokenEnv == "" {
		return nil
	}

	tok := os.Getenv(cfg.AuthTokenEnv)
	if tok == "" {
		return fmt.Errorf(
			"auth_token_env %q is set but the environment variable is empty or unset",
			cfg.AuthTokenEnv)
	}

	cfg.AuthToken = tok

	return nil
}

func pingDB(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	return nil
}
