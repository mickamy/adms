//go:build integration

package server_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/server"
)

// writeFixturePostgres provisions a fresh smoke table on the shared PG test
// database, runs fn against a live server, and tears the table down. Each
// test gets a unique table name so several t.Parallel() siblings don't race
// the same CREATE TABLE — Postgres' system catalog locks deadlock when
// concurrent CREATE TABLE statements collide on the same relation name.
func writeFixturePostgres(t *testing.T, fn func(ctx testCtx)) {
	t.Helper()

	db, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v (is `docker compose up` running?)", err)
	}

	tableName := uniqueTableName(t.Name())

	stmts := []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tableName),
		fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL DEFAULT TRUE
		)`, tableName),
	}

	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tableName))
	})

	srv, err := server.New(
		config.Config{
			Driver:         database.DriverPostgres,
			Listen:         ":0",
			Timeout:        30 * time.Second,
			DefaultLimit:   100,
			MaxLimit:       1000,
			AllowedSchemas: []string{"public"},
			AllowedTables:  []string{tableName},
		},
		db,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	fn(testCtx{t: t, db: db, ts: ts, table: tableName})
}

// uniqueTableName turns a test name into a PG-safe identifier; only ASCII
// letters, digits, and underscores survive, so siblings of the same TestFoo
// run against distinct tables. Postgres caps identifiers at NAMEDATALEN-1
// = 63 bytes by default, so long names are truncated and a stable hash
// suffix is appended to keep distinct test names distinct after truncation.
func uniqueTableName(testName string) string {
	var b strings.Builder

	b.WriteString("write_smoke_")

	for _, r := range testName {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	name := strings.ToLower(b.String())

	const maxIdentLen = 63
	if len(name) <= maxIdentLen {
		return name
	}

	suffix := fmt.Sprintf("_%08x", crc32.ChecksumIEEE([]byte(name)))

	return name[:maxIdentLen-len(suffix)] + suffix
}

func TestUniqueTableName_FitsPostgresIdentLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{"short name passes through", "TestFoo"},
		{"exactly at limit", strings.Repeat("X", 51)},
		{"well over limit", strings.Repeat("X", 200)},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := uniqueTableName(tt.in)
			if len(got) > 63 {
				t.Errorf("len(%q) = %d, want <= 63", got, len(got))
			}
		})
	}

	// Distinct long names should still produce distinct identifiers after
	// truncation thanks to the hash suffix.
	a := uniqueTableName(strings.Repeat("X", 100) + "A")
	b := uniqueTableName(strings.Repeat("X", 100) + "B")

	if a == b {
		t.Errorf("uniqueTableName collision after truncation: %q == %q", a, b)
	}
}

type testCtx struct {
	t     *testing.T
	db    *sql.DB
	ts    *httptest.Server
	table string
}

func (c testCtx) request(method, path, body string, headers map[string]string) *http.Response {
	c.t.Helper()

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(c.t.Context(), method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}

	return resp
}

func TestWriteHandlerPostgres_InsertSingleMinimal(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		resp := c.request(http.MethodPost, "/"+c.table,
			`{"id":1,"name":"alice","active":true}`, nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 201; body = %s", resp.StatusCode, body)
		}

		var n int
		err := c.db.QueryRowContext(c.t.Context(),
			fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = 1`, c.table)).Scan(&n)
		if err != nil {
			t.Fatalf("count: %v", err)
		}

		if n != 1 {
			t.Errorf("inserted rows = %d, want 1", n)
		}
	})
}

func TestWriteHandlerPostgres_InsertBulkRepresentation(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		resp := c.request(http.MethodPost, "/"+c.table,
			`[{"id":1,"name":"alice","active":true},{"id":2,"name":"bob","active":false}]`,
			map[string]string{"Prefer": "return=representation"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 201; body = %s", resp.StatusCode, body)
		}

		if got := resp.Header.Get("Content-Range"); got != "0-1/2" {
			t.Errorf("Content-Range = %q, want %q", got, "0-1/2")
		}

		var rows []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got, want := len(rows), 2; got != want {
			t.Fatalf("rows = %d, want %d", got, want)
		}

		if got := rows[0]["name"]; got != "alice" {
			t.Errorf("rows[0].name = %v, want alice", got)
		}
	})
}

func TestWriteHandlerPostgres_UpdateMinimalCountExact(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		_, err := c.db.ExecContext(c.t.Context(),
			fmt.Sprintf(`INSERT INTO %s (id, name, active) VALUES
				(1, 'alice', true), (2, 'bob', true), (3, 'carol', false)`, c.table))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := c.request(http.MethodPatch, "/"+c.table+"?active=is.true",
			`{"name":"updated"}`,
			map[string]string{"Prefer": "count=exact"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 204; body = %s", resp.StatusCode, body)
		}

		if got := resp.Header.Get("Content-Range"); got != "*/2" {
			t.Errorf("Content-Range = %q, want %q", got, "*/2")
		}
	})
}

func TestWriteHandlerPostgres_UpdateRepresentation(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		_, err := c.db.ExecContext(c.t.Context(),
			fmt.Sprintf(`INSERT INTO %s (id, name, active) VALUES (1, 'alice', true)`, c.table))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := c.request(http.MethodPatch, "/"+c.table+"?id=eq.1",
			`{"name":"alice2"}`,
			map[string]string{"Prefer": "return=representation"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
		}

		var rows []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got, want := len(rows), 1; got != want {
			t.Fatalf("rows = %d, want %d", got, want)
		}

		if got := rows[0]["name"]; got != "alice2" {
			t.Errorf("rows[0].name = %v, want alice2", got)
		}
	})
}

func TestWriteHandlerPostgres_DeleteRepresentation(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		_, err := c.db.ExecContext(c.t.Context(),
			fmt.Sprintf(`INSERT INTO %s (id, name, active) VALUES
				(1, 'alice', true), (2, 'bob', false)`, c.table))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := c.request(http.MethodDelete, "/"+c.table+"?id=eq.1", "",
			map[string]string{"Prefer": "return=representation"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
		}

		var rows []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got, want := len(rows), 1; got != want {
			t.Fatalf("rows = %d, want %d (deleted snapshot)", got, want)
		}

		var remaining int
		err = c.db.QueryRowContext(c.t.Context(),
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, c.table)).Scan(&remaining)
		if err != nil {
			t.Fatalf("count: %v", err)
		}

		if remaining != 1 {
			t.Errorf("remaining rows = %d, want 1", remaining)
		}
	})
}

func TestWriteHandlerPostgres_DBErrorReturns500(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		// Close the DB after the server has finished introspection. Subsequent
		// QueryContext / ExecContext calls fail, exercising executeWrite's
		// error paths (covering both the representation and minimal branches).
		_ = c.db.Close()

		cases := []struct {
			name    string
			method  string
			path    string
			body    string
			headers map[string]string
		}{
			{
				"POST minimal", http.MethodPost, "/" + c.table,
				`{"id":1,"name":"alice","active":true}`, nil,
			},
			{
				"PATCH representation", http.MethodPatch, "/" + c.table + "?id=eq.1",
				`{"name":"x"}`, map[string]string{"Prefer": "return=representation"},
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				resp := c.request(tt.method, tt.path, tt.body, tt.headers)
				defer func() { _ = resp.Body.Close() }()

				if resp.StatusCode != http.StatusInternalServerError {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf("status = %d, want 500; body = %s", resp.StatusCode, body)
				}
			})
		}
	})
}

func TestWriteHandlerPostgres_DeleteMinimalCountExact(t *testing.T) {
	t.Parallel()

	writeFixturePostgres(t, func(c testCtx) {
		_, err := c.db.ExecContext(c.t.Context(),
			fmt.Sprintf(`INSERT INTO %s (id, name, active) VALUES
				(1, 'alice', true), (2, 'bob', false), (3, 'carol', true)`, c.table))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := c.request(http.MethodDelete, "/"+c.table+"?active=is.true", "",
			map[string]string{"Prefer": "count=exact"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 204; body = %s", resp.StatusCode, body)
		}

		if got := resp.Header.Get("Content-Range"); got != "*/2" {
			t.Errorf("Content-Range = %q, want %q", got, "*/2")
		}
	})
}
