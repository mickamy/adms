package build_test

import (
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
)

func TestDelete_Postgres(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name          string
		q             query.Query
		withReturning bool
		wantSQL       string
		wantArgs      []any
	}{
		{
			name: "with filter",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
			},
			wantSQL:  `DELETE FROM "public"."users" WHERE "id" = $1`,
			wantArgs: []any{"1"},
		},
		{
			name: "with filter and RETURNING",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
			},
			withReturning: true,
			wantSQL:       `DELETE FROM "public"."users" WHERE "id" = $1 RETURNING *`,
			wantArgs:      []any{"1"},
		},
		{
			name:    "no filter omits WHERE",
			q:       query.Query{},
			wantSQL: `DELETE FROM "public"."users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, err := build.Delete(table, tt.q, d, tt.withReturning)
			if err != nil {
				t.Fatalf("Delete: %v", err)
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

func TestDelete_MySQL(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	table := usersTable()

	sql, args, err := build.Delete(table,
		query.Query{
			Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
		},
		d, true)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := "DELETE FROM `public`.`users` WHERE `id` = ?"
	if sql != want {
		t.Errorf("sql =\n  %s\nwant\n  %s", sql, want)
	}

	if !reflect.DeepEqual(args, []any{"1"}) {
		t.Errorf("args = %#v", args)
	}
}

func TestDelete_Errors(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	_, _, err := build.Delete(table,
		query.Query{
			Filter: query.Predicate{Column: "ghost", Op: query.OpEq, Value: "1"},
		},
		d, false)
	if err == nil {
		t.Errorf("Delete: expected error, got nil")
	}
}
