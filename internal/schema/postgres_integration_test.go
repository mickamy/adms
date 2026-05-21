//go:build integration

package schema_test

import (
	"database/sql"
	"os"
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
		`DROP TABLE IF EXISTS posts, users CASCADE`,
		`CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE posts (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id),
			title TEXT NOT NULL
		)`,
		`COMMENT ON COLUMN users.email IS 'optional email address'`,
	}

	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	got, err := schema.PostgresIntrospector().Introspect(ctx, conn, []string{"public"})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}

	users, posts := findTables(t, got)

	assertPK(t, "users", users.PrimaryKey, []string{"id"})
	assertPK(t, "posts", posts.PrimaryKey, []string{"id"})

	email := findColumn(t, users, "email")
	if !email.Nullable {
		t.Error("users.email should be nullable")
	}

	if email.Comment != "optional email address" {
		t.Errorf("users.email comment = %q, want %q", email.Comment, "optional email address")
	}

	assertFK(t, "posts → users", posts.ForeignKeys, "public.users", []string{"user_id"}, []string{"id"})
	assertFK(t, "users ← posts", users.ReferencedBy, "public.posts", []string{"user_id"}, []string{"id"})
}

func findTables(t *testing.T, s schema.Schema) (users, posts *schema.Table) {
	t.Helper()

	for i := range s.Tables {
		switch s.Tables[i].Name {
		case "users":
			users = &s.Tables[i]
		case "posts":
			posts = &s.Tables[i]
		}
	}

	if users == nil {
		t.Fatal("users table not found")
	}

	if posts == nil {
		t.Fatal("posts table not found")
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
