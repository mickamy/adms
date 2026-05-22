package query

import "fmt"

// Operator is a PostgREST-style filter operator.
type Operator int

const (
	OpEq Operator = iota + 1
	OpNeq
	OpGt
	OpGte
	OpLt
	OpLte
	OpLike
	OpILike
	OpIn
	OpIs
)

var operatorNames = map[Operator]string{
	OpEq:    "eq",
	OpNeq:   "neq",
	OpGt:    "gt",
	OpGte:   "gte",
	OpLt:    "lt",
	OpLte:   "lte",
	OpLike:  "like",
	OpILike: "ilike",
	OpIn:    "in",
	OpIs:    "is",
}

var operatorByName = func() map[string]Operator {
	m := make(map[string]Operator, len(operatorNames))
	for op, name := range operatorNames {
		m[name] = op
	}
	return m
}()

func (op Operator) String() string {
	if name, ok := operatorNames[op]; ok {
		return name
	}
	return fmt.Sprintf("Operator(%d)", int(op))
}

func parseOperator(s string) (Operator, error) {
	op, ok := operatorByName[s]
	if !ok {
		return 0, fmt.Errorf("unknown operator %q", s)
	}
	return op, nil
}
