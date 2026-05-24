package logger_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/logger"
)

func TestNew_LevelMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		level     string
		emitDebug bool
		emitInfo  bool
		emitWarn  bool
		emitError bool
	}{
		{"debug", "debug", true, true, true, true},
		{"info", "info", false, true, true, true},
		{"warn", "warn", false, false, true, true},
		{"error", "error", false, false, false, true},
		{"unknown defaults to info", "verbose", false, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			l := logger.New(&buf, tc.level)

			l.Debug("d")
			l.Info("i")
			l.Warn("w")
			l.Error("e")

			out := buf.String()

			checks := []struct {
				want bool
				msg  string
			}{
				{tc.emitDebug, "d"},
				{tc.emitInfo, "i"},
				{tc.emitWarn, "w"},
				{tc.emitError, "e"},
			}

			for _, c := range checks {
				got := containsLogMsg(t, out, c.msg)
				if got != c.want {
					t.Errorf("emit %q = %v, want %v\noutput:\n%s", c.msg, got, c.want, out)
				}
			}
		})
	}
}

//nolint:paralleltest // Init mutates the process-wide slog default.
func TestInit_RoutesPackageHelpersToTarget(t *testing.T) {
	var buf bytes.Buffer

	logger.Capture(t, &buf) // restores after this test so Init's effect is scoped.
	logger.Init(&buf, "info")

	logger.Info(t.Context(), "via default", "key", "value")

	if !containsLogMsg(t, buf.String(), "via default") {
		t.Errorf("default logger did not write to the configured destination, got:\n%s", buf.String())
	}
}

//nolint:paralleltest // Capture mutates the process-wide slog default.
func TestCapture_RestoresPreviousDefault(t *testing.T) {
	var first, second bytes.Buffer

	ctx := t.Context()

	logger.Capture(t, &first)
	logger.Info(ctx, "inside outer capture")

	t.Run("nested capture restores on subtest end", func(t *testing.T) {
		logger.Capture(t, &second)
		logger.Info(t.Context(), "inside nested capture")
	})

	logger.Info(ctx, "after nested capture")

	if !containsLogMsg(t, first.String(), "inside outer capture") {
		t.Errorf("first buf missing outer log: %s", first.String())
	}

	if !containsLogMsg(t, second.String(), "inside nested capture") {
		t.Errorf("second buf missing nested log: %s", second.String())
	}

	if !containsLogMsg(t, first.String(), "after nested capture") {
		t.Errorf("first buf should resume receiving after nested t.Cleanup ran: %s", first.String())
	}

	if containsLogMsg(t, second.String(), "after nested capture") {
		t.Errorf("second buf should not receive logs after its capture ended: %s", second.String())
	}
}

//nolint:paralleltest // Capture mutates the process-wide slog default.
func TestPackageHelpers_AllLevels(t *testing.T) {
	var buf bytes.Buffer

	logger.Capture(t, &buf)

	ctx := t.Context()

	logger.Debug(ctx, "d")
	logger.Info(ctx, "i")
	logger.Warn(ctx, "w")
	logger.Error(ctx, "e")

	for _, msg := range []string{"d", "i", "w", "e"} {
		if !containsLogMsg(t, buf.String(), msg) {
			t.Errorf("missing msg=%q in: %s", msg, buf.String())
		}
	}
}

//nolint:paralleltest // Capture mutates the process-wide slog default.
func TestPackageHelpers_SourceAttributionPointsAtCaller(t *testing.T) {
	// When source attribution is on, the JSON handler emits
	// "source":{"function":"...","file":"...","line":N}. The wrapper has to
	// skip its own frame so the source resolves to the caller, not to
	// internal/logger/logger.go.
	var buf bytes.Buffer

	logger.Capture(t, &buf)

	logger.Info(t.Context(), "trace caller")

	rec := decodeFirstRecord(t, buf.String())

	src, ok := rec["source"].(map[string]any)
	if !ok {
		t.Fatalf("source attribute missing or wrong type: %T %v", rec["source"], rec["source"])
	}

	file, _ := src["file"].(string)
	if !strings.HasSuffix(file, "logger_test.go") {
		t.Errorf("source.file = %q, want suffix logger_test.go (otherwise wrappers leaked their own frame)", file)
	}
}

func decodeFirstRecord(t *testing.T, raw string) map[string]any {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}

		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		return rec
	}

	t.Fatalf("no decodable JSON log line in:\n%s", raw)

	return nil
}

func containsLogMsg(t *testing.T, raw, msg string) bool {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}

		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Logf("skip undecodable line: %q (%v)", line, err)

			continue
		}

		if rec["msg"] == msg {
			return true
		}
	}

	return false
}
