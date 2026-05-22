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

	"github.com/mickamy/adms/internal/schema"
)

const shutdownTimeout = 10 * time.Second

type Server struct {
	Addr           string
	DB             *sql.DB
	Introspector   schema.Introspector
	AllowedSchemas []string
	AllowedTables  []string
	Timeout        time.Duration
	Logger         io.Writer

	schema     schema.Schema
	schemaJSON []byte
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.serve(ctx, ln)
}

func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	fmt.Fprintf(s.logger(), "adms: listening on %s\n", ln.Addr())

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
		fmt.Fprintln(s.logger(), "adms: shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck
			return fmt.Errorf("shutdown: %w", err)
		}

		if err := <-errCh; err != nil {
			return fmt.Errorf("serve: %w", err)
		}

		return nil
	}
}

// prepare introspects the schema, applies the table allowlist, and marshals
// the schema to JSON once so the schema handler can serve bytes without
// re-encoding on every request.
func (s *Server) prepare(ctx context.Context) error {
	prepCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	sch, err := s.Introspector.Introspect(prepCtx, s.DB, s.AllowedSchemas)
	if err != nil {
		return fmt.Errorf("introspect: %w", err)
	}

	s.schema = filterAllowedTables(sch, s.AllowedTables)

	body, err := json.MarshalIndent(s.schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	s.schemaJSON = body

	return nil
}

func (s *Server) logger() io.Writer {
	if s.Logger == nil {
		return io.Discard
	}

	return s.Logger
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
