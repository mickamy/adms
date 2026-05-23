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
		{
			name: "embedded relation with columns",
			in:   "posts(id,title)",
			want: []query.SelectItem{{
				Embed: &query.Embed{
					Relation: "posts",
					Items: []query.SelectItem{
						{Column: "id"},
						{Column: "title"},
					},
				},
			}},
		},
		{
			name: "embedded relation with asterisk",
			in:   "posts(*)",
			want: []query.SelectItem{{
				Embed: &query.Embed{
					Relation: "posts",
					Items:    []query.SelectItem{{Column: "*"}},
				},
			}},
		},
		{
			name: "aliased embed",
			in:   "author:users(id,name)",
			want: []query.SelectItem{{
				Alias: "author",
				Embed: &query.Embed{
					Relation: "users",
					Items: []query.SelectItem{
						{Column: "id"},
						{Column: "name"},
					},
				},
			}},
		},
		{
			name: "mix of column and embed",
			in:   "id,name,posts(id,title)",
			want: []query.SelectItem{
				{Column: "id"},
				{Column: "name"},
				{Embed: &query.Embed{
					Relation: "posts",
					Items: []query.SelectItem{
						{Column: "id"},
						{Column: "title"},
					},
				}},
			},
		},
		{
			name: "nested embed is parsed (build layer may still reject)",
			in:   "posts(id,comments(id,body))",
			want: []query.SelectItem{{
				Embed: &query.Embed{
					Relation: "posts",
					Items: []query.SelectItem{
						{Column: "id"},
						{Embed: &query.Embed{
							Relation: "comments",
							Items: []query.SelectItem{
								{Column: "id"},
								{Column: "body"},
							},
						}},
					},
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, err := query.Parse(url.Values{"select": {tt.in}})
			if err != nil {
				t.Fatalf("Parse(select=%q): %v", tt.in, err)
			}

			if !reflect.DeepEqual(q.Select, tt.want) {
				t.Errorf("Select =\n  %#v\nwant\n  %#v", q.Select, tt.want)
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
		{"aliased column without embed", "author:users", "aliased column"},
		{"empty alias", ":users(id)", "empty alias"},
		{"empty relation", "(id)", "empty relation name"},
		{"unmatched open paren in embed", "posts(id,title", "unmatched"},
		{"unmatched close paren in embed", "posts(id))", "unmatched"},
		{"relation name with space", "po sts(id)", "invalid relation name"},
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
