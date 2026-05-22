package query_test

import (
	"net/url"
	"testing"

	"github.com/mickamy/adms/internal/query"
)

func TestOperatorString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   query.Operator
		want string
	}{
		{query.OpEq, "eq"},
		{query.OpNeq, "neq"},
		{query.OpGt, "gt"},
		{query.OpGte, "gte"},
		{query.OpLt, "lt"},
		{query.OpLte, "lte"},
		{query.OpLike, "like"},
		{query.OpILike, "ilike"},
		{query.OpIn, "in"},
		{query.OpIs, "is"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := tt.op.String(); got != tt.want {
				t.Errorf("Operator(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}

	if got := query.Operator(0).String(); got != "Operator(0)" {
		t.Errorf("Operator(0).String() = %q, want %q", got, "Operator(0)")
	}
}

func TestParseOperatorRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		op        query.Operator
		urlValue  string
		wantValue string
	}{
		{"eq", query.OpEq, "eq.42", "42"},
		{"neq", query.OpNeq, "neq.42", "42"},
		{"gt", query.OpGt, "gt.0", "0"},
		{"gte", query.OpGte, "gte.0", "0"},
		{"lt", query.OpLt, "lt.100", "100"},
		{"lte", query.OpLte, "lte.100", "100"},
		{"like", query.OpLike, "like.%foo%", "%foo%"},
		{"ilike", query.OpILike, "ilike.%foo%", "%foo%"},
		{"in", query.OpIn, "in.(1,2,3)", "1,2,3"},
		{"is", query.OpIs, "is.null", "null"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, err := query.Parse(url.Values{"col": {tt.urlValue}})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			p, ok := q.Filter.(query.Predicate)
			if !ok {
				t.Fatalf("Filter = %T, want Predicate", q.Filter)
			}

			if p.Op != tt.op {
				t.Errorf("Op = %v, want %v", p.Op, tt.op)
			}

			if p.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", p.Value, tt.wantValue)
			}
		})
	}
}

func TestLogicalOpString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   query.LogicalOp
		want string
	}{
		{query.LogicalAnd, "and"},
		{query.LogicalOr, "or"},
		{query.LogicalOp(0), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := tt.op.String(); got != tt.want {
				t.Errorf("LogicalOp(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}
