package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) schemaDump(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(s.Schema); err != nil {
		// Response headers are already sent; just log and let the connection drop.
		fmt.Fprintf(s.Logger, "adms: encode schema: %v\n", err)
	}
}
