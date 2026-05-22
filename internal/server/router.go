package server

import "net/http"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.schemaDump)

	logger := s.logger()

	// logging wraps recoverer so a panic still produces an access-log line
	// (recoverer writes the 500 to the statusRecorder injected by logging).
	return logging(logger, recoverer(logger, mux))
}
