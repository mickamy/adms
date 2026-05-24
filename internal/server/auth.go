package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// authBearer hashes both the configured and presented tokens with SHA-256
// before comparing, so length differences cannot leak through the
// constant-time compare's early-return-on-mismatched-length path. /healthz
// stays open regardless of token so liveness probes do not need it.
func authBearer(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}

	expected := sha256.Sum256([]byte(token))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthzPath(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		got, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="adms", error="invalid_request"`)
			writeProblem(w, r, http.StatusUnauthorized,
				"unauthenticated", "Unauthenticated",
				"request requires a Bearer token in the Authorization header")

			return
		}

		gotHash := sha256.Sum256([]byte(got))
		if subtle.ConstantTimeCompare(gotHash[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="adms", error="invalid_token"`)
			writeProblem(w, r, http.StatusUnauthorized,
				"unauthenticated", "Unauthenticated",
				"the Authorization token is invalid")

			return
		}

		next.ServeHTTP(w, r)
	})
}

func isHealthzPath(p string) bool {
	return p == "/healthz" || p == "/healthz/"
}

// bearerToken splits an Authorization header into its scheme and credentials.
// Returns false for anything but exactly `Bearer <token>` (case-insensitive
// scheme), since RFC 6750 token68 forbids embedded whitespace.
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
