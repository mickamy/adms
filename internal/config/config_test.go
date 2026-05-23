package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
)

func TestLoad(t *testing.T) {
	// Cannot use t.Parallel() here: subtests call t.Setenv, which is
	// incompatible with parallel execution (it mutates process-wide env).

	tests := []struct {
		name     string
		filename string
		body     string
		env      map[string]string
		want     config.Config
		wantErr  string
	}{
		{
			name:     "yaml minimal",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: "postgres://x"`,
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "postgres://x",
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "yaml full",
			filename: "adms.yaml",
			body: `driver: mysql
dsn: "root@tcp(localhost)/x"
listen: ":8080"
read_only: true
allowed_schemas: [public, internal]
allowed_tables: [users, posts]
timeout: 1m
cors_origins: ["http://localhost:3000"]
auth_token_env: ADMS_TOKEN
log_level: debug
ui:
  enabled: true
  listen: ":9000"`,
			want: config.Config{
				Driver:         database.DriverMySQL,
				DSN:            "root@tcp(localhost)/x",
				Listen:         ":8080",
				ReadOnly:       true,
				AllowedSchemas: []string{"public", "internal"},
				AllowedTables:  []string{"users", "posts"},
				Timeout:        time.Minute,
				CORSOrigins:    []string{"http://localhost:3000"},
				AuthTokenEnv:   "ADMS_TOKEN",
				LogLevel:       "debug",
				UI:             config.UIConfig{Enabled: true, Listen: ":9000"},
			},
		},
		{
			name:     "yaml env expansion",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: "${TEST_DSN}"`,
			env: map[string]string{"TEST_DSN": "postgres://expanded"},
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "postgres://expanded",
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "env value containing quotes does not break decode",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: "${TEST_DSN}"`,
			env: map[string]string{"TEST_DSN": `host:"port" path\with\backslash`},
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      `host:"port" path\with\backslash`,
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "env expansion applies to string slice entries",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
allowed_schemas: ["${TEST_SCHEMA}", "static"]`,
			env: map[string]string{"TEST_SCHEMA": "from_env"},
			want: config.Config{
				Driver:         database.DriverPostgres,
				DSN:            "x",
				Listen:         config.DefaultListen,
				AllowedSchemas: []string{"from_env", "static"},
				Timeout:        config.DefaultTimeout,
				LogLevel:       config.DefaultLogLevel,
				UI:             config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "string slice entries are trimmed and empties dropped",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
allowed_schemas: ["public", "  reporting  ", "", " "]
allowed_tables: [" users ", "posts", ""]
cors_origins: ["", "https://example.com  "]`,
			want: config.Config{
				Driver:         database.DriverPostgres,
				DSN:            "x",
				Listen:         config.DefaultListen,
				AllowedSchemas: []string{"public", "reporting"},
				AllowedTables:  []string{"users", "posts"},
				CORSOrigins:    []string{"https://example.com"},
				Timeout:        config.DefaultTimeout,
				LogLevel:       config.DefaultLogLevel,
				UI:             config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "env value expanding to empty is dropped from string slices",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
allowed_tables: ["users", "${UNSET_VAR}", "posts"]`,
			want: config.Config{
				Driver:        database.DriverPostgres,
				DSN:           "x",
				Listen:        config.DefaultListen,
				AllowedTables: []string{"users", "posts"},
				Timeout:       config.DefaultTimeout,
				LogLevel:      config.DefaultLogLevel,
				UI:            config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "yaml env unset expands to empty -> missing dsn",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: "${TEST_DSN}"`,
			wantErr: "dsn is required",
		},
		{
			name:     "toml minimal",
			filename: "adms.toml",
			body: `driver = "postgres"
dsn = "postgres://x"`,
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "postgres://x",
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "toml full with ui section",
			filename: "adms.toml",
			body: `driver = "mysql"
dsn = "root@tcp(localhost)/x"
listen = ":8080"
read_only = true
timeout = "1m"

[ui]
enabled = true
listen = ":9000"`,
			want: config.Config{
				Driver:   database.DriverMySQL,
				DSN:      "root@tcp(localhost)/x",
				Listen:   ":8080",
				ReadOnly: true,
				Timeout:  time.Minute,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Enabled: true, Listen: ":9000"},
			},
		},
		{
			name:     "yml extension is treated as yaml",
			filename: "adms.yml",
			body: `driver: postgres
dsn: x`,
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "x",
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "driver alias pg",
			filename: "adms.yaml",
			body: `driver: PG
dsn: x`,
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "x",
				Listen:   config.DefaultListen,
				Timeout:  config.DefaultTimeout,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "missing driver",
			filename: "adms.yaml",
			body:     `dsn: x`,
			wantErr:  "driver is required",
		},
		{
			name:     "whitespace-only driver treated as missing",
			filename: "adms.yaml",
			body: `driver: "   "
dsn: x`,
			wantErr: "driver is required",
		},
		{
			name:     "whitespace-only dsn treated as missing",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: "   "`,
			wantErr: "dsn is required",
		},
		{
			name:     "unknown driver",
			filename: "adms.yaml",
			body: `driver: sqlite
dsn: x`,
			wantErr: `unknown driver "sqlite"`,
		},
		{
			name:     "invalid timeout",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
timeout: notaduration`,
			wantErr: `invalid timeout "notaduration"`,
		},
		{
			name:     "zero timeout rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
timeout: 0s`,
			wantErr: "must be positive",
		},
		{
			name:     "negative timeout rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
timeout: -5s`,
			wantErr: "must be positive",
		},
		{
			name:     "default_limit and max_limit override defaults",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
default_limit: 50
max_limit: 200`,
			want: config.Config{
				Driver:       database.DriverPostgres,
				DSN:          "x",
				Listen:       config.DefaultListen,
				Timeout:      config.DefaultTimeout,
				LogLevel:     config.DefaultLogLevel,
				UI:           config.UIConfig{Listen: config.DefaultUIListen},
				DefaultLimit: 50,
				MaxLimit:     200,
			},
		},
		{
			name:     "negative default_limit rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
default_limit: -1`,
			wantErr: "default_limit",
		},
		{
			name:     "explicit zero default_limit rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
default_limit: 0`,
			wantErr: "default_limit",
		},
		{
			name:     "negative max_limit rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
max_limit: -1`,
			wantErr: "max_limit",
		},
		{
			name:     "explicit zero max_limit rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
max_limit: 0`,
			wantErr: "max_limit",
		},
		{
			name:     "default_limit exceeding max_limit rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
default_limit: 500
max_limit: 100`,
			wantErr: "default_limit (500) must not exceed max_limit (100)",
		},
		{
			name:     "whitespace around scalar string fields is normalized",
			filename: "adms.yaml",
			body: `driver: "  postgres  "
dsn: "  postgres://x  "
listen: "   "
timeout: "  1m  "
log_level: "   "
ui:
  listen: "   "`,
			want: config.Config{
				Driver:   database.DriverPostgres,
				DSN:      "postgres://x",
				Listen:   config.DefaultListen,
				Timeout:  time.Minute,
				LogLevel: config.DefaultLogLevel,
				UI:       config.UIConfig{Listen: config.DefaultUIListen},
			},
		},
		{
			name:     "yaml unknown field is rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
typo_field: oops`,
			wantErr: "parse yaml",
		},
		{
			name:     "yaml multi-document is rejected",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: x
---
driver: mysql
dsn: y`,
			wantErr: "multiple YAML documents are not supported",
		},
		{
			name:     "toml unknown field is rejected",
			filename: "adms.toml",
			body: `driver = "postgres"
dsn = "x"
typo_field = "oops"`,
			wantErr: "unknown toml key",
		},
		{
			name:     "unsupported extension",
			filename: "adms.json",
			body:     `{}`,
			wantErr:  "unsupported config extension",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			path := filepath.Join(t.TempDir(), tt.filename)
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			got, err := config.Load(path)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want substring %q", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Load() error = %q, want substring %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			assertConfigEqual(t, got, tt.want)
		})
	}
}

func TestLoad_ErrorIncludesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		body     string
	}{
		{
			name:     "yaml type mismatch",
			filename: "adms.yaml",
			body: `driver: postgres
dsn: [not, a, string]`,
		},
		{
			name:     "toml syntax error",
			filename: "adms.toml",
			body: `driver = "postgres"
[unterminated`,
		},
		{
			name:     "build-time validation error",
			filename: "adms.yaml",
			body:     `dsn: x`,
		},
		{
			name:     "build-time unknown driver",
			filename: "adms.yaml",
			body: `driver: sqlite
dsn: x`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), tt.filename)
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			_, err := config.Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want non-nil")
			}

			if !strings.Contains(err.Error(), path) {
				t.Errorf("Load() error = %q, want substring %q", err, path)
			}
		})
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil for missing file")
	}

	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("Load() error = %q, want substring %q", err, "read config")
	}
}

func TestLoad_Examples(t *testing.T) {
	// Cannot use t.Parallel() here: t.Setenv mutates process-wide env.

	t.Setenv("ADMS_DSN", "postgres://example")

	for _, path := range []string{
		"../../examples/adms.yaml",
		"../../examples/adms.toml",
	} {
		assertExampleLoads(t, path)
	}
}

func assertExampleLoads(t *testing.T, path string) {
	t.Helper()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", path, err)
	}

	if cfg.Driver != database.DriverPostgres {
		t.Errorf("[%s] Driver = %q, want %q", path, cfg.Driver, database.DriverPostgres)
	}

	if cfg.DSN != "postgres://example" {
		t.Errorf("[%s] DSN = %q, want %q (env expansion failed)", path, cfg.DSN, "postgres://example")
	}

	if cfg.Listen != ":7777" {
		t.Errorf("[%s] Listen = %q, want %q", path, cfg.Listen, ":7777")
	}

	if cfg.Timeout != 30*time.Second {
		t.Errorf("[%s] Timeout = %v, want %v", path, cfg.Timeout, 30*time.Second)
	}

	if cfg.UI.Listen != ":7778" {
		t.Errorf("[%s] UI.Listen = %q, want %q", path, cfg.UI.Listen, ":7778")
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("picks yaml over yml over toml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		for _, name := range []string{"adms.yaml", "adms.yml", "adms.toml"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}

		got, err := config.Detect(dir)
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}

		if want := filepath.Join(dir, "adms.yaml"); got != want {
			t.Errorf("Detect() = %q, want %q", got, want)
		}
	})

	t.Run("falls through to toml when only toml exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "adms.toml"), []byte(""), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		got, err := config.Detect(dir)
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}

		if want := filepath.Join(dir, "adms.toml"); got != want {
			t.Errorf("Detect() = %q, want %q", got, want)
		}
	})

	t.Run("returns ErrNoConfigFound on empty dir", func(t *testing.T) {
		t.Parallel()

		_, err := config.Detect(t.TempDir())
		if !errors.Is(err, config.ErrNoConfigFound) {
			t.Errorf("Detect() error = %v, want ErrNoConfigFound", err)
		}
	})

	t.Run("ignores directories named like config files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "adms.yaml"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		_, err := config.Detect(dir)
		if !errors.Is(err, config.ErrNoConfigFound) {
			t.Errorf("Detect() error = %v, want ErrNoConfigFound (directory should be ignored)", err)
		}
	})

	t.Run("returns wrapped error when stat fails for reasons other than not-exist", func(t *testing.T) {
		t.Parallel()

		// Using a regular file as the dir argument makes os.Stat on any child
		// path return ENOTDIR, which is not fs.ErrNotExist.
		notDir := filepath.Join(t.TempDir(), "regular-file")
		if err := os.WriteFile(notDir, []byte{}, 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		_, err := config.Detect(notDir)
		if err == nil {
			t.Fatal("Detect() error = nil, want non-nil error")
		}

		if errors.Is(err, config.ErrNoConfigFound) {
			t.Errorf("Detect() returned ErrNoConfigFound for ENOTDIR-like failure: %v", err)
		}

		if !strings.Contains(err.Error(), "stat") {
			t.Errorf("Detect() error = %q, want substring %q", err, "stat")
		}
	})
}

func assertConfigEqual(t *testing.T, got, want config.Config) {
	t.Helper()

	if got.Driver != want.Driver {
		t.Errorf("Driver = %q, want %q", got.Driver, want.Driver)
	}

	if got.DSN != want.DSN {
		t.Errorf("DSN = %q, want %q", got.DSN, want.DSN)
	}

	if got.Listen != want.Listen {
		t.Errorf("Listen = %q, want %q", got.Listen, want.Listen)
	}

	if got.ReadOnly != want.ReadOnly {
		t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, want.ReadOnly)
	}

	if !equalStrings(got.AllowedSchemas, want.AllowedSchemas) {
		t.Errorf("AllowedSchemas = %v, want %v", got.AllowedSchemas, want.AllowedSchemas)
	}

	if !equalStrings(got.AllowedTables, want.AllowedTables) {
		t.Errorf("AllowedTables = %v, want %v", got.AllowedTables, want.AllowedTables)
	}

	if got.Timeout != want.Timeout {
		t.Errorf("Timeout = %v, want %v", got.Timeout, want.Timeout)
	}

	if !equalStrings(got.CORSOrigins, want.CORSOrigins) {
		t.Errorf("CORSOrigins = %v, want %v", got.CORSOrigins, want.CORSOrigins)
	}

	if got.AuthTokenEnv != want.AuthTokenEnv {
		t.Errorf("AuthTokenEnv = %q, want %q", got.AuthTokenEnv, want.AuthTokenEnv)
	}

	if got.LogLevel != want.LogLevel {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, want.LogLevel)
	}

	if got.UI != want.UI {
		t.Errorf("UI = %+v, want %+v", got.UI, want.UI)
	}

	wantDefaultLimit := want.DefaultLimit
	if wantDefaultLimit == 0 {
		wantDefaultLimit = config.DefaultLimit
	}

	if got.DefaultLimit != wantDefaultLimit {
		t.Errorf("DefaultLimit = %d, want %d", got.DefaultLimit, wantDefaultLimit)
	}

	wantMaxLimit := want.MaxLimit
	if wantMaxLimit == 0 {
		wantMaxLimit = config.DefaultMaxLimit
	}

	if got.MaxLimit != wantMaxLimit {
		t.Errorf("MaxLimit = %d, want %d", got.MaxLimit, wantMaxLimit)
	}
}

func equalStrings(a, b []string) bool {
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
