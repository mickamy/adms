package server

import (
	"context"
	"database/sql"
	"io"
	"net"
	"net/http"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/schema"
)

func (s *Server) Routes() http.Handler { return s.routes() }

func (s *Server) Serve(ctx context.Context, ln net.Listener) error { return s.serve(ctx, ln) }

func (s *Server) Prepare(ctx context.Context) error { return s.prepare(ctx) }

// NewWithIntrospector lets tests inject a stub introspector, bypassing the
// cfg.Driver → introspector mapping used by the public New constructor.
func NewWithIntrospector(
	cfg config.Config,
	db *sql.DB,
	intro schema.Introspector,
	logger io.Writer,
) (*Server, error) {
	return newServer(cfg, db, intro, logger)
}

var (
	Recoverer = recoverer
	Logging   = logging
)

const ProblemTypePrefix = problemTypePrefix

func WriteProblem(
	w http.ResponseWriter,
	r *http.Request,
	logger io.Writer,
	status int,
	typeSuffix, title, detail string,
) {
	writeProblem(w, r, logger, status, typeSuffix, title, detail)
}
