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
	body, err := json.MarshalIndent(s.Schema, "", "  ")
	if err != nil {
		fmt.Fprintf(s.logger(), "adms: encode schema: %v\n", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
