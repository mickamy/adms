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

func InputKind(c schema.Column) string {
	return inputKind(c)
}

func FilterHint(c schema.Column) string {
	return filterHint(c)
}

func IsReservedFilterName(name string) bool {
	return isReservedFilterName(name)
}

type FKRef = fkRef
