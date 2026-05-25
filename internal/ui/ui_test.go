package ui_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
		}
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
		`>actions<`,
		`const pkColumn = "id"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---body---\n%s", want, body)
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
