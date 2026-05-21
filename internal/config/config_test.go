package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/database"
)

func TestParse(t *testing.T) {
	// Cannot use t.Parallel() here: subtests call t.Setenv, which is
	// incompatible with parallel execution (it mutates process-wide env).

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    config.Config
		wantErr string
	}{
		{
			name:    "missing driver",
			args:    []string{},
			wantErr: "--driver is required",
		},
		{
			name:    "missing dsn",
			args:    []string{"--driver=postgres"},
			wantErr: "--dsn is required",
		},
		{
			name:    "unknown driver",
			args:    []string{"--driver=sqlite", "--dsn=foo"},
			wantErr: `unknown driver "sqlite"`,
		},
		{
			name: "flags only",
			args: []string{
				"--driver=postgres",
				"--dsn=postgres://x",
				"--allowed-schemas=public,internal",
				"--allowed-tables=users,posts",
			},
			want: config.Config{
				Driver:         database.DriverPostgres,
				DSN:            "postgres://x",
				AllowedSchemas: []string{"public", "internal"},
				AllowedTables:  []string{"users", "posts"},
			},
		},
		{
			name: "env only",
			env: map[string]string{
				"ADMS_DRIVER":          "mysql",
				"ADMS_DSN":             "root@tcp(localhost)/x",
				"ADMS_ALLOWED_SCHEMAS": "main",
			},
			want: config.Config{
				Driver:         database.DriverMySQL,
				DSN:            "root@tcp(localhost)/x",
				AllowedSchemas: []string{"main"},
			},
		},
		{
			name: "flag overrides env",
			args: []string{"--driver=mysql"},
			env: map[string]string{
				"ADMS_DRIVER": "postgres",
				"ADMS_DSN":    "any-dsn",
			},
			want: config.Config{
				Driver: database.DriverMySQL,
				DSN:    "any-dsn",
			},
		},
		{
			name: "driver alias pg",
			args: []string{"--driver=PG", "--dsn=x"},
			want: config.Config{
				Driver: database.DriverPostgres,
				DSN:    "x",
			},
		},
		{
			name: "trim whitespace in csv",
			args: []string{"--driver=postgres", "--dsn=x", "--allowed-schemas=a, b ,c"},
			want: config.Config{
				Driver:         database.DriverPostgres,
				DSN:            "x",
				AllowedSchemas: []string{"a", "b", "c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{
				"ADMS_DRIVER",
				"ADMS_DSN",
				"ADMS_ALLOWED_SCHEMAS",
				"ADMS_ALLOWED_TABLES",
			} {
				t.Setenv(key, tt.env[key])
			}

			got, err := config.Parse(tt.args, &bytes.Buffer{})

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Parse() error = nil, want substring %q", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Parse() error = %q, want substring %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			if got.Driver != tt.want.Driver {
				t.Errorf("Driver = %q, want %q", got.Driver, tt.want.Driver)
			}

			if got.DSN != tt.want.DSN {
				t.Errorf("DSN = %q, want %q", got.DSN, tt.want.DSN)
			}

			if !equalStrings(got.AllowedSchemas, tt.want.AllowedSchemas) {
				t.Errorf("AllowedSchemas = %v, want %v", got.AllowedSchemas, tt.want.AllowedSchemas)
			}

			if !equalStrings(got.AllowedTables, tt.want.AllowedTables) {
				t.Errorf("AllowedTables = %v, want %v", got.AllowedTables, tt.want.AllowedTables)
			}
		})
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
