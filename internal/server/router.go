package server

import "net/http"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.schemaDump)
	mux.HandleFunc("GET /{table}", s.read)

	// logging wraps recoverer so panics still produce an access-log line.
	return logging(s.logger, recoverer(s.logger, mux))
}
