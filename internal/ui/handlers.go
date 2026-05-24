package ui

import (
	"io"
	"net/http"

	"github.com/mickamy/adms/internal/logger"
	"github.com/mickamy/adms/internal/schema"
)

type layoutData struct {
	APIOrigin    string
	ReadOnly     bool
	Tables       []schema.Table
	ActiveTable  string
	Title        string
	ContentTmpl  string
	ContentTable *schema.Table
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.renderLayout(w, r, layoutData{
		Title:       "adms",
		ContentTmpl: "content_index.html",
	})
}

func (s *Server) tableView(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("table")

	var found *schema.Table

	for i := range s.schema.Tables {
		if s.schema.Tables[i].Name == name {
			found = &s.schema.Tables[i]

			break
		}
	}

	if found == nil {
		http.NotFound(w, r)

		return
	}

	s.renderLayout(w, r, layoutData{
		Title:        name + " — adms",
		ActiveTable:  name,
		ContentTmpl:  "content_table.html",
		ContentTable: found,
	})
}

func (s *Server) renderLayout(w http.ResponseWriter, r *http.Request, data layoutData) {
	data.APIOrigin = s.apiOrigin
	data.ReadOnly = s.readOnly
	data.Tables = s.schema.Tables

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := s.tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		logger.Error(r.Context(), "ui render",
			"path", r.URL.EscapedPath(), "err", err.Error())
	}
}
