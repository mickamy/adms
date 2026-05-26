package ui

import (
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/mickamy/adms/internal/logger"
	"github.com/mickamy/adms/internal/schema"
)

type layoutData struct {
	APIOrigin    string
	AuthToken    string
	ReadOnly     bool
	Tables       []schema.Table
	ActiveTable  string
	Title        string
	ContentTmpl  string
	ContentTable *schema.Table
	RowPKColumn  string
	RowPK        string
	OutgoingFKs  map[string]fkRef
	ReferencedBy []fkRef
}

// fkRef is the half of a single-column foreign key the UI cares about:
// the other table's bare name and the column inside it. Used both for
// outgoing FKs (this column → otherTable.column) and incoming FKs
// (otherTable.column → this row), so the templates can build links
// without re-walking the schema.
type fkRef struct {
	Column string
	Table  string
}

// bareTableName strips the "schema." prefix that introspection attaches to
// referenced tables, since UI URLs are keyed by the bare name only.
func bareTableName(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[i+1:]
	}

	return qualified
}

// outgoingFKs returns a map from local-column-name → referenced table/column
// for single-column FKs. Composite FKs are dropped because the UI cannot
// encode them in a single URL hop.
func outgoingFKs(t *schema.Table) map[string]fkRef {
	out := make(map[string]fkRef, len(t.ForeignKeys))

	for _, fk := range t.ForeignKeys {
		if len(fk.Columns) != 1 || len(fk.References) != 1 {
			continue
		}

		out[fk.Columns[0]] = fkRef{
			Table:  bareTableName(fk.Table),
			Column: fk.References[0],
		}
	}

	return out
}

// referencedByList returns each single-column incoming FK as a remote
// table/column pair so the row detail can link to filtered lists of
// children rows.
func referencedByList(t *schema.Table) []fkRef {
	out := make([]fkRef, 0, len(t.ReferencedBy))

	for _, fk := range t.ReferencedBy {
		if len(fk.Columns) != 1 || len(fk.References) != 1 {
			continue
		}

		out = append(out, fkRef{
			Table:  bareTableName(fk.Table),
			Column: fk.Columns[0],
		})
	}

	return out
}

// inputKind classifies a column for the form UI so the right HTML input
// can be rendered and the JS value parser can dispatch on it. Returns
// one of "boolean", "integer", "number", "date", "json", "text". MySQL
// boolean is recognized only as the conventional tinyint(1). Postgres
// array types ("text[]", "integer[]", ...) map to "json" so users can
// enter JSON array literals that PostgREST will convert. Timestamp and
// datetime types fall through to "text" because HTML datetime-local
// loses the timezone information Postgres timestamptz carries.
func inputKind(c schema.Column) string {
	raw := strings.ToLower(c.Type)
	if strings.HasPrefix(raw, "tinyint(1)") {
		return "boolean"
	}

	if strings.HasSuffix(raw, "[]") {
		return "json"
	}

	bare := raw
	if i := strings.Index(bare, "("); i >= 0 {
		bare = strings.TrimSpace(bare[:i])
	}

	switch bare {
	case "boolean", "bool":
		return "boolean"
	case "smallint", "integer", "int", "bigint",
		"tinyint", "mediumint",
		"smallserial", "serial", "bigserial":
		return "integer"
	case "numeric", "decimal", "real", "double precision", "double", "float":
		return "number"
	case "date":
		return "date"
	case "json", "jsonb":
		return "json"
	}

	return "text"
}

// filterHint returns a placeholder hint for the table-view filter input
// for a column. The first form is the bare value; the table-view JS
// auto-prefixes it with the kind-default operator (cs for json, eq for
// the rest) so users can search without typing the prefix. Subsequent
// forms show the explicit PostgREST operators that override the default.
func filterHint(c schema.Column) string {
	switch inputKind(c) {
	case "boolean":
		return "true, eq.true, is.null"
	case "integer":
		return "10, gt.0, lt.100, in.(1,2,3)"
	case "number":
		return "10.5, gt.0, lt.100"
	case "date":
		return "2026-01-01, gte.2026-01-01"
	case "json":
		return `["a"], cs.[...], cd.[...], is.null`
	}

	return "foo, like.*foo*, ilike.*foo*"
}

// reservedFilterNamesList is the canonical set of query-string keys the
// adms parser owns for pagination / projection / grouping (see
// internal/query/parser.go). Exposed as a template function so the
// table-view JS RESERVED_KEYS Set is rendered from the same list,
// avoiding a Go / JS drift. Callers must treat the returned slice as
// immutable.
var reservedFilterNamesList = []string{"select", "order", "limit", "offset", "and", "or"}

func reservedFilterNames() []string { return reservedFilterNamesList }

// isReservedFilterName reports whether a column name collides with an
// adms query-string key. A table whose column is named one of these
// cannot be filtered through the API, so the table-view template skips
// the filter input for it and the JS buildURL guard skips the matching
// URL parameter.
func isReservedFilterName(name string) bool {
	return slices.Contains(reservedFilterNamesList, name)
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
		OutgoingFKs:  outgoingFKs(t),
	})
}

func (s *Server) newRow(w http.ResponseWriter, r *http.Request) {
	// The new-row form is purely a write affordance; in read-only mode
	// the API would 403 on POST anyway, so serve the page itself as 404
	// to keep the UI honest about what is possible.
	if s.readOnly {
		http.NotFound(w, r)

		return
	}

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

func (s *Server) schemaView(w http.ResponseWriter, r *http.Request) {
	t := s.findTable(r.PathValue("table"))
	if t == nil {
		http.NotFound(w, r)

		return
	}

	s.renderLayout(w, r, layoutData{
		Title:        "Schema · " + t.Name,
		ActiveTable:  t.Name,
		ContentTmpl:  "content_schema.html",
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
		OutgoingFKs:  outgoingFKs(t),
		ReferencedBy: referencedByList(t),
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
	data.AuthToken = s.authToken
	data.ReadOnly = s.readOnly
	data.Tables = s.schema.Tables

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := s.tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		logger.Error(r.Context(), "ui render",
			"path", r.URL.EscapedPath(), "err", err.Error())
	}
}
