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
	// cors sits outside authBearer so OPTIONS preflight, which carries no
	// Authorization header, can be answered without tripping the auth gate.
	return logging(s.logger,
		recoverer(s.logger,
			cors(s.corsOrigins,
				authBearer(s.logger, s.authToken, mux))))
}
