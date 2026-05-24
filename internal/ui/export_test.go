package ui

import (
	"context"
	"net"
	"net/http"
)

func (s *Server) Routes() http.Handler { return s.routes() }

func (s *Server) Serve(ctx context.Context, ln net.Listener) error { return s.serve(ctx, ln) }
