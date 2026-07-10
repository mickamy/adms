package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/cli"
	"github.com/mickamy/adms/internal/exit"
)

//nolint:paralleltest // t.Chdir mutates process-wide CWD and is not parallel-safe.
func TestRun_NoArgsAutoDetectsNoConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer

	code := cli.Run(nil, &stdout, &stderr)
	if code != exit.Usage {
		t.Errorf("exit = %d, want %d (stderr=%q)", code, exit.Usage, stderr.String())
	}

	if !strings.Contains(stderr.String(), "no config file found") {
		t.Errorf("stderr = %q, want substring %q", stderr.String(), "no config file found")
	}

	if !strings.Contains(stderr.String(), "adms [config-file]") {
		t.Errorf("stderr should include usage, got %q", stderr.String())
	}
}

func TestRun_PreConnectionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       func(t *testing.T) []string
		wantExit   int
		wantStderr string
	}{
		{
			name:       "too many arguments",
			args:       func(_ *testing.T) []string { return []string{"a.yaml", "b.yaml"} },
			wantExit:   exit.Usage,
			wantStderr: "too many arguments",
		},
		{
			name: "missing file",
			args: func(t *testing.T) []string {
				return []string{filepath.Join(t.TempDir(), "missing.yaml")}
			},
			wantExit:   exit.Usage,
			wantStderr: "read config",
		},
		{
			name: "unsupported extension",
			args: func(t *testing.T) []string {
				p := filepath.Join(t.TempDir(), "adms.json")
				if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}

				return []string{p}
			},
			wantExit:   exit.Usage,
			wantStderr: "unsupported config extension",
		},
		{
			name: "missing driver in config",
			args: func(t *testing.T) []string {
				p := filepath.Join(t.TempDir(), "adms.yaml")
				if err := os.WriteFile(p, []byte("dsn: x\n"), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}

				return []string{p}
			},
			wantExit:   exit.Usage,
			wantStderr: "driver is required",
		},
		{
			name: "unknown driver",
			args: func(t *testing.T) []string {
				p := filepath.Join(t.TempDir(), "adms.yaml")
				body := "driver: sqlite\ndsn: x\n"
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}

				return []string{p}
			},
			wantExit:   exit.Usage,
			wantStderr: `unknown driver "sqlite"`,
		},
		{
			// Env name is unique to this test so parallel runs cannot race on it.
			name: "auth static token_env points to unset variable",
			args: func(t *testing.T) []string {
				p := filepath.Join(t.TempDir(), "adms.yaml")
				body := "driver: postgres\ndsn: x\n" +
					"auth:\n  mode: static\n  static:\n    token_env: ADMS_AUTH_TOKEN_NOT_SET_IN_CLI_TEST\n"
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}

				return []string{p}
			},
			wantExit:   exit.Usage,
			wantStderr: "auth.static.token_env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			code := cli.Run(tt.args(t), &stdout, &stderr)
			if code != tt.wantExit {
				t.Errorf("exit = %d, want %d (stderr=%q)", code, tt.wantExit, stderr.String())
			}

			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

//nolint:paralleltest // t.Setenv mutates process-wide env and is not parallel-safe.
func TestRun_ResolvesAuthTokenFromEnv(t *testing.T) {
	const envName = "ADMS_AUTH_TOKEN_RESOLVED_IN_CLI_TEST"

	// DSN points to a closed TCP port so pingDB fails fast (exit.Error)
	// after resolveAuth runs, without needing a real database.
	cases := []struct {
		name      string
		authBlock string
		setenv    func(t *testing.T)
	}{
		{
			name:      "auth mode none needs no resolution and run reaches ping",
			authBlock: "",
			setenv:    func(*testing.T) {},
		},
		{
			name:      "auth static resolves token from populated env",
			authBlock: "auth:\n  mode: static\n  static:\n    token_env: " + envName + "\n",
			setenv: func(t *testing.T) {
				t.Setenv(envName, "tk")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setenv(t)

			path := filepath.Join(t.TempDir(), "adms.yaml")

			body := "driver: postgres\n" +
				`dsn: "postgres://adms:adms@127.0.0.1:1/adms_test?sslmode=disable&connect_timeout=1"` + "\n" +
				tc.authBlock +
				"timeout: 1s\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			var stdout, stderr bytes.Buffer

			code := cli.Run([]string{path}, &stdout, &stderr)
			if code != exit.Error {
				t.Fatalf("exit = %d, want %d (stderr=%q)", code, exit.Error, stderr.String())
			}

			if strings.Contains(stderr.String(), "token_env") {
				t.Errorf("stderr = %q, want token_env NOT to appear (resolution should have succeeded)",
					stderr.String())
			}

			if !strings.Contains(stderr.String(), "ping") {
				t.Errorf("stderr = %q, want substring %q (run should reach pingDB)", stderr.String(), "ping")
			}
		})
	}
}

func TestUICORSOrigins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		listen string
		want   []string
	}{
		{"port only", ":7778", []string{"http://localhost:7778", "http://127.0.0.1:7778"}},
		{"host:port", "127.0.0.1:8080", []string{"http://localhost:8080", "http://127.0.0.1:8080"}},
		{"malformed empty", "", nil},
		{"malformed no port", "localhost", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := cli.UICORSOrigins(tc.listen)

			if !equalStringSlices(got, tc.want) {
				t.Errorf("UICORSOrigins(%q) = %v, want %v", tc.listen, got, tc.want)
			}
		})
	}
}

func TestAPIOriginFromListen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		listen string
		want   string
	}{
		{"port only", ":7777", "http://localhost:7777"},
		{"bind-all v4", "0.0.0.0:7777", "http://localhost:7777"},
		{"bind-all v6", "[::]:7777", "http://localhost:7777"},
		{"explicit host", "127.0.0.1:8000", "http://127.0.0.1:8000"},
		{"malformed", "", "http://localhost"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cli.APIOriginFromListen(tc.listen); got != tc.want {
				t.Errorf("APIOriginFromListen(%q) = %q, want %q", tc.listen, got, tc.want)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
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

func TestPrintUsage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cli.PrintUsage(&buf)

	for _, want := range []string{
		"adms [config-file]",
		"--version",
		"--help",
		"${VAR}",
		"Literal $ cannot be escaped",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("PrintUsage output missing %q\n---output---\n%s", want, buf.String())
		}
	}
}
