package server

import "net/http"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.schemaDump)
	mux.HandleFunc("GET /{table}", s.read)
	mux.HandleFunc("POST /{table}", s.insert)
	mux.HandleFunc("PATCH /{table}", s.update)
	mux.HandleFunc("DELETE /{table}", s.delete)

	// logging wraps recoverer so panics still produce an access-log line.
	// auth sits inside both so 401 responses are logged and panics in the auth
	// middleware itself are still caught.
	return logging(s.logger, recoverer(s.logger, authBearer(s.logger, s.authToken, mux)))
}
