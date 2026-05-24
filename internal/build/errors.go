package build

// FilterError marks a builder failure that originated from the WHERE
// clause (unknown column in a filter, malformed predicate, etc.). HTTP
// handlers can route this to "invalid-query" responses so query-parameter
// mistakes don't get misreported as body errors. Non-FilterError build
// failures relate to the SET/body and map to "invalid-body".
type FilterError struct {
	Err error
}

func (e *FilterError) Error() string { return e.Err.Error() }
func (e *FilterError) Unwrap() error { return e.Err }
