package schema

type tableKey struct {
	Schema string
	Name   string
}

type fkDirection int

const (
	fkDirectionForward fkDirection = iota
	fkDirectionReverse
)
