package dialect

type Dialect interface {
	Name() string
	Quote(ident string) string
	Placeholder(i int) string
	SupportsILIKE() bool
	SupportsReturning() bool
	JSONAgg(expr, orderBy string) string
	// JSONObject formats a JSON object literal from alternating key / value
	// expressions: pairs[0] is the first key, pairs[1] is its expression, and
	// so on. Keys must already be quoted as SQL literals (e.g., 'id').
	JSONObject(pairs []string) string
	// EmptyJSONArray returns a SQL expression equivalent to an empty JSON
	// array, used as a fallback when an aggregated subquery has no rows.
	EmptyJSONArray() string
}
