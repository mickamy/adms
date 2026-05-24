//go:build integration

package server_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/server"
)

func mysqlWriteDSN() string {
	if v := os.Getenv("ADMS_TEST_MYSQL_DSN"); v != "" {
		return v
	}

	return "root:mysql@tcp(localhost:3307)/adms_test?parseTime=true&multiStatements=true"
}

// writeFixtureMySQL mirrors writeFixturePostgres but targets the MySQL
// compose service. MySQL has no `return=representation` support yet (501
// at the handler), so this fixture exercises only the minimal write path
// and validates that json.Number round-trips through the driver.
func writeFixtureMySQL(t *testing.T, fn func(ctx testCtx)) {
	t.Helper()

	db, err := sql.Open("mysql", mysqlWriteDSN())
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
			name VARCHAR(64) NOT NULL,
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
			Driver:        database.DriverMySQL,
			Listen:        ":0",
			Timeout:       30 * time.Second,
			DefaultLimit:  100,
			MaxLimit:      1000,
			AllowedTables: []string{tableName},
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

	fn(testCtx{t: t, db: db, ts: ts, table: tableName})
}

func TestWriteHandlerMySQL_InsertSingleMinimal(t *testing.T) {
	t.Parallel()

	writeFixtureMySQL(t, func(c testCtx) {
		// id is BIGINT; ensure json.Number round-trips so the value lands
		// as 1 (and not a truncated float).
		resp := c.request(http.MethodPost, "/"+c.table,
			`{"id":9007199254740993,"name":"alice","active":true}`, nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 201; body = %s", resp.StatusCode, body)
		}

		var id int64
		err := c.db.QueryRowContext(c.t.Context(),
			fmt.Sprintf(`SELECT id FROM %s`, c.table)).Scan(&id)
		if err != nil {
			t.Fatalf("scan id: %v", err)
		}

		// 9007199254740993 (= 2^53 + 1) is the canonical "loses precision
		// when decoded as float64" value. If the test fixture comes back as
		// 9007199254740992, the json.Number path is broken.
		if id != 9007199254740993 {
			t.Errorf("id = %d, want 9007199254740993 (json.Number must preserve >2^53 integers)", id)
		}
	})
}

func TestWriteHandlerMySQL_RepresentationUnsupported(t *testing.T) {
	t.Parallel()

	writeFixtureMySQL(t, func(c testCtx) {
		resp := c.request(http.MethodPost, "/"+c.table,
			`{"id":1,"name":"alice","active":true}`,
			map[string]string{"Prefer": "return=representation"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotImplemented {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 501; body = %s", resp.StatusCode, body)
		}
	})
}

func TestWriteHandlerMySQL_PatchMinimalCountExact(t *testing.T) {
	t.Parallel()

	writeFixtureMySQL(t, func(c testCtx) {
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

func TestWriteHandlerMySQL_DeleteMinimalCountExact(t *testing.T) {
	t.Parallel()

	writeFixtureMySQL(t, func(c testCtx) {
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

func TestWriteHandlerMySQL_DuplicateInsertReturns409(t *testing.T) {
	t.Parallel()

	writeFixtureMySQL(t, func(c testCtx) {
		_, err := c.db.ExecContext(c.t.Context(),
			fmt.Sprintf(`INSERT INTO %s (id, name, active) VALUES (1, 'alice', true)`, c.table))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := c.request(http.MethodPost, "/"+c.table,
			`{"id":1,"name":"bob","active":true}`, nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 409; body = %s", resp.StatusCode, body)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "constraint-violation") {
			t.Errorf("body = %s, want it to mention constraint-violation", body)
		}
	})
}
