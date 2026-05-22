package query_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/query"
)

func TestSelect_OK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []query.SelectItem
	}{
		{"single column", "id", []query.SelectItem{{Column: "id"}}},
		{"multiple columns", "id,name,created_at", []query.SelectItem{
			{Column: "id"},
			{Column: "name"},
			{Column: "created_at"},
		}},
		{"asterisk", "*", []query.SelectItem{{Column: "*"}}},
		{"trims whitespace", " id , name ", []query.SelectItem{
			{Column: "id"},
			{Column: "name"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, err := query.Parse(url.Values{"select": {tt.in}})
			if err != nil {
				t.Fatalf("Parse(select=%q): %v", tt.in, err)
			}

			if !reflect.DeepEqual(q.Select, tt.want) {
				t.Errorf("Select = %#v, want %#v", q.Select, tt.want)
			}
		})
	}
}

func TestSelect_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{"trailing comma", "id,"},
		{"leading comma", ",id"},
		{"double comma", "id,,name"},
		{"embedded relation", "posts(id,title)"},
		{"aliased column", "author:users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := query.Parse(url.Values{"select": {tt.in}}); err == nil {
				t.Errorf("Parse(select=%q): expected error, got nil", tt.in)
			}
		})
	}
}
