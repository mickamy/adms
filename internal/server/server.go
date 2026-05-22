package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		fmt.Fprintf(s.Logger, "adms: listening on %s\n", s.Addr)

		err := srv.ListenAndServe()
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

		return nil
	}
}
