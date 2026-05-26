package ui_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/ui"
)

const apiOrigin = "http://localhost:7777"

func sampleSchema() schema.Schema {
	return schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "users",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "name", Type: "text"},
				},
				PrimaryKey: []string{"id"},
				ReferencedBy: []schema.ForeignKey{
					{Table: "public.posts", Columns: []string{"user_id"}, References: []string{"id"}},
				},
			},
			{
				Schema: "public",
				Name:   "posts",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "user_id", Type: "bigint"},
					{Name: "title", Type: "text"},
				},
				PrimaryKey: []string{"id"},
				ForeignKeys: []schema.ForeignKey{
					{Table: "public.users", Columns: []string{"user_id"}, References: []string{"id"}},
				},
			},
		},
	}
}

func newTestUIServer(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	srv, err := ui.New(config.Config{UI: config.UIConfig{Listen: ":0"}}, sch, apiOrigin)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts
}

func TestIndexRendersSidebar(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`<meta name="adms-api-origin" content="http://localhost:7777">`,
		`href="/t/users"`,
		`href="/t/posts"`,
		// Tailwind is served locally now, not from the Play CDN.
		`<link rel="stylesheet" href="/static/css/tailwind.css">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
	}

	// Hard guard against the CDN script crawling back in.
	if strings.Contains(body, "cdn.tailwindcss.com") {
		t.Errorf("layout still references the Tailwind Play CDN; UI must be served from embed.FS")
	}
}

func TestLayoutCarriesA11yLandmarks(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	// `users` is the active table so the sidebar entry should advertise it.
	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// Skip-to-content link for keyboard users.
		`href="#main-content"`,
		`Skip to content`,
		// <main> carries the matching id and is focusable from the skip link.
		`<main id="main-content" tabindex="-1"`,
		// Sidebar search has a real <label> rather than relying on placeholder.
		`<label for="table-filter" class="sr-only">Filter tables</label>`,
		`id="table-filter"`,
		// The nav landmark carries the accessible name; aside stays
		// unlabelled to avoid the duplicate "Tables, complementary,
		// Tables, navigation" announcement that screen readers would
		// otherwise produce.
		`<nav aria-label="Tables">`,
		// Active table entry must announce itself as the current page.
		`aria-current="page"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layout a11y landmark missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestTableViewCarriesA11yAttributes(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// tbody announces row reloads to screen readers. Initial busy
		// state is true so the static "Loading…" markup before the IIFE
		// runs is correctly described while data is still pending.
		`<tbody id="rows" aria-live="polite" aria-busy="true"`,
		// Modal title is referenced from the dialog for accessible name.
		`<dialog id="edit-modal" aria-labelledby="edit-modal-title"`,
		`<h2 id="edit-modal-title"`,
		// Modal status span uses role=status / aria-live.
		`id="edit-status" role="status" aria-live="polite"`,
		// FK arrow link in renderRows builds an aria-label so the bare "→"
		// has a screen-reader-accessible name.
		`aria-label="${label}"`,
		`const label = ` + "`" + `Open ${escapeHTML(ref.table)} row ${escapeHTML(String(v))}` + "`" + `;`,
		// Load / error states announce themselves on the tbody.
		`tbody.setAttribute('aria-busy', 'true')`,
		`tbody.setAttribute('aria-busy', 'false')`,
		`role="alert"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("table view a11y attribute missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestTableViewLoadsSkeletonRows(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// Helper function + the JS that puts the skeleton on screen.
		`function skeletonRows(count)`,
		// Row count is derived from the user's limit input, clamped to
		// a sane ceiling so a `limit=1000` page does not paint a sea of
		// placeholders.
		`form.elements.limit.value`,
		`tbody.innerHTML = skeletonRows(Math.min(limit, 10));`,
		// motion-safe gates the shimmer on prefers-reduced-motion=no-preference,
		// so the bundled CSS must include the qualifier output.
		`motion-safe:animate-pulse`,
		// aria-hidden on each row keeps screen readers from announcing
		// the placeholder while aria-busy is true.
		`aria-hidden="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("table view skeleton wiring missing %q\n---body---\n%s", want, body)
		}
	}

	cssResp := httpGet(t, ts.URL+"/static/css/tailwind.css")
	defer func() { _ = cssResp.Body.Close() }()

	cssBody := readAll(t, cssResp)
	for _, want := range []string{
		// The reduced-motion media query and the pulse keyframe both
		// land in the bundled CSS, so the shimmer only animates for
		// users who have not opted out.
		`@media (prefers-reduced-motion:no-preference)`,
		`@keyframes pulse`,
		`.motion-safe\:animate-pulse`,
	} {
		if !strings.Contains(cssBody, want) {
			t.Errorf("tailwind bundle missing %q for reduced-motion-safe skeleton", want)
		}
	}
}

func TestRowViewStatusSpanIsAnnounced(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users/r/1")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	if !strings.Contains(body, `id="status" role="status" aria-live="polite"`) {
		t.Errorf("row view status span must carry role/aria-live\n---body---\n%s", body)
	}
}

func TestStaticTailwindCSSIsServed(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/static/css/tailwind.css")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css; …", ct)
	}

	body := readAll(t, resp)
	// Sanity check the file actually contains generated Tailwind utility
	// rules. `.flex` is referenced from every form-bearing template and
	// is therefore guaranteed to survive tree-shaking — using it instead
	// of an incidental comment substring keeps the check stable across
	// CLI versions that might strip or rewrite the MIT banner.
	if !strings.Contains(body, ".flex") || len(body) < 5*1024 {
		t.Errorf("tailwind.css looks empty / stubbed: %d bytes", len(body))
	}
}

func TestTableViewRenders(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`data-col="id"`,
		`data-col="name"`,
		"PK: id",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestTableViewIncludesActions(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`href="/t/users/new"`,
		`href="/t/users/schema"`,
		`>actions<`,
		`const pkColumn = "id"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestSchemaViewRendersTableMetadata(t *testing.T) {
	t.Parallel()

	scoreDefault := "0"
	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "users",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint", IsIdentity: true},
					{Name: "email", Type: "text", Comment: "primary contact"},
					{Name: "name", Type: "text", Nullable: true},
					{Name: "score", Type: "numeric", Default: &scoreDefault},
				},
				PrimaryKey: []string{"id"},
				ReferencedBy: []schema.ForeignKey{
					{Table: "public.posts", Columns: []string{"user_id"}, References: []string{"id"}},
				},
				Indexes: []schema.Index{
					{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
					{
						Name: "users_email_idx", Columns: []string{"email"},
						Unique: true, Method: "btree", Where: "deleted_at IS NULL",
					},
				},
			},
			{
				Schema: "public",
				Name:   "posts",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "user_id", Type: "bigint"},
				},
				PrimaryKey: []string{"id"},
				ForeignKeys: []schema.ForeignKey{
					{Table: "public.users", Columns: []string{"user_id"}, References: []string{"id"}},
				},
			},
		},
	}

	ts := newTestUIServer(t, sch)

	resp := httpGet(t, ts.URL+"/t/users/schema")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// page header
		`<title>Schema · users</title>`,
		`← Data`,
		// columns table headers
		`>name<`, `>type<`, `>nullable<`, `>default<`, `>notes<`,
		// rows
		`>id</td>`,
		`>email</td>`,
		`>bigint</td>`,
		`>numeric</td>`,
		// nullable mapping
		`>yes</td>`, `>no</td>`,
		// default rendered raw
		`>0</td>`,
		// notes column carries identity tag + column comment
		`>identity</span>`,
		`primary contact`,
		// PK / Referenced by / Indexes sections
		`>Primary key<`,
		`· PK: id ·`,
		`>Referenced by<`,
		// Referenced-by row: linked source table + qualified right-hand side
		`<a href="/t/posts/schema" class="text-zinc-100 hover:underline">public.posts</a>(user_id)`,
		`public.users(id)`,
		`>Indexes<`,
		`users_pkey`,
		`users_email_idx`,
		`>UNIQUE</span>`,
		`>btree</span>`,
		`>WHERE deleted_at IS NULL</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schema view missing %q\n---body---\n%s", want, body)
		}
	}

	// users has no outgoing FKs; section should be absent.
	if strings.Contains(body, `>Foreign keys<`) {
		t.Errorf("users schema view should not render an outgoing Foreign keys section")
	}

	// posts schema page should show the outgoing FK.
	pres := httpGet(t, ts.URL+"/t/posts/schema")
	defer func() { _ = pres.Body.Close() }()

	pbody := readAll(t, pres)
	for _, want := range []string{
		`>Foreign keys<`,
		// Outgoing FK row: linked target table
		`<a href="/t/users/schema" class="text-zinc-100 hover:underline">public.users</a>(id)`,
	} {
		if !strings.Contains(pbody, want) {
			t.Errorf("posts schema view missing %q", want)
		}
	}
}

func TestSchemaViewUnknownTableReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/ghost/schema")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTableViewRendersEditModal(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`<dialog id="edit-modal"`,
		`id="edit-form"`,
		`id="edit-cancel"`,
		`id="edit-delete"`,
		`id="edit-fullpage-link"`,
		`data-edit-col="id"`,
		`data-edit-col="name"`,
		`modal.showModal()`,
		`function openEditModal(`,
		`data-edit-pk="${pkVal}"`,
		`editFullpageLink.href = '/t/' + encodeURIComponent(tableName) + '/r/' + encodeURIComponent(pkVal)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("table view missing modal piece %q\n---body---\n%s", want, body)
		}
	}

	// The edit button must NOT navigate to /r/{pk} anymore — modal handles
	// it. The deep-link /t/{table}/r/{pk} still works through the sidebar
	// and the "Open full page" link, but the table row's edit action should
	// be a button, not an anchor.
	bad := `<a href="/t/${encodeURIComponent(tableName)}/r/${pkVal}"`
	if strings.Contains(body, bad) {
		t.Errorf("table view still navigates from edit action; want modal\n---body---\n%s", body)
	}
}

func TestTableViewOmitsEditModalForCompositePK(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "joinrow",
				Columns: []schema.Column{
					{Name: "a", Type: "bigint"},
					{Name: "b", Type: "bigint"},
				},
				PrimaryKey: []string{"a", "b"},
			},
		},
	}

	ts := newTestUIServer(t, sch)

	resp := httpGet(t, ts.URL+"/t/joinrow")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)
	if strings.Contains(body, `<dialog id="edit-modal"`) {
		t.Errorf("composite-PK table should not render edit modal\n---body---\n%s", body)
	}
}

func TestTableViewEmitsInlineCellEditWiring(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// Per-column kind map drives the inline editor's input choice.
		// Go's html/template JS-escapes the keys as string literals.
		`const columnKinds = {`,
		`"id": "integer"`,
		`"name": "text"`,
		// Tbody-level dblclick handler + the per-cell data attributes
		// emitted from renderRows. Asserting the JS source covers both
		// the handler wiring and the cell-render template literal.
		`tbody.addEventListener('dblclick'`,
		`td[data-col][data-row-pk]`,
		`data-col="${escapeHTML(c)}" data-row-pk="${escapeHTML(pkRaw)}"`,
		`function startCellEdit(td)`,
		`function createCellEditInput(kind)`,
		`function saveCellEdit(col, pk, value)`,
		// PK column must remain read-only — it builds the PATCH URL.
		// Read-only deployments turn this off entirely.
		`const editable = !uiReadOnly && pkRaw !== null && c !== pkColumn;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("inline cell edit wiring missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestNewRowFormRenders(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users/new")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`>Insert<`,
		`name="id"`,
		`name="name"`,
		`const tableName = "users"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestNewRowUnknownTableReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/ghost/new")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRowViewRenders(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users/r/1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)

	for _, want := range []string{
		`>Save<`,
		`>Delete<`,
		`const pkColumn = "id"`,
		`const pkValue = "1"`,
		`name="name"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestRowViewUnknownTableReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/ghost/r/1")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRowViewMultiPKShowsUnsupported(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "composite",
				Columns: []schema.Column{
					{Name: "a", Type: "bigint"},
					{Name: "b", Type: "bigint"},
				},
				PrimaryKey: []string{"a", "b"},
			},
		},
	}

	ts := newTestUIServer(t, sch)

	resp := httpGet(t, ts.URL+"/t/composite/r/anything")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body := readAll(t, resp)
	if !strings.Contains(body, "Per-row editing is not available") {
		t.Errorf("body missing unsupported notice\n---body---\n%s", body)
	}
}

func TestTableViewUnknownReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/ghost")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTableViewExposesOutgoingFKs(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	// posts.user_id → users(id) — the JS map needs to know it.
	resp := httpGet(t, ts.URL+"/t/posts")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)
	if !strings.Contains(body, `"user_id": { table: "users", column: "id" }`) {
		t.Errorf("outgoing FK map missing from table view\n---body---\n%s", body)
	}
}

func TestRowViewLinksOutgoingFKAndListsReferencedBy(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	// posts row: outgoing FK posts.user_id → users(id) should appear in JS.
	resp := httpGet(t, ts.URL+"/t/posts/r/9")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)
	if !strings.Contains(body, `"user_id": { table: "users", column: "id" }`) {
		t.Errorf("outgoing FK map missing from posts row view")
	}

	// users row: ReferencedBy posts.user_id should render as a section link.
	uresp := httpGet(t, ts.URL+"/t/users/r/1")
	defer func() { _ = uresp.Body.Close() }()

	ubody := readAll(t, uresp)

	for _, want := range []string{
		`>Referenced by<`,
		`href="/t/posts?user_id=eq.1&order=user_id.asc"`,
		`posts.user_id = 1`,
	} {
		if !strings.Contains(ubody, want) {
			t.Errorf("users row view missing %q\n---body---\n%s", want, ubody)
		}
	}
}

func TestRowViewSkipsReferencedBySection(t *testing.T) {
	// posts row has no incoming FKs in the sample schema, so the
	// "Referenced by" header should not render at all.
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/posts/r/9")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)
	if strings.Contains(body, "Referenced by") {
		t.Errorf("posts row should not render Referenced by section\n---body---\n%s", body)
	}
}

func TestBareTableName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"public.users", "users"},
		{"users", "users"},
		{"weird.dotted.name", "name"},
		{"", ""},
	}

	for _, tc := range cases {
		if got := ui.BareTableName(tc.in); got != tc.want {
			t.Errorf("bareTableName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInputKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ, want string
	}{
		// boolean
		{"boolean", "boolean"},
		{"BOOLEAN", "boolean"},
		{"bool", "boolean"},
		{"tinyint(1)", "boolean"},
		{"TINYINT(1) UNSIGNED", "boolean"},
		// integer — Postgres
		{"smallint", "integer"},
		{"integer", "integer"},
		{"bigint", "integer"},
		{"smallserial", "integer"},
		{"serial", "integer"},
		{"bigserial", "integer"},
		// integer — MySQL
		{"tinyint", "integer"},
		{"tinyint(4)", "integer"},
		{"mediumint", "integer"},
		{"int(11)", "integer"},
		{"bigint(20)", "integer"},
		// number (decimal / float)
		{"numeric", "number"},
		{"numeric(10,2)", "number"},
		{"real", "number"},
		{"double precision", "number"},
		{"decimal(10,2)", "number"},
		{"float", "number"},
		{"double", "number"},
		// date
		{"date", "date"},
		// json (incl. Postgres arrays)
		{"json", "json"},
		{"jsonb", "json"},
		{"text[]", "json"},
		{"integer[]", "json"},
		{"numeric(10,2)[]", "json"},
		// text fallback (varchar, timestamp, uuid, …)
		{"text", "text"},
		{"character varying", "text"},
		{"character varying(255)", "text"},
		{"varchar(255)", "text"},
		{"timestamp with time zone", "text"},
		{"timestamp without time zone", "text"},
		{"datetime", "text"},
		{"uuid", "text"},
		{"", "text"},
	}

	for _, tc := range cases {
		got := ui.InputKind(schema.Column{Type: tc.typ})
		if got != tc.want {
			t.Errorf("inputKind(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

func TestFilterHint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ      string
		contains string
	}{
		{"boolean", "is.null"},
		{"tinyint(1)", "eq.true"},
		{"bigint", "gt.0"},
		{"integer", "in.(1,2,3)"},
		{"numeric", "gt.0"},
		{"double precision", "lt.100"},
		{"date", "gte.2026"},
		{"jsonb", "cs."},
		{"text[]", "cd."},
		{"text", "like.*foo*"},
		{"timestamp with time zone", "ilike."},
	}

	for _, tc := range cases {
		got := ui.FilterHint(schema.Column{Type: tc.typ})
		if !strings.Contains(got, tc.contains) {
			t.Errorf("filterHint(%q) = %q, want substring %q", tc.typ, got, tc.contains)
		}
	}
}

func TestTableViewFilterPlaceholdersAreKindAware(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, typedSchema())

	resp := httpGet(t, ts.URL+"/t/alltypes")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		// boolean column "active" → boolean hint
		`name="active" data-filter-kind="boolean" placeholder="true, eq.true, is.null"`,
		// integer column "id" → integer hint
		`name="id" data-filter-kind="integer" placeholder="10, gt.0, lt.100, in.(1,2,3)"`,
		// json column "meta" / array column "tags" → json hint
		`name="meta" data-filter-kind="json" placeholder="[&#34;a&#34;], cs.[...], cd.[...], is.null"`,
		`name="tags" data-filter-kind="json" placeholder="[&#34;a&#34;], cs.[...], cd.[...], is.null"`,
		// date column "born" → date hint
		`name="born" data-filter-kind="date" placeholder="2026-01-01, gte.2026-01-01"`,
		// number column "score" → number hint
		`name="score" data-filter-kind="number" placeholder="10.5, gt.0, lt.100"`,
		// text column "name" → text hint
		`name="name" data-filter-kind="text" placeholder="foo, like.*foo*, ilike.*foo*"`,
		// JS picks up the same kind to auto-prefix the kind's default
		// operator (cs for json, eq for everything else) when the user
		// did not operator-prefix the value.
		`const operatorPrefix = /^(not\.)?(eq|neq|gt|gte|lt|lte|like|ilike|in|is|cs|cd)\./;`,
		`value = (kind === 'json' ? 'cs.' : 'eq.') + value;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("filter placeholder missing %q\n---body---\n%s", want, body)
		}
	}
}

func typedSchema() schema.Schema {
	return schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "alltypes",
				Columns: []schema.Column{
					{Name: "id", Type: "bigint"},
					{Name: "name", Type: "text"},
					{Name: "active", Type: "boolean"},
					{Name: "born", Type: "date"},
					{Name: "meta", Type: "jsonb"},
					{Name: "score", Type: "numeric"},
					{Name: "tags", Type: "text[]"},
				},
				PrimaryKey: []string{"id"},
			},
		},
	}
}

func TestRowFormRendersTypedInputs(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, typedSchema())

	resp := httpGet(t, ts.URL+"/t/alltypes/r/1")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`data-col="id" data-kind="integer" name="id" type="number" step="1"`,
		`data-col="name" data-kind="text" name="name" type="text"`,
		`<select data-col="active" data-kind="boolean" name="active"`,
		`<option value="true">true</option>`,
		`<option value="false">false</option>`,
		`data-col="born" data-kind="date" name="born" type="date"`,
		`<textarea data-col="meta" data-kind="json" name="meta"`,
		`data-col="score" data-kind="number" name="score" type="number" step="any"`,
		`<textarea data-col="tags" data-kind="json" name="tags"`,
		// helpers live in layout.html now and are addressed via the
		// `adms` prefix; assert their presence in the rendered page.
		`function admsParseValue(input)`,
		`function admsSetInputValue(input, v)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("row form missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestNewFormRendersTypedInputs(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, typedSchema())

	resp := httpGet(t, ts.URL+"/t/alltypes/new")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`data-col="id" data-kind="integer" name="id" type="number" step="1"`,
		`<select data-col="active" data-kind="boolean" name="active"`,
		`data-col="born" data-kind="date" name="born" type="date"`,
		`<textarea data-col="meta" data-kind="json" name="meta"`,
		`data-col="score" data-kind="number" name="score" type="number" step="any"`,
		`<textarea data-col="tags" data-kind="json" name="tags"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new form missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestEditModalRendersTypedInputs(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, typedSchema())

	resp := httpGet(t, ts.URL+"/t/alltypes")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`data-edit-col="id" data-kind="integer" name="id" type="number" step="1"`,
		`<select data-edit-col="active" data-kind="boolean" name="active"`,
		`data-edit-col="born" data-kind="date" name="born" type="date"`,
		`<textarea data-edit-col="meta" data-kind="json" name="meta"`,
		`data-edit-col="score" data-kind="number" name="score" type="number" step="any"`,
		`<textarea data-edit-col="tags" data-kind="json" name="tags"`,
		`function admsParseValue(input)`,
		`function admsSetInputValue(input, v)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit modal missing %q\n---body---\n%s", want, body)
		}
	}
}

func TestOutgoingFKsSkipsCompositeFKs(t *testing.T) {
	t.Parallel()

	tbl := &schema.Table{
		Columns:    []schema.Column{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		PrimaryKey: []string{"a"},
		ForeignKeys: []schema.ForeignKey{
			{Table: "public.x", Columns: []string{"a"}, References: []string{"id"}},
			{Table: "public.y", Columns: []string{"b", "c"}, References: []string{"k1", "k2"}},
		},
	}

	got := ui.OutgoingFKs(tbl)
	if len(got) != 1 || got["a"].Table != "x" {
		t.Errorf("outgoingFKs = %+v, want single entry for column a → x", got)
	}
}

func TestReferencedByListSkipsCompositeFKsAndPreservesOrder(t *testing.T) {
	t.Parallel()

	tbl := &schema.Table{
		ReferencedBy: []schema.ForeignKey{
			{Table: "public.a", Columns: []string{"x"}, References: []string{"id"}},
			{Table: "public.b", Columns: []string{"y", "z"}, References: []string{"k1", "k2"}},
			{Table: "public.c", Columns: []string{"w"}, References: []string{"id"}},
		},
	}

	got := ui.ReferencedByList(tbl)
	want := []ui.FKRef{
		{Table: "a", Column: "x"},
		{Table: "c", Column: "w"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("referencedByList = %+v, want %+v", got, want)
	}
}

func TestRowViewRendersMultipleReferencedByEntries(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema:     "public",
				Name:       "users",
				Columns:    []schema.Column{{Name: "id", Type: "bigint"}},
				PrimaryKey: []string{"id"},
				ReferencedBy: []schema.ForeignKey{
					{Table: "public.posts", Columns: []string{"author_id"}, References: []string{"id"}},
					{Table: "public.comments", Columns: []string{"user_id"}, References: []string{"id"}},
				},
			},
			{
				Schema:     "public",
				Name:       "posts",
				Columns:    []schema.Column{{Name: "id", Type: "bigint"}},
				PrimaryKey: []string{"id"},
			},
			{
				Schema:     "public",
				Name:       "comments",
				Columns:    []schema.Column{{Name: "id", Type: "bigint"}},
				PrimaryKey: []string{"id"},
			},
		},
	}

	ts := newTestUIServer(t, sch)

	resp := httpGet(t, ts.URL+"/t/users/r/42")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`href="/t/posts?author_id=eq.42&order=author_id.asc"`,
		`posts.author_id = 42`,
		`href="/t/comments?user_id=eq.42&order=user_id.asc"`,
		`comments.user_id = 42`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("users row view missing %q\n---body---\n%s", want, body)
		}
	}

	postsIdx := strings.Index(body, "posts.author_id = 42")
	commentsIdx := strings.Index(body, "comments.user_id = 42")
	if postsIdx < 0 || commentsIdx < 0 || postsIdx >= commentsIdx {
		t.Errorf("expected posts.author_id before comments.user_id; postsIdx=%d commentsIdx=%d", postsIdx, commentsIdx)
	}
}

func newTestUIServerWithToken(t *testing.T, sch schema.Schema, token string) *httptest.Server {
	t.Helper()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: ":0"}, AuthToken: token},
		sch, apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts
}

func newTestUIServerReadOnly(t *testing.T, sch schema.Schema) *httptest.Server {
	t.Helper()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: ":0"}, ReadOnly: true},
		sch, apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return ts
}

func TestTableViewReadOnlyHidesWriteAffordances(t *testing.T) {
	t.Parallel()

	ts := newTestUIServerReadOnly(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, forbidden := range []string{
		`href="/t/users/new"`,
		`<dialog id="edit-modal"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("read-only table view should not emit %q\n---body---\n%s", forbidden, body)
		}
	}

	// Match the whole `<th>actions</th>` shape so a future whitespace /
	// attribute tweak inside the cell does not silently let the column
	// header slip back into the read-only build.
	if regexp.MustCompile(`<th[^>]*>\s*actions\s*</th>`).MatchString(body) {
		t.Errorf("read-only table view should not render an actions column header\n---body---\n%s", body)
	}

	// Whitespace tolerance: html/template's JS escaper pads booleans
	// with surrounding spaces and that's an implementation detail we
	// don't want the test pinned to.
	if !regexp.MustCompile(`const uiReadOnly\s*=\s*true\b`).MatchString(body) {
		t.Errorf("read-only table view should expose a truthy uiReadOnly to the IIFE\n---body---\n%s", body)
	}
}

func TestTableViewCompositePKAndReadOnlyBothGateOut(t *testing.T) {
	// Both gates of `{{if and $.RowPKColumn (not $.ReadOnly)}}` should
	// hold independently; this test exercises the case where both are
	// false so a regression that drops one of them would still be
	// caught by a single inspection.
	t.Parallel()

	sch := schema.Schema{
		Tables: []schema.Table{
			{
				Schema: "public",
				Name:   "joinrow",
				Columns: []schema.Column{
					{Name: "a", Type: "bigint"},
					{Name: "b", Type: "bigint"},
				},
				PrimaryKey: []string{"a", "b"},
			},
		},
	}

	ts := newTestUIServerReadOnly(t, sch)

	resp := httpGet(t, ts.URL+"/t/joinrow")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, forbidden := range []string{
		`<dialog id="edit-modal"`,
		`href="/t/joinrow/new"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("composite-PK + read-only table view should not emit %q\n---body---\n%s", forbidden, body)
		}
	}

	if regexp.MustCompile(`<th[^>]*>\s*actions\s*</th>`).MatchString(body) {
		t.Errorf("composite-PK + read-only table view should not render an actions column header\n---body---\n%s", body)
	}
}

func TestRowViewReadOnlyDisablesForm(t *testing.T) {
	t.Parallel()

	ts := newTestUIServerReadOnly(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users/r/1")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`<fieldset disabled`,
		`Read-only mode · writes disabled`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("read-only row view missing %q\n---body---\n%s", want, body)
		}
	}

	if !regexp.MustCompile(`const uiReadOnly\s*=\s*true\b`).MatchString(body) {
		t.Errorf("read-only row view should expose a truthy uiReadOnly to the IIFE\n---body---\n%s", body)
	}

	// preventDefault has to run before the uiReadOnly gate so an Enter
	// keystroke inside an input does not trigger the browser's default
	// GET submission. Lock the order in so a regression cannot slip the
	// preventDefault back inside `if (!uiReadOnly) { ... }`.
	preventDefaultRe := regexp.MustCompile(
		`(?s)form\.addEventListener\('submit'.*?e\.preventDefault\(\);\s*if\s*\(uiReadOnly\)\s*return;`,
	)
	if !preventDefaultRe.MatchString(body) {
		t.Errorf("row form submit handler must call e.preventDefault() unconditionally before the uiReadOnly gate")
	}

	for _, forbidden := range []string{
		`type="submit"`,
		`id="delete"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("read-only row view should not emit %q\n---body---\n%s", forbidden, body)
		}
	}
}

func TestNewRowInReadOnlyReturns404(t *testing.T) {
	t.Parallel()

	ts := newTestUIServerReadOnly(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/t/users/new")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestLayoutOmitsAuthMetaWhenNoToken(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)
	if strings.Contains(body, `<meta name="adms-auth-token"`) {
		t.Errorf("layout should omit adms-auth-token meta when no token configured\n---body---\n%s", body)
	}
}

func TestLayoutEmitsAuthMetaAndFetchWrapperWhenTokenSet(t *testing.T) {
	t.Parallel()

	ts := newTestUIServerWithToken(t, sampleSchema(), "sekret-1234")

	resp := httpGet(t, ts.URL+"/")
	defer func() { _ = resp.Body.Close() }()

	body := readAll(t, resp)

	for _, want := range []string{
		`<meta name="adms-auth-token" content="sekret-1234">`,
		`'Bearer ' + token`,
		`headers.set('Authorization'`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layout missing %q with auth token configured\n---body---\n%s", want, body)
		}
	}

	// The wrapper must run before content <script>s that fire their
	// initial fetch synchronously, so it has to live inside <head>.
	headEnd := strings.Index(body, "</head>")
	wrapperIdx := strings.Index(body, "headers.set('Authorization'")
	if headEnd < 0 || wrapperIdx < 0 || wrapperIdx >= headEnd {
		t.Errorf("fetch wrapper must appear inside <head>; headEnd=%d wrapperIdx=%d", headEnd, wrapperIdx)
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	ts := newTestUIServer(t, sampleSchema())

	resp := httpGet(t, ts.URL+"/__healthz")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if got := readAll(t, resp); got != "ok\n" {
		t.Errorf("body = %q, want %q", got, "ok\n")
	}
}

func TestNewRequiresAPIOrigin(t *testing.T) {
	t.Parallel()

	_, err := ui.New(config.Config{UI: config.UIConfig{Listen: ":0"}}, sampleSchema(), "")
	if err == nil {
		t.Fatal("ui.New error = nil, want non-nil for empty apiOrigin")
	}
}

func TestRunBindsAndShutsDown(t *testing.T) {
	t.Parallel()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: "127.0.0.1:0"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Run(ctx) }()

	// Give Run a moment to bind before we cancel; without this the listener
	// may not have entered Serve yet and we would not be testing the
	// graceful-shutdown path.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}
}

func TestRunListenFailure(t *testing.T) {
	t.Parallel()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: "127.0.0.1:99999"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run() error = nil, want non-nil for invalid addr")
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	srv, err := ui.New(
		config.Config{UI: config.UIConfig{Listen: ":0"}},
		sampleSchema(),
		apiOrigin,
	)
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	waitForUI(t, addr, 2*time.Second)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return after context cancel")
	}
}

func waitForUI(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		probeCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)

		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+addr+"/__healthz", nil)
		resp, err := http.DefaultClient.Do(req)

		cancel()

		if err == nil {
			_ = resp.Body.Close()

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("UI server did not start within %s: last err=%v", timeout, err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return resp
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return string(b)
}
