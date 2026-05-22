package server

import "net/http"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.schemaDump)

	logger := s.logger()

	// logging wraps recoverer so panics still produce an access-log line.
	return logging(logger, recoverer(logger, mux))
}
