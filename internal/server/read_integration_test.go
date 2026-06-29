//go:build integration

package server_test

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/server"
)

func pgTestDSN() string {
	if v := os.Getenv("ADMS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}

	return "postgres://postgres:postgres@localhost:5433/adms_test?sslmode=disable"
}

func TestReadHandlerPostgres_Success(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	stmts := []string{
		`DROP TABLE IF EXISTS read_smoke_users`,
		`CREATE TABLE read_smoke_users (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL
		)`,
		`INSERT INTO read_smoke_users (id, name, active) VALUES
			(1, 'alice', true),
			(2, 'bob', false),
			(3, 'carol', true)`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS read_smoke_users`)
	})

	srv, err := server.New(
		config.Config{
			Driver:         database.DriverPostgres,
			Listen:         ":0",
			Timeout:        30 * time.Second,
			DefaultLimit:   100,
			MaxLimit:       1000,
			AllowedSchemas: []string{"public"},
		},
		db,
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/read_smoke_users?order=id.asc&active=is.true", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows = %d, want %d (active=true filter should drop bob)", got, want)
	}

	if got, want := rows[0]["name"], "alice"; got != want {
		t.Errorf("rows[0].name = %v, want %q", got, want)
	}

	if got, want := rows[1]["name"], "carol"; got != want {
		t.Errorf("rows[1].name = %v, want %q", got, want)
	}

	if got, want := rows[0]["active"], true; got != want {
		t.Errorf("rows[0].active = %v, want %v", got, want)
	}
}

func TestReadHandlerPostgres_Embed(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	stmts := []string{
		`DROP TABLE IF EXISTS embed_posts, embed_users`,
		`CREATE TABLE embed_users (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL
		)`,
		`CREATE TABLE embed_posts (
			id BIGINT PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES embed_users(id),
			title TEXT NOT NULL
		)`,
		`INSERT INTO embed_users (id, name) VALUES (1, 'alice'), (2, 'bob')`,
		`INSERT INTO embed_posts (id, user_id, title) VALUES
			(10, 1, 'hello'),
			(11, 1, 'world'),
			(20, 2, 'solo')`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS embed_posts, embed_users`)
	})

	srv, err := server.New(
		config.Config{
			Driver:         database.DriverPostgres,
			Listen:         ":0",
			Timeout:        30 * time.Second,
			DefaultLimit:   100,
			MaxLimit:       1000,
			AllowedSchemas: []string{"public"},
			AllowedTables:  []string{"embed_users", "embed_posts"},
		},
		db,
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	t.Run("one-to-many", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/embed_users?select=id,name,embed_posts(id,title)&order=id.asc", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
		}

		var rows []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got, want := len(rows), 2; got != want {
			t.Fatalf("rows = %d, want %d", got, want)
		}

		alicePosts, ok := rows[0]["embed_posts"].([]any)
		if !ok {
			t.Fatalf("alice.embed_posts = %T, want []any", rows[0]["embed_posts"])
		}

		if got, want := len(alicePosts), 2; got != want {
			t.Errorf("alice posts count = %d, want %d", got, want)
		}

		bobPosts, ok := rows[1]["embed_posts"].([]any)
		if !ok {
			t.Fatalf("bob.embed_posts = %T, want []any", rows[1]["embed_posts"])
		}

		if got, want := len(bobPosts), 1; got != want {
			t.Errorf("bob posts count = %d, want %d", got, want)
		}
	})

	t.Run("many-to-one with alias", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/embed_posts?select=id,title,author:embed_users(id,name)&order=id.asc", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
		}

		var rows []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got, want := len(rows), 3; got != want {
			t.Fatalf("rows = %d, want %d", got, want)
		}

		author, ok := rows[0]["author"].(map[string]any)
		if !ok {
			t.Fatalf("row[0].author = %T, want map[string]any", rows[0]["author"])
		}

		if got, want := author["name"], "alice"; got != want {
			t.Errorf("row[0].author.name = %v, want %q", got, want)
		}
	})
}

func TestReadHandlerPostgres_CSV(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	stmts := []string{
		`DROP TABLE IF EXISTS csv_export`,
		`CREATE TABLE csv_export (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL
		)`,
		// A value with a comma and a quote exercises RFC 4180 escaping.
		`INSERT INTO csv_export (id, name) VALUES
			(1, 'alice'),
			(2, 'bob, "the builder"')`,
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS csv_export`)
	})

	srv, err := server.New(
		config.Config{
			Driver:         database.DriverPostgres,
			Listen:         ":0",
			Timeout:        30 * time.Second,
			DefaultLimit:   100,
			MaxLimit:       1000,
			AllowedSchemas: []string{"public"},
		},
		db,
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/csv_export?order=id.asc", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/csv")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/csv; charset=utf-8", ct)
	}

	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="csv_export.csv"` {
		t.Errorf("Content-Disposition = %q, want attachment; filename=\"csv_export.csv\"", cd)
	}

	got, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}

	want := [][]string{
		{"id", "name"},
		{"1", "alice"},
		{"2", `bob, "the builder"`},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("CSV content mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}
