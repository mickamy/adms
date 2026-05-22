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
