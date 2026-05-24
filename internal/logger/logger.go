// Package logger wraps log/slog so other packages can log without
// importing slog directly.
package logger

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"time"
)

// New constructs a JSON logger at the named level. Unknown levels fall
// back to info; config.Load already rejects anything outside the set, so
// this branch only triggers if a caller bypasses config validation.
func New(w io.Writer, level string) *slog.Logger {
	var lvl slog.Level

	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	}))
}

// Init installs a logger built from New as slog's process-wide default.
func Init(w io.Writer, level string) {
	slog.SetDefault(New(w, level))
}

// Capture swaps the default logger for the duration of the test. The swap
// is global, so tests calling Capture must not use t.Parallel().
func Capture(t TestingT, w io.Writer) {
	t.Helper()

	prev := slog.Default()
	slog.SetDefault(New(w, "debug"))

	t.Cleanup(func() { slog.SetDefault(prev) })
}

// TestingT is the subset of *testing.T that Capture needs, declared here
// so production builds do not depend on the testing package.
type TestingT interface {
	Helper()
	Cleanup(fn func())
}

func Debug(ctx context.Context, msg string, args ...any) {
	logAt(ctx, slog.LevelDebug, msg, args...)
}

func Info(ctx context.Context, msg string, args ...any) {
	logAt(ctx, slog.LevelInfo, msg, args...)
}

func Warn(ctx context.Context, msg string, args ...any) {
	logAt(ctx, slog.LevelWarn, msg, args...)
}

func Error(ctx context.Context, msg string, args ...any) {
	logAt(ctx, slog.LevelError, msg, args...)
}

// logAt builds the record with a program counter pointing at the caller of
// the public wrapper, so slog's source attribute resolves to user code
// rather than to this file. The skip count of 3 omits runtime.Callers,
// logAt, and the Debug/Info/Warn/Error wrapper itself.
func logAt(ctx context.Context, level slog.Level, msg string, args ...any) {
	l := slog.Default()
	if !l.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr

	runtime.Callers(3, pcs[:])

	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)

	_ = l.Handler().Handle(ctx, r)
}
