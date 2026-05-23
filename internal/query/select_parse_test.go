package query_test

import (
	"net/url"
	"reflect"
	"strings"
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
		{"whitespace-only select yields no items", "   ", nil},
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
		name    string
		in      string
		wantSub string
	}{
		{"trailing comma", "id,", "empty select item"},
		{"leading comma", ",id", "empty select item"},
		{"double comma", "id,,name", "empty select item"},
		{"embedded relation", "posts(id,title)", "embedded select"},
		{"aliased column", "author:users", "aliased select"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := query.Parse(url.Values{"select": {tt.in}})
			if err == nil {
				t.Fatalf("Parse(select=%q): expected error, got nil", tt.in)
			}

			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Parse(select=%q) error = %q, want it to contain %q", tt.in, err.Error(), tt.wantSub)
			}
		})
	}
}
