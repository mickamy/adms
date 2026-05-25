package ui

import (
	"context"
	"net"
	"net/http"

	"github.com/mickamy/adms/internal/schema"
)

func (s *Server) Routes() http.Handler { return s.routes() }

func (s *Server) Serve(ctx context.Context, ln net.Listener) error { return s.serve(ctx, ln) }

func BareTableName(s string) string { return bareTableName(s) }

func OutgoingFKs(t *schema.Table) map[string]FKRef {
	return outgoingFKs(t)
}

func ReferencedByList(t *schema.Table) []FKRef {
	return referencedByList(t)
}

type FKRef = fkRef
