package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// Principal is the authenticated identity attached to a request's context by
// the authenticate middleware. Handlers read it to make authorization
// decisions. A request served with authentication disabled still carries a
// Principal — an empty, anonymous one — so downstream code never has to guard
// against a missing value.
type Principal struct {
	Subject string
	Roles   []string
	Claims  map[string]any
}

// Authenticator resolves the Principal for a request. A nil error means the
// request is authenticated (possibly anonymously); returning an *authError
// rejects it and lets the middleware render the matching 401.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

var (
	_ Authenticator = noneAuth{}
	_ Authenticator = staticTokenAuth{}
)

// authError carries what the authenticate middleware needs to render a 401:
// the WWW-Authenticate challenge and the problem-detail message. Authenticators
// return it instead of writing to the ResponseWriter so their logic stays pure
// and testable.
type authError struct {
	wwwAuthenticate string
	detail          string
}

func (e *authError) Error() string { return e.detail }

type contextKey int

const principalKey contextKey = iota

// authenticate resolves the Principal via a and stores it in the request
// context before calling next. /healthz stays open regardless of the
// authenticator so liveness probes need no credentials.
func authenticate(a Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthzPath(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		p, err := a.Authenticate(r)
		if err != nil {
			writeAuthError(w, r, err)

			return
		}

		ctx := context.WithValue(r.Context(), principalKey, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	detail := "authentication failed"

	if ae, ok := errors.AsType[*authError](err); ok {
		if ae.wwwAuthenticate != "" {
			w.Header().Set("WWW-Authenticate", ae.wwwAuthenticate)
		}

		detail = ae.detail
	}

	writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Unauthenticated", detail)
}

// principalFrom returns the Principal stored by the authenticate middleware.
// ok is false on requests that bypass authentication (e.g. /healthz).
func principalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)

	return p, ok
}

// noneAuth authenticates every request as anonymous. It is selected when no
// token is configured, preserving the fully-open behavior.
type noneAuth struct{}

func (noneAuth) Authenticate(*http.Request) (Principal, error) {
	return Principal{}, nil
}

// staticTokenAuth accepts a single shared Bearer token. Both the configured
// and presented tokens are hashed with SHA-256 before a constant-time compare,
// so a length difference cannot leak through the compare's early-return path.
type staticTokenAuth struct {
	expected [sha256.Size]byte
}

func newStaticTokenAuth(token string) staticTokenAuth {
	return staticTokenAuth{expected: sha256.Sum256([]byte(token))}
}

func (a staticTokenAuth) Authenticate(r *http.Request) (Principal, error) {
	got, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return Principal{}, &authError{
			wwwAuthenticate: `Bearer realm="adms", error="invalid_request"`,
			detail:          "request requires a Bearer token in the Authorization header",
		}
	}

	gotHash := sha256.Sum256([]byte(got))
	if subtle.ConstantTimeCompare(gotHash[:], a.expected[:]) != 1 {
		return Principal{}, &authError{
			wwwAuthenticate: `Bearer realm="adms", error="invalid_token"`,
			detail:          "the Authorization token is invalid",
		}
	}

	return Principal{Subject: "static-token"}, nil
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
