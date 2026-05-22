package query

// Query is the parsed form of a single request's query parameters.
// It carries only syntactic shape; identifier validation against the
// schema and limit clamping are the caller's responsibility.
type Query struct {
	Select []SelectItem
	Filter FilterNode
	Order  []OrderItem
	Limit  *int
	Offset *int
}

// SelectItem is one element of a select clause. Phase 3 fills only
// Column (a flat column name or "*"); Alias and Embed are reserved
// for Phase 4 relation embedding.
type SelectItem struct {
	Column string
	Alias  string
	Embed  *Embed
}

// Embed describes a nested relation projection. Populated in Phase 4.
type Embed struct {
	Relation string
	Items    []SelectItem
}

// OrderItem is one element of an order clause.
type OrderItem struct {
	Column string
	Desc   bool
}

// FilterNode is a node in the filter tree: either a Predicate (leaf) or
// a FilterGroup (branch). The sealing method keeps the union closed to
// this package.
type FilterNode interface {
	filterNode()
}

// FilterGroup combines child nodes under a logical connective.
type FilterGroup struct {
	Op    LogicalOp
	Nodes []FilterNode
}

func (FilterGroup) filterNode() {}

// Predicate is a leaf filter expression of the form "column op value".
// For OpIn the Value is the comma-separated list with the surrounding
// parentheses already stripped. For OpIs the Value is one of "null",
// "true", "false".
type Predicate struct {
	Column string
	Op     Operator
	Value  string
	Not    bool
}

func (Predicate) filterNode() {}

// LogicalOp is a logical connective for FilterGroup.
type LogicalOp int

const (
	LogicalAnd LogicalOp = iota + 1
	LogicalOr
)

func (op LogicalOp) String() string {
	switch op {
	case LogicalAnd:
		return "and"
	case LogicalOr:
		return "or"
	default:
		return "?"
	}
}
