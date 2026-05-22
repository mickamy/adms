package server

import "net/http"

func (s *Server) Routes() http.Handler { return s.routes() }

var (
	Recoverer = recoverer
)
