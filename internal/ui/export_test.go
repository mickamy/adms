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

type (
	ERDView   = erdView
	ERDColumn = erdColumn
)

func BuildERD(tables []schema.Table) ERDView { return buildERD(tables) }

func KeyColumns(t schema.Table) []ERDColumn { return keyColumns(t) }

// BorderPoint exposes the border-clipping geometry with plain coordinates
// so the test does not need to construct an unexported node.
func BorderPoint(x, y, w, h, tx, ty float64) (float64, float64) {
	return borderPoint(erdNode{X: x, Y: y, W: w, H: h}, tx, ty)
}
