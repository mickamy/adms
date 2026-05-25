//go:build integration

package schema_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mickamy/adms/internal/schema"
)

func pgTestDSN() string {
	if v := os.Getenv("ADMS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}

	return "postgres://postgres:postgres@localhost:5433/adms_test?sslmode=disable"
}

func TestPostgresIntrospect(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := t.Context()
	if err := conn.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	stmts := []string{
		`DROP TABLE IF EXISTS introspect_posts, introspect_users CASCADE`,
		`CREATE TABLE introspect_users (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE introspect_posts (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES introspect_users(id),
			title TEXT NOT NULL
		)`,
		`COMMENT ON COLUMN introspect_users.email IS 'optional email address'`,
		`CREATE UNIQUE INDEX introspect_users_email_idx ON introspect_users(email)`,
		`CREATE INDEX introspect_posts_user_title_idx ON introspect_posts(user_id, title)`,
		`CREATE INDEX introspect_users_active_idx ON introspect_users(name) WHERE email IS NOT NULL`,
	}

	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		_, _ = conn.Exec(`DROP TABLE IF EXISTS introspect_posts, introspect_users CASCADE`)
	})

	got, err := schema.PostgresIntrospector().Introspect(ctx, conn, []string{"public"})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}

	users, posts := findTables(t, got, "introspect_users", "introspect_posts")

	assertPK(t, "users", users.PrimaryKey, []string{"id"})
	assertPK(t, "posts", posts.PrimaryKey, []string{"id"})

	email := findColumn(t, users, "email")
	if !email.Nullable {
		t.Error("users.email should be nullable")
	}

	if email.Comment != "optional email address" {
		t.Errorf("users.email comment = %q, want %q", email.Comment, "optional email address")
	}

	assertFK(t, "posts → users", posts.ForeignKeys, "public.introspect_users", []string{"user_id"}, []string{"id"})
	assertFK(t, "users ← posts", users.ReferencedBy, "public.introspect_posts", []string{"user_id"}, []string{"id"})

	assertHasIndex(t, "users", users.Indexes, indexExpect{
		name: "introspect_users_pkey", cols: []string{"id"}, unique: true, method: "btree",
	})
	assertHasIndex(t, "users", users.Indexes, indexExpect{
		name: "introspect_users_email_idx", cols: []string{"email"}, unique: true, method: "btree",
	})
	assertHasIndex(t, "posts", posts.Indexes, indexExpect{
		name: "introspect_posts_user_title_idx", cols: []string{"user_id", "title"},
		unique: false, method: "btree",
	})
	// Partial-index predicate is captured into Index.Where.
	assertHasIndex(t, "users", users.Indexes, indexExpect{
		name: "introspect_users_active_idx", cols: []string{"name"},
		unique: false, method: "btree", whereContains: "email IS NOT NULL",
	})
}

func findTables(t *testing.T, s schema.Schema, usersName, postsName string) (users, posts *schema.Table) {
	t.Helper()

	for i := range s.Tables {
		switch s.Tables[i].Name {
		case usersName:
			users = &s.Tables[i]
		case postsName:
			posts = &s.Tables[i]
		}
	}

	if users == nil {
		t.Fatalf("table %q not found", usersName)
	}

	if posts == nil {
		t.Fatalf("table %q not found", postsName)
	}

	return users, posts
}

func findColumn(t *testing.T, tbl *schema.Table, name string) *schema.Column {
	t.Helper()

	for i := range tbl.Columns {
		if tbl.Columns[i].Name == name {
			return &tbl.Columns[i]
		}
	}

	t.Fatalf("%s.%s not found", tbl.Name, name)

	return nil
}

func assertPK(t *testing.T, name string, got, want []string) {
	t.Helper()

	if !equalSlice(got, want) {
		t.Errorf("%s PrimaryKey = %v, want %v", name, got, want)
	}
}

func assertFK(t *testing.T, label string, fks []schema.ForeignKey, wantTable string, wantCols, wantRefs []string) {
	t.Helper()

	if len(fks) != 1 {
		t.Fatalf("%s: got %d FKs, want 1", label, len(fks))
	}

	fk := fks[0]
	if fk.Table != wantTable {
		t.Errorf("%s Table = %q, want %q", label, fk.Table, wantTable)
	}

	if !equalSlice(fk.Columns, wantCols) {
		t.Errorf("%s Columns = %v, want %v", label, fk.Columns, wantCols)
	}

	if !equalSlice(fk.References, wantRefs) {
		t.Errorf("%s References = %v, want %v", label, fk.References, wantRefs)
	}
}

// indexExpect collects the per-index assertions so callers can opt into
// checking method / partial-predicate when relevant without bloating the
// happy-path call sites.
type indexExpect struct {
	name          string
	cols          []string
	unique        bool
	method        string // exact match if non-empty
	whereContains string // substring match if non-empty
}

func assertHasIndex(t *testing.T, label string, indexes []schema.Index, want indexExpect) {
	t.Helper()

	for _, idx := range indexes {
		if idx.Name != want.name {
			continue
		}

		if !equalSlice(idx.Columns, want.cols) {
			t.Errorf("%s index %q Columns = %v, want %v", label, want.name, idx.Columns, want.cols)
		}

		if idx.Unique != want.unique {
			t.Errorf("%s index %q Unique = %v, want %v", label, want.name, idx.Unique, want.unique)
		}

		if want.method != "" && idx.Method != want.method {
			t.Errorf("%s index %q Method = %q, want %q", label, want.name, idx.Method, want.method)
		}

		if want.whereContains != "" && !strings.Contains(idx.Where, want.whereContains) {
			t.Errorf("%s index %q Where = %q, want substring %q",
				label, want.name, idx.Where, want.whereContains)
		}

		return
	}

	t.Errorf("%s missing index %q; have %+v", label, want.name, indexes)
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
