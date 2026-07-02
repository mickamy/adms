package server_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/adms/internal/config"
	"github.com/mickamy/adms/internal/server"
)

func TestExtractRoles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		claims map[string]any
		claim  string
		want   []string
	}{
		{"empty claim name", map[string]any{"roles": []any{"a"}}, "", nil},
		{"missing claim", map[string]any{"other": 1}, "roles", nil},
		{"array of strings", map[string]any{"roles": []any{"viewer", "admin"}}, "roles", []string{"viewer", "admin"}},
		{
			"array drops non-strings and empties",
			map[string]any{"roles": []any{"viewer", 1, "", "admin"}}, "roles",
			[]string{"viewer", "admin"},
		},
		{"space-separated string", map[string]any{"scope": "viewer admin"}, "scope", []string{"viewer", "admin"}},
		{"wrong type yields nil", map[string]any{"roles": 42}, "roles", nil},
		{"empty array yields empty", map[string]any{"roles": []any{}}, "roles", []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := server.ExtractRoles(tc.claims, tc.claim)
			if !equalStringSlice(got, tc.want) {
				t.Errorf("ExtractRoles(%v, %q) = %v, want %v", tc.claims, tc.claim, got, tc.want)
			}
		})
	}
}

func TestOIDCAuth_Verification(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	idp := newMockIDP(t, key)

	auth, err := server.NewOIDCAuth(config.OIDC{
		Issuer:     idp.URL,
		Audience:   "adms",
		RolesClaim: "roles",
	})
	if err != nil {
		t.Fatalf("NewOIDCAuth: %v", err)
	}

	t.Run("valid token yields principal with roles array", func(t *testing.T) {
		t.Parallel()

		claims := baseClaims(idp.URL)
		claims["roles"] = []any{"viewer", "support"}
		token := signJWT(t, key, claims)

		p := requireAuthenticated(t, auth, token)
		if p.Subject != "user-123" {
			t.Errorf("Subject = %q, want %q", p.Subject, "user-123")
		}

		if !equalStringSlice(p.Roles, []string{"viewer", "support"}) {
			t.Errorf("Roles = %v, want [viewer support]", p.Roles)
		}
	})

	t.Run("roles from space-separated claim", func(t *testing.T) {
		t.Parallel()

		claims := baseClaims(idp.URL)
		claims["roles"] = "viewer support"
		token := signJWT(t, key, claims)

		p := requireAuthenticated(t, auth, token)
		if !equalStringSlice(p.Roles, []string{"viewer", "support"}) {
			t.Errorf("Roles = %v, want [viewer support]", p.Roles)
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		t.Parallel()

		claims := baseClaims(idp.URL)
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
		token := signJWT(t, key, claims)

		requireRejected(t, auth, token, `error="invalid_token"`)
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		t.Parallel()

		claims := baseClaims(idp.URL)
		claims["aud"] = "someone-else"
		token := signJWT(t, key, claims)

		requireRejected(t, auth, token, `error="invalid_token"`)
	})

	t.Run("tampered signature is rejected", func(t *testing.T) {
		t.Parallel()

		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}

		// Signed by a key whose public half the IdP does not publish.
		token := signJWT(t, other, baseClaims(idp.URL))

		requireRejected(t, auth, token, `error="invalid_token"`)
	})

	t.Run("missing header is rejected with invalid_request", func(t *testing.T) {
		t.Parallel()

		requireRejected(t, auth, "", `error="invalid_request"`)
	})
}

func TestNewOIDCAuth_UnreachableIssuerFails(t *testing.T) {
	t.Parallel()

	_, err := server.NewOIDCAuth(config.OIDC{
		Issuer:   "http://127.0.0.1:1/does-not-exist",
		Audience: "adms",
	})
	if err == nil {
		t.Fatal("NewOIDCAuth error = nil, want a discovery failure (fail-closed)")
	}
}

// requireAuthenticated drives the token through the authenticate middleware and
// returns the Principal the next handler saw. The handler hands the Principal
// back over a buffered channel (not the response body) so the value crosses the
// goroutine boundary with proper synchronization and no JSON round-trip.
func requireAuthenticated(t *testing.T, auth server.Authenticator, token string) server.Principal {
	t.Helper()

	got := make(chan server.Principal, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := server.PrincipalFrom(r.Context())
		if !ok {
			t.Error("PrincipalFrom returned ok=false in next handler")
		}

		got <- p
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(server.Authenticate(auth, next))
	t.Cleanup(ts.Close)

	req := newRequest(t, ts.URL+"/")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (token should authenticate)", resp.StatusCode)
	}

	return <-got
}

// requireRejected drives the token (empty means no Authorization header) through
// the middleware and asserts a 401 carrying wantChallenge in WWW-Authenticate.
func requireRejected(t *testing.T, auth server.Authenticator, token, wantChallenge string) {
	t.Helper()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not run for a rejected token")
	})

	ts := httptest.NewServer(server.Authenticate(auth, next))
	t.Cleanup(ts.Close)

	req := newRequest(t, ts.URL+"/")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, wantChallenge) {
		t.Errorf("WWW-Authenticate = %q, want to contain %q", got, wantChallenge)
	}
}

// newMockIDP serves the minimal OIDC discovery document and JWKS that go-oidc
// needs to verify RS256 tokens signed by key.
func newMockIDP(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                ts.URL,
			"jwks_uri":                              ts.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "test",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})

	return ts
}

func baseClaims(issuer string) map[string]any {
	return map[string]any{
		"iss": issuer,
		"sub": "user-123",
		"aud": "adms",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

// signJWT builds a compact RS256 JWS with a static "test" kid so the mock IdP's
// JWKS can key on it.
func signJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": "test", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)

	digest := sha256.Sum256([]byte(signingInput))

	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func equalStringSlice(a, b []string) bool {
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
