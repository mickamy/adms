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
	// StringLiteral renders s as a single-quoted SQL string literal with
	// engine-appropriate escaping (e.g., MySQL also escapes backslashes when
	// NO_BACKSLASH_ESCAPES is off, which is the default).
	StringLiteral(s string) string
	// ContainmentExpr returns an SQL boolean expression for a JSON / array
	// containment predicate. col is the already-quoted column reference,
	// valPlaceholder is the parameter placeholder for the right-hand value,
	// columnType is the column's introspected type (e.g., "jsonb",
	// "text[]", "json"), and contained switches between "col contains
	// value" (false: PostgREST's `cs`, Postgres `@>`) and "col is
	// contained in value" (true: PostgREST's `cd`, Postgres `<@`).
	// Returns an error if the dialect cannot express the operation for
	// the given column type.
	ContainmentExpr(col, valPlaceholder, columnType string, contained bool) (string, error)
}
