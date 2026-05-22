package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/schema"
)

const shutdownTimeout = 10 * time.Second

type Server struct {
	Addr    string
	Schema  schema.Schema
	DB      *sql.DB
	Dialect dialect.Dialect
	Logger  io.Writer
}

func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.serve(ctx, ln)
}

func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	fmt.Fprintf(s.Logger, "adms: listening on %s\n", ln.Addr())

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
		fmt.Fprintln(s.Logger, "adms: shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck
			return fmt.Errorf("shutdown: %w", err)
		}

		// Surface any error from Serve that raced with shutdown.
		if err := <-errCh; err != nil {
			return fmt.Errorf("serve: %w", err)
		}

		return nil
	}
}
