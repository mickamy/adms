package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/schema"
)

const shutdownTimeout = 10 * time.Second

type Server struct {
	addr           string
	db             *sql.DB
	introspector   schema.Introspector
	allowedSchemas []string
	allowedTables  []string
	timeout        time.Duration
	logger         io.Writer

	schema     schema.Schema
	schemaJSON []byte
}

func New(cfg config.Config, db *sql.DB, logger io.Writer) (*Server, error) {
	intro, err := cfg.Driver.Introspector()
	if err != nil {
		return nil, err //nolint:wrapcheck // Driver.Introspector already prefixes "database:".
	}

	return newServer(cfg, db, intro, logger)
}

func newServer(cfg config.Config, db *sql.DB, intro schema.Introspector, logger io.Writer) (*Server, error) {
	if intro == nil {
		return nil, errors.New("server: introspector is required")
	}

	if cfg.Timeout <= 0 {
		return nil, errors.New("server: timeout must be positive")
	}

	if logger == nil {
		logger = io.Discard
	}

	return &Server{
		addr:           cfg.Listen,
		db:             db,
		introspector:   intro,
		allowedSchemas: cfg.AllowedSchemas,
		allowedTables:  cfg.AllowedTables,
		timeout:        cfg.Timeout,
		logger:         logger,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.serve(ctx, ln)
}

func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	fmt.Fprintf(s.logger, "adms: listening on %s\n", ln.Addr())

	srv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)

	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Fprintln(s.logger, "adms: shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		shutdownErr := srv.Shutdown(shutdownCtx) //nolint:contextcheck
		if shutdownErr != nil {
			// Graceful shutdown failed (ctx timeout, hung handlers). Force-close
			// the listener and any active connections so we do not leak them.
			_ = srv.Close()
		}

		// Wait for Serve to return regardless of shutdown outcome, so we surface
		// a serve error that raced with the shutdown.
		serveErr := <-errCh

		if shutdownErr != nil {
			return fmt.Errorf("shutdown: %w", shutdownErr)
		}

		if serveErr != nil {
			return fmt.Errorf("serve: %w", serveErr)
		}

		return nil
	}
}

// prepare introspects the schema, applies the table allowlist, and marshals
// the schema to JSON once so the schema handler can serve bytes without
// re-encoding on every request.
func (s *Server) prepare(ctx context.Context) error {
	prepCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	sch, err := s.introspector.Introspect(prepCtx, s.db, s.allowedSchemas)
	if err != nil {
		return fmt.Errorf("introspect: %w", err)
	}

	s.schema = filterAllowedTables(sch, s.allowedTables)

	body, err := json.MarshalIndent(s.schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	s.schemaJSON = body

	return nil
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
