package dialect

type Dialect interface {
	Name() string
	Quote(ident string) string
	Placeholder(i int) string
	SupportsILIKE() bool
	SupportsReturning() bool
	JSONAgg(expr, orderBy string) string
}
