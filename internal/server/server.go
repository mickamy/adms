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
	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/schema"
)

const shutdownTimeout = 10 * time.Second

type Server struct {
	addr           string
	db             *sql.DB
	introspector   schema.Introspector
	dialect        dialect.Dialect
	allowedSchemas []string
	allowedTables  []string
	defaultLimit   int
	maxLimit       int
	maxBodyBytes   int64
	readOnly       bool
	timeout        time.Duration
	logger         io.Writer

	schema     schema.Schema
	schemaJSON []byte
	tableIndex tableLookup
}

// tableLookup adapts the introspected table index to build.SchemaLookup so
// the SQL builder can resolve embedded relations.
type tableLookup map[string]*schema.Table

func (l tableLookup) Table(name string) (*schema.Table, bool) {
	t, ok := l[name]
	return t, ok
}

func New(cfg config.Config, db *sql.DB, logger io.Writer) (*Server, error) {
	intro, err := cfg.Driver.Introspector()
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}

	return newServer(cfg, db, intro, logger)
}

func newServer(cfg config.Config, db *sql.DB, intro schema.Introspector, logger io.Writer) (*Server, error) {
	if intro == nil {
		return nil, errors.New("server: introspector is required")
	}

	if db == nil {
		return nil, errors.New("server: db is required")
	}

	if cfg.Timeout <= 0 {
		return nil, errors.New("server: timeout must be positive")
	}

	if cfg.DefaultLimit <= 0 {
		return nil, errors.New("server: default_limit must be positive")
	}

	if cfg.MaxLimit <= 0 {
		return nil, errors.New("server: max_limit must be positive")
	}

	if cfg.DefaultLimit > cfg.MaxLimit {
		return nil, fmt.Errorf(
			"server: default_limit (%d) must not exceed max_limit (%d)",
			cfg.DefaultLimit, cfg.MaxLimit)
	}

	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = config.DefaultMaxBodyBytes
	}

	dlc, err := cfg.Driver.Dialect()
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}

	if logger == nil {
		logger = io.Discard
	}

	return &Server{
		addr:           cfg.Listen,
		db:             db,
		introspector:   intro,
		dialect:        dlc,
		allowedSchemas: cfg.AllowedSchemas,
		allowedTables:  cfg.AllowedTables,
		defaultLimit:   cfg.DefaultLimit,
		maxLimit:       cfg.MaxLimit,
		maxBodyBytes:   maxBodyBytes,
		readOnly:       cfg.ReadOnly,
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

		realShutdownErr := shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed)
		if realShutdownErr {
			// Graceful shutdown failed (ctx timeout, hung handlers). Force-close
			// the listener and any active connections so we do not leak them.
			_ = srv.Close()
		}

		// Wait for Serve to return regardless of shutdown outcome, so we surface
		// a serve error that raced with the shutdown.
		serveErr := <-errCh

		// Prefer serveErr when both fail: Shutdown's ErrServerClosed is just a
		// side effect of Serve having already exited.
		if serveErr != nil {
			return fmt.Errorf("serve: %w", serveErr)
		}

		if realShutdownErr {
			return fmt.Errorf("shutdown: %w", shutdownErr)
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

	idx, err := indexTables(s.schema.Tables)
	if err != nil {
		return fmt.Errorf("index tables: %w", err)
	}

	s.tableIndex = idx

	body, err := json.MarshalIndent(s.schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	s.schemaJSON = body

	return nil
}

// indexTables builds a name → *Table map for O(1) route lookup. Duplicate
// table names across schemas yield an error rather than a silent overwrite,
// so the caller can either narrow allowed_schemas/allowed_tables or extend
// the routing scheme to disambiguate.
func indexTables(tables []schema.Table) (tableLookup, error) {
	idx := make(tableLookup, len(tables))

	for i := range tables {
		t := &tables[i]
		if existing, ok := idx[t.Name]; ok {
			return nil, fmt.Errorf(
				"duplicate table name %q in schemas %q and %q",
				t.Name, existing.Schema, t.Schema)
		}

		idx[t.Name] = t
	}

	return idx, nil
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
