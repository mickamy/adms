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
	RowPKColumn  string
	RowPK        string
}

// SinglePKColumn returns the lone PK column or "" if the table has a
// composite PK or no PK at all. The row-detail UI hides write actions in
// that case because PostgREST-style URLs cannot encode multi-column
// equality without disambiguation.
func (s *Server) singlePKColumn(t *schema.Table) string {
	if len(t.PrimaryKey) != 1 {
		return ""
	}

	return t.PrimaryKey[0]
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
	t := s.findTable(r.PathValue("table"))
	if t == nil {
		http.NotFound(w, r)

		return
	}

	s.renderLayout(w, r, layoutData{
		Title:        t.Name + " — adms",
		ActiveTable:  t.Name,
		ContentTmpl:  "content_table.html",
		ContentTable: t,
		RowPKColumn:  s.singlePKColumn(t),
	})
}

func (s *Server) newRow(w http.ResponseWriter, r *http.Request) {
	t := s.findTable(r.PathValue("table"))
	if t == nil {
		http.NotFound(w, r)

		return
	}

	s.renderLayout(w, r, layoutData{
		Title:        "New row · " + t.Name,
		ActiveTable:  t.Name,
		ContentTmpl:  "content_new.html",
		ContentTable: t,
	})
}

func (s *Server) rowView(w http.ResponseWriter, r *http.Request) {
	t := s.findTable(r.PathValue("table"))
	if t == nil {
		http.NotFound(w, r)

		return
	}

	pkCol := s.singlePKColumn(t)
	if pkCol == "" {
		s.renderLayout(w, r, layoutData{
			Title:        t.Name + " — adms",
			ActiveTable:  t.Name,
			ContentTmpl:  "content_row_unsupported.html",
			ContentTable: t,
		})

		return
	}

	s.renderLayout(w, r, layoutData{
		Title:        t.Name + " · " + r.PathValue("pk"),
		ActiveTable:  t.Name,
		ContentTmpl:  "content_row.html",
		ContentTable: t,
		RowPKColumn:  pkCol,
		RowPK:        r.PathValue("pk"),
	})
}

func (s *Server) findTable(name string) *schema.Table {
	for i := range s.schema.Tables {
		if s.schema.Tables[i].Name == name {
			return &s.schema.Tables[i]
		}
	}

	return nil
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
