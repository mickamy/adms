//go:build integration

package schema_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/mickamy/adms/internal/schema"
)

func mysqlTestDSN() string {
	if v := os.Getenv("ADMS_TEST_MYSQL_DSN"); v != "" {
		return v
	}

	return "root:mysql@tcp(localhost:3307)/adms_test?parseTime=true&multiStatements=true"
}

func TestMySQLIntrospect(t *testing.T) {
	conn, err := sql.Open("mysql", mysqlTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := t.Context()
	if err := conn.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	stmts := []string{
		`DROP TABLE IF EXISTS posts`,
		`DROP TABLE IF EXISTS users`,
		`CREATE TABLE users (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255) COMMENT 'optional email address',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE posts (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			user_id BIGINT UNSIGNED NOT NULL,
			title VARCHAR(255) NOT NULL,
			CONSTRAINT fk_posts_user FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
	}

	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	got, err := schema.MySQLIntrospector().Introspect(ctx, conn, []string{"adms_test"})
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

	assertFK(t, "posts → users", posts.ForeignKeys, "adms_test.users", []string{"user_id"}, []string{"id"})
	assertFK(t, "users ← posts", users.ReferencedBy, "adms_test.posts", []string{"user_id"}, []string{"id"})
}
