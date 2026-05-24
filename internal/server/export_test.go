package server

import (
	"context"
	"database/sql"
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
) (*Server, error) {
	return newServer(cfg, db, intro)
}

var (
	Recoverer            = recoverer
	Logging              = logging
	AuthBearer           = authBearer
	BearerToken          = bearerToken
	Cors                 = cors
	NormalizeScanValue   = normalizeScanValue
	FilterAllowedTables  = filterAllowedTables
	ParsePrefer          = parsePrefer
	ContentRangeReturned = contentRangeReturned
	ParseInsertBody      = parseInsertBody
	ParseUpdateBody      = parseUpdateBody
	ClassifyDBError      = classifyDBError
)

const ProblemTypePrefix = problemTypePrefix

const (
	PreferReturnMinimal        = preferReturnMinimal
	PreferReturnRepresentation = preferReturnRepresentation
)

type PreferDirective = preferDirective

func WriteProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	typeSuffix, title, detail string,
) {
	writeProblem(w, r, status, typeSuffix, title, detail)
}
