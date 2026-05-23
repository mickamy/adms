package query_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/query"
)

func TestParse_OK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   url.Values
		want query.Query
	}{
		{
			name: "empty",
			in:   url.Values{},
			want: query.Query{},
		},
		{
			name: "single eq filter",
			in:   url.Values{"id": {"eq.42"}},
			want: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "42"},
			},
		},
		{
			name: "not.eq",
			in:   url.Values{"status": {"not.eq.archived"}},
			want: query.Query{
				Filter: query.Predicate{Column: "status", Op: query.OpEq, Value: "archived", Not: true},
			},
		},
		{
			name: "is null",
			in:   url.Values{"deleted_at": {"is.null"}},
			want: query.Query{
				Filter: query.Predicate{Column: "deleted_at", Op: query.OpIs, Value: "null"},
			},
		},
		{
			name: "in strips parens",
			in:   url.Values{"id": {"in.(1,2,3)"}},
			want: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpIn, Value: "1,2,3"},
			},
		},
		{
			name: "multiple filters AND grouped",
			in: url.Values{
				"status": {"eq.active"},
				"age":    {"gt.18"},
			},
			want: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalAnd,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "age", Op: query.OpGt, Value: "18"},
						query.Predicate{Column: "status", Op: query.OpEq, Value: "active"},
					},
				},
			},
		},
		{
			name: "same column repeated AND grouped",
			in:   url.Values{"age": {"gt.18", "lt.65"}},
			want: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalAnd,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "age", Op: query.OpGt, Value: "18"},
						query.Predicate{Column: "age", Op: query.OpLt, Value: "65"},
					},
				},
			},
		},
		{
			name: "or group flat",
			in:   url.Values{"or": {"(status.eq.active,status.eq.pending)"}},
			want: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalOr,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "status", Op: query.OpEq, Value: "active"},
						query.Predicate{Column: "status", Op: query.OpEq, Value: "pending"},
					},
				},
			},
		},
		{
			name: "nested and inside or",
			in:   url.Values{"or": {"(a.eq.1,and=(b.gt.0,c.is.null))"}},
			want: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalOr,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "a", Op: query.OpEq, Value: "1"},
						query.FilterGroup{
							Op: query.LogicalAnd,
							Nodes: []query.FilterNode{
								query.Predicate{Column: "b", Op: query.OpGt, Value: "0"},
								query.Predicate{Column: "c", Op: query.OpIs, Value: "null"},
							},
						},
					},
				},
			},
		},
		{
			name: "select flat",
			in:   url.Values{"select": {"id,name"}},
			want: query.Query{
				Select: []query.SelectItem{{Column: "id"}, {Column: "name"}},
			},
		},
		{
			name: "order asc and desc",
			in:   url.Values{"order": {"name.asc,created_at.desc"}},
			want: query.Query{
				Order: []query.OrderItem{
					{Column: "name", Desc: false},
					{Column: "created_at", Desc: true},
				},
			},
		},
		{
			name: "order without direction defaults to asc",
			in:   url.Values{"order": {"name"}},
			want: query.Query{
				Order: []query.OrderItem{{Column: "name", Desc: false}},
			},
		},
		{
			name: "limit and offset",
			in:   url.Values{"limit": {"20"}, "offset": {"40"}},
			want: query.Query{
				Limit:  new(20),
				Offset: new(40),
			},
		},
		{
			name: "kitchen sink",
			in: url.Values{
				"select": {"id,name,status"},
				"status": {"eq.active"},
				"order":  {"created_at.desc"},
				"limit":  {"10"},
				"offset": {"0"},
			},
			want: query.Query{
				Select: []query.SelectItem{{Column: "id"}, {Column: "name"}, {Column: "status"}},
				Filter: query.Predicate{Column: "status", Op: query.OpEq, Value: "active"},
				Order:  []query.OrderItem{{Column: "created_at", Desc: true}},
				Limit:  new(10),
				Offset: new(0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := query.Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%v): unexpected error: %v", tt.in, err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%v) =\n  %#v\nwant\n  %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParse_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   url.Values
	}{
		{"unknown operator", url.Values{"id": {"bogus.42"}}},
		{"missing operator separator", url.Values{"id": {"42"}}},
		{"invalid is value", url.Values{"deleted_at": {"is.maybe"}}},
		{"in value missing parens", url.Values{"id": {"in.1,2,3"}}},
		{"in empty list", url.Values{"id": {"in.()"}}},
		{"limit non-integer", url.Values{"limit": {"abc"}}},
		{"limit negative", url.Values{"limit": {"-1"}}},
		{"offset non-integer", url.Values{"offset": {"abc"}}},
		{"offset negative", url.Values{"offset": {"-5"}}},
		{"select embedded rejected", url.Values{"select": {"posts(id)"}}},
		{"or group missing parens", url.Values{"or": {"a.eq.1,b.eq.2"}}},
		{"or group unmatched open", url.Values{"or": {"(a.eq.1"}}},
		{"or group unmatched close", url.Values{"or": {"a.eq.1)"}}},
		{"nested group unmatched", url.Values{"or": {"(a.eq.1,and=(b.eq.2)"}}},
		{"order empty item", url.Values{"order": {"name,,id"}}},
		{"order suffix not asc or desc", url.Values{"order": {"name.foo"}}},
		{"order extra dots", url.Values{"order": {"name.asc.desc"}}},
		{"duplicate select", url.Values{"select": {"id", "name"}}},
		{"duplicate order", url.Values{"order": {"id.asc", "name.asc"}}},
		{"duplicate limit", url.Values{"limit": {"10", "20"}}},
		{"duplicate offset", url.Values{"offset": {"0", "10"}}},
		{"empty column", url.Values{"": {"eq.1"}}},
		{"group element with empty column", url.Values{"or": {"(.eq.1,b.eq.2)"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := query.Parse(tt.in); err == nil {
				t.Errorf("Parse(%v): expected error, got nil", tt.in)
			}
		})
	}
}
