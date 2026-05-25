// Package ui serves the optional admin web UI on its own listener so the
// API URL space can stay 100% owned by table names. The UI is enabled
// via ui.enabled in the config and shuts down with the API server when
// the shared signal context fires.
package ui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/logger"
	"github.com/mickamy/adms/internal/schema"
)

const shutdownTimeout = 10 * time.Second

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	addr      string
	apiOrigin string
	authToken string
	schema    schema.Schema
	readOnly  bool
	tmpl      *template.Template
}

// New constructs the UI server. apiOrigin is the URL the browser uses to
// reach the API server (e.g., "http://localhost:7777"); HTMX requests on
// the rendered pages target it directly, so the API host must allow this
// origin via CORS.
func New(cfg config.Config, sch schema.Schema, apiOrigin string) (*Server, error) {
	if apiOrigin == "" {
		return nil, errors.New("ui: apiOrigin is required")
	}

	// Funcs must be registered before ParseFS so every template parsed
	// below can call them (e.g., `{{inputKind .}}`). A bare
	// template.ParseFS would fail to compile templates that reference
	// these funcs, so all UI templates must come through this constructor.
	tmpl, err := template.New("ui").Funcs(template.FuncMap{
		"inputKind": inputKind,
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("ui: parse templates: %w", err)
	}

	return &Server{
		addr:      cfg.UI.Listen,
		apiOrigin: apiOrigin,
		authToken: cfg.AuthToken,
		schema:    sch,
		readOnly:  cfg.ReadOnly,
		tmpl:      tmpl,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ui listen: %w", err)
	}

	return s.serve(ctx, ln)
}

func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	logger.Info(ctx, "ui listening", "addr", ln.Addr().String())

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
		logger.Info(ctx, "ui shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		shutdownErr := srv.Shutdown(shutdownCtx) //nolint:contextcheck
		realShutdownErr := shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed)

		if realShutdownErr {
			_ = srv.Close()
		}

		serveErr := <-errCh

		if serveErr != nil {
			return fmt.Errorf("ui serve: %w", serveErr)
		}

		if realShutdownErr {
			return fmt.Errorf("ui shutdown: %w", shutdownErr)
		}

		return nil
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /__healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /t/{table}", s.tableView)
	mux.HandleFunc("GET /t/{table}/new", s.newRow)
	mux.HandleFunc("GET /t/{table}/r/{pk}", s.rowView)

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Errorf("ui: derive static sub-FS: %w", err))
	}

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	return mux
}
