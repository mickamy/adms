package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/logger"
	"github.com/mickamy/adms/internal/server"
	"github.com/mickamy/adms/internal/ui"
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

	if err := resolveAuth(&cfg); err != nil {
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

	if cfg.UI.Enabled {
		cfg.CORSOrigins = append(cfg.CORSOrigins, uiCORSOrigins(cfg.UI.Listen)...)
	}

	srv, err := server.New(cfg, db.DB)
	if err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	if err := srv.Prepare(sigCtx); err != nil {
		fmt.Fprintf(stderr, "adms: %v\n", err)

		return exit.Error
	}

	return runServers(sigCtx, cfg, srv, stderr)
}

// runServers drives the API server (and the UI server, when enabled) on
// their own goroutines and returns the first error either reports, or
// exit.OK if both exit cleanly after the shared context is cancelled.
func runServers(ctx context.Context, cfg config.Config, apiSrv *server.Server, stderr io.Writer) int {
	var wg sync.WaitGroup

	errCh := make(chan error, 2)

	wg.Go(func() {
		if err := apiSrv.Run(ctx); err != nil {
			errCh <- fmt.Errorf("api: %w", err)
		}
	})

	if cfg.UI.Enabled {
		uiSrv, err := ui.New(cfg, apiSrv.Schema(), apiOriginFromListen(cfg.Listen))
		if err != nil {
			fmt.Fprintf(stderr, "adms: %v\n", err)

			return exit.Error
		}

		wg.Go(func() {
			if err := uiSrv.Run(ctx); err != nil {
				errCh <- err
			}
		})
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		fmt.Fprintf(stderr, "adms: %v\n", err)
	}

	if ctx.Err() == nil {
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

// resolveAuth reads the static bearer token from its env var when auth.mode is
// static. It fails fast if the variable is unset, so opting into static auth
// cannot silently serve an open API. Other modes need no startup resolution.
func resolveAuth(cfg *config.Config) error {
	if cfg.Auth.Mode != config.AuthModeStatic {
		return nil
	}

	tok := os.Getenv(cfg.Auth.TokenEnv)
	if tok == "" {
		return fmt.Errorf(
			"auth.static.token_env %q is set but the environment variable is empty or unset",
			cfg.Auth.TokenEnv)
	}

	cfg.Auth.Token = tok

	return nil
}

// uiCORSOrigins returns the browser origins from which the UI will reach
// the API. Operators behind a reverse proxy with a custom host need to add
// to cors_origins manually; this only covers the localhost-only case.
func uiCORSOrigins(uiListen string) []string {
	_, port, err := net.SplitHostPort(uiListen)
	if err != nil || port == "" {
		return nil
	}

	return []string{
		"http://localhost:" + port,
		"http://127.0.0.1:" + port,
	}
}

// apiOriginFromListen turns a listen address into the origin the UI uses
// for HTMX requests. Hosts that do not bind to localhost need a reverse
// proxy or manual config; this helper covers the default :PORT case.
func apiOriginFromListen(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "http://localhost"
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func pingDB(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	return nil
}
