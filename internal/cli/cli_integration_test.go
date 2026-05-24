//go:build integration

package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mickamy/adms/internal/cli"
	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
	"github.com/mickamy/adms/internal/exit"
	"github.com/mickamy/adms/internal/server"
)

func pgTestDSN() string {
	if v := os.Getenv("ADMS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}

	return "postgres://postgres:postgres@localhost:5433/adms_test?sslmode=disable"
}

// TestRunServers_DualListenerGracefulShutdown drives runServers end-to-end
// against a real Postgres so the goroutine wiring, error channel drain, and
// shutdown path are exercised. The API + UI both listen on :0 so we never
// race on a fixed port.
func TestRunServers_DualListenerGracefulShutdown(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", pgTestDSN())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(t.Context()); err != nil {
		t.Skipf("postgres unreachable; skipping (is `docker compose up` running?): %v", err)
	}

	cfg := config.Config{
		Driver:         database.DriverPostgres,
		DSN:            pgTestDSN(),
		Listen:         "127.0.0.1:0",
		Timeout:        2 * time.Second,
		DefaultLimit:   100,
		MaxLimit:       1000,
		AllowedSchemas: []string{"public"},
		UI: config.UIConfig{
			Enabled: true,
			Listen:  "127.0.0.1:0",
		},
	}

	srv, err := server.New(cfg, db)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Prepare(t.Context()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var stderr bytes.Buffer

	done := make(chan int, 1)

	go func() { done <- cli.RunServers(ctx, cfg, srv, &stderr) }()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != exit.OK {
			t.Errorf("runServers exit = %d, want %d (stderr=%q)", code, exit.OK, stderr.String())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runServers did not return after cancel")
	}
}
