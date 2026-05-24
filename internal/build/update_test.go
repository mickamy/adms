package build_test

import (
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
)

func TestUpdate_Postgres(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name          string
		set           map[string]any
		q             query.Query
		withReturning bool
		wantSQL       string
		wantArgs      []any
	}{
		{
			name: "set with filter",
			set:  map[string]any{"name": "alice2"},
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
			},
			wantSQL:  `UPDATE "public"."users" SET "name" = $1 WHERE "id" = $2`,
			wantArgs: []any{"alice2", "1"},
		},
		{
			name: "set with filter and RETURNING",
			set:  map[string]any{"active": false},
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
			},
			withReturning: true,
			wantSQL:       `UPDATE "public"."users" SET "active" = $1 WHERE "id" = $2 RETURNING *`,
			wantArgs:      []any{false, "1"},
		},
		{
			name: "multi-column set sorts keys",
			set:  map[string]any{"name": "alice2", "age": 31},
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
			},
			wantSQL:  `UPDATE "public"."users" SET "age" = $1, "name" = $2 WHERE "id" = $3`,
			wantArgs: []any{31, "alice2", "1"},
		},
		{
			name:     "no filter omits WHERE",
			set:      map[string]any{"active": true},
			q:        query.Query{},
			wantSQL:  `UPDATE "public"."users" SET "active" = $1`,
			wantArgs: []any{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, err := build.Update(table, tt.set, tt.q, d, tt.withReturning)
			if err != nil {
				t.Fatalf("Update: %v", err)
			}

			if sql != tt.wantSQL {
				t.Errorf("sql =\n  %s\nwant\n  %s", sql, tt.wantSQL)
			}

			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, tt.wantArgs)
			}
		})
	}
}

func TestUpdate_MySQL(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	table := usersTable()

	sql, args, err := build.Update(table,
		map[string]any{"name": "alice2"},
		query.Query{
			Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
		},
		d, true)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	want := "UPDATE `public`.`users` SET `name` = ? WHERE `id` = ?"
	if sql != want {
		t.Errorf("sql =\n  %s\nwant\n  %s", sql, want)
	}

	if !reflect.DeepEqual(args, []any{"alice2", "1"}) {
		t.Errorf("args = %#v", args)
	}
}

func TestUpdate_Errors(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name string
		set  map[string]any
		q    query.Query
	}{
		{
			name: "set is empty",
			set:  map[string]any{},
		},
		{
			name: "unknown column in set",
			set:  map[string]any{"ghost": 1},
		},
		{
			name: "unknown column in filter",
			set:  map[string]any{"name": "alice2"},
			q: query.Query{
				Filter: query.Predicate{Column: "ghost", Op: query.OpEq, Value: "1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := build.Update(table, tt.set, tt.q, d, false)
			if err == nil {
				t.Errorf("Update: expected error, got nil")
			}
		})
	}
}
