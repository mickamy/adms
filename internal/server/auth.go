package server

import (
	"crypto/subtle"
	"io"
	"net/http"
	"strings"
)

// authBearer enforces an HTTP Bearer authentication scheme. When token is
// empty, it returns next unchanged so configurations without auth pay no
// runtime cost. /healthz (with or without a trailing slash) stays open even
// when auth is enabled so liveness probes do not need the token.
//
// The scheme match is case-insensitive per RFC 7235 §2.1. The token compare
// is constant-time so a remote caller cannot distinguish "wrong length" from
// "wrong bytes" by timing.
func authBearer(out io.Writer, token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}

	expected := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthzPath(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		got, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="adms", error="invalid_request"`)
			writeProblem(w, r, out, http.StatusUnauthorized,
				"unauthenticated", "Unauthenticated",
				"request requires a Bearer token in the Authorization header")

			return
		}

		if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="adms", error="invalid_token"`)
			writeProblem(w, r, out, http.StatusUnauthorized,
				"unauthenticated", "Unauthenticated",
				"the Authorization token is invalid")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// isHealthzPath returns true for /healthz and /healthz/ (the only liveness-
// probe path adms exposes). Other trailing-slash variants like /healthz//
// stay in the auth path so we are not lenient beyond a single trailing slash.
func isHealthzPath(p string) bool {
	return p == "/healthz" || p == "/healthz/"
}

// bearerToken extracts the credentials from an Authorization header value of
// the form `Bearer <token>`. The scheme is matched case-insensitively. The
// header is rejected if it has anything other than exactly two whitespace-
// separated fields, since RFC 6750 token68 forbids embedded whitespace.
func bearerToken(h string) (string, bool) {
	fields := strings.Fields(h)
	if len(fields) != 2 {
		return "", false
	}

	if !strings.EqualFold(fields[0], "bearer") {
		return "", false
	}

	return fields[1], true
}
