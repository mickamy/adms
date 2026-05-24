package server

import "net/http"

// Keep corsAllowedMethods / corsAllowedHeaders in sync with router.go and
// the read / write handlers; nothing enforces the link statically.
// corsExposeHeaders must list Content-Range explicitly because it is not a
// CORS-safelisted response header and browser JS otherwise cannot read it.
const (
	corsAllowedMethods = "GET, POST, PATCH, DELETE, OPTIONS"
	corsAllowedHeaders = "Authorization, Content-Type, Prefer"
	corsExposeHeaders  = "Content-Range"
	corsMaxAge         = "86400"
)

// cors must sit outside the bearer-auth middleware so OPTIONS preflight,
// which carries no Authorization header, can be answered before auth runs.
// Disallowed origins fall through to next without Allow-Origin; the browser
// blocks the response while non-browser callers still see it.
func cors(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}

	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)

			return
		}

		// Add (not Set) so later middleware can append other Vary entries.
		w.Header().Add("Vary", "Origin")

		if _, ok := allowed[origin]; !ok {
			next.ServeHTTP(w, r)

			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Expose-Headers", corsExposeHeaders)

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
			w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
			w.Header().Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)

			return
		}

		next.ServeHTTP(w, r)
	})
}
