package server

import "net/http"

// corsAllowedMethods / corsAllowedHeaders mirror the set of methods and
// headers adms accepts on its data routes. Keep these in sync with router.go
// (methods) and the write / read handlers (headers).
//
// corsExposeHeaders lists response headers a browser JS client may read via
// `response.headers.get(...)`. Content-Range is non-simple and must be
// explicitly exposed so the UI (and any other browser caller) can use the
// write API's `Prefer: count=exact` paging information.
const (
	corsAllowedMethods = "GET, POST, PATCH, DELETE, OPTIONS"
	corsAllowedHeaders = "Authorization, Content-Type, Prefer"
	corsExposeHeaders  = "Content-Range"
	corsMaxAge         = "86400"
)

// cors returns a middleware that handles CORS for the adms API.
//
//   - Empty origins → the middleware is a no-op (returns next unchanged).
//   - No Origin header → request passes through without CORS headers.
//   - Allowed origin → Access-Control-Allow-Origin echoes the request's
//     Origin and Vary: Origin is added so caches do not serve the response
//     to a different origin.
//   - Disallowed origin → Vary: Origin is added but no Allow-Origin; the
//     browser is expected to block the response, while non-browser callers
//     still see whatever the next handler returns.
//   - OPTIONS preflight (allowed origin + Access-Control-Request-Method) is
//     short-circuited with 204 + Allow-Methods / Allow-Headers / Max-Age.
//     The middleware sits outside the bearer-auth middleware so preflight
//     requests, which carry no Authorization header, do not get rejected
//     before the browser can issue the real request.
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

		// Add (not Set) so future middleware that wants to extend Vary with
		// other dimensions (e.g., Accept-Encoding) can append rather than
		// overwrite the Origin entry we own here.
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
