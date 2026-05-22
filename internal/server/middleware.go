package server

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func recoverer(out io.Writer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Fprintf(out, "adms: panic in %s %s: %v\n", r.Method, r.URL.EscapedPath(), rec)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func logging(out io.Writer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		fmt.Fprintf(out, "%s %s %d %s\n",
			r.Method, r.URL.EscapedPath(), rec.status, time.Since(start).Round(time.Microsecond))
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

	n, err := r.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write: %w", err)
	}

	return n, nil
}
