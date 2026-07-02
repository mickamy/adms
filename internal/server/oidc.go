package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/mickamy/adms/internal/config"
)

var _ Authenticator = oidcAuth{}

// oidcDiscoveryTimeout bounds both the startup discovery round-trip and every
// later JWKS refresh. It is enforced via the HTTP client rather than a
// cancelable context because go-oidc retains the provider's context for
// background key rotation, so that context must outlive startup.
const oidcDiscoveryTimeout = 10 * time.Second

// oidcAuth validates OIDC/JWT bearer tokens. The verifier checks the
// signature (against the issuer's JWKS), issuer, audience, and expiry; roles
// are read from the configured claim.
type oidcAuth struct {
	verifier   *oidc.IDTokenVerifier
	rolesClaim string
}

func newOIDCAuth(cfg config.OIDC) (oidcAuth, error) {
	client := &http.Client{Timeout: oidcDiscoveryTimeout}
	ctx := oidc.ClientContext(context.Background(), client)

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return oidcAuth{}, fmt.Errorf("oidc: discover issuer %q: %w", cfg.Issuer, err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.Audience})

	return oidcAuth{verifier: verifier, rolesClaim: cfg.RolesClaim}, nil
}

func (a oidcAuth) Authenticate(r *http.Request) (Principal, error) {
	raw, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return Principal{}, &authError{
			wwwAuthenticate: `Bearer realm="adms", error="invalid_request"`,
			detail:          "request requires a Bearer token in the Authorization header",
		}
	}

	tok, err := a.verifier.Verify(r.Context(), raw)
	if err != nil {
		return Principal{}, &authError{
			wwwAuthenticate: `Bearer realm="adms", error="invalid_token"`,
			detail:          "the Authorization token is invalid",
		}
	}

	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return Principal{}, &authError{
			wwwAuthenticate: `Bearer realm="adms", error="invalid_token"`,
			detail:          "the token claims could not be parsed",
		}
	}

	return Principal{
		Subject: tok.Subject,
		Roles:   extractRoles(claims, a.rolesClaim),
		Claims:  claims,
	}, nil
}

// extractRoles reads the roles claim, accepting either a JSON array of strings
// or a single space-separated string (both are common in the wild). An empty
// claim name, a missing claim, or an unexpected shape yields no roles.
func extractRoles(claims map[string]any, claimName string) []string {
	if claimName == "" {
		return nil
	}

	switch v := claims[claimName].(type) {
	case []any:
		roles := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				roles = append(roles, s)
			}
		}

		return roles
	case string:
		return strings.Fields(v)
	default:
		return nil
	}
}
