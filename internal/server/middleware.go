package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mickamy/adms/internal/logger"
)

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func(ctx context.Context) {
			if rec := recover(); rec != nil {
				logger.Error(ctx, "panic",
					"method", r.Method,
					"path", r.URL.EscapedPath(),
					"recover", fmt.Sprint(rec))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}(r.Context())

		next.ServeHTTP(w, r)
	})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logger.Info(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.EscapedPath(),
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}

	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying ResponseWriter so
// handlers can still access optional methods (Flush, Hijack, etc.) through the
// middleware chain.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true

	// Pass the error through unchanged so handlers and middleware can match it
	// against sentinel values (e.g., http.ErrAbortHandler) via errors.Is/As.
	return r.ResponseWriter.Write(b) //nolint:wrapcheck
}
