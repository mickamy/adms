package build_test

import (
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

func usersTable() *schema.Table {
	return &schema.Table{
		Schema: "public",
		Name:   "users",
		Columns: []schema.Column{
			{Name: "id"},
			{Name: "name"},
			{Name: "email"},
			{Name: "age"},
			{Name: "deleted_at", Nullable: true},
			{Name: "active"},
		},
	}
}

func TestSelect_Postgres(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name     string
		q        query.Query
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "default selects all",
			q:       query.Query{},
			wantSQL: `SELECT * FROM "public"."users" LIMIT 100 OFFSET 0`,
		},
		{
			name: "select flat columns",
			q: query.Query{
				Select: []query.SelectItem{{Column: "id"}, {Column: "name"}},
			},
			wantSQL: `SELECT "id", "name" FROM "public"."users" LIMIT 100 OFFSET 0`,
		},
		{
			name: "eq filter",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "42"},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "id" = $1 LIMIT 100 OFFSET 0`,
			wantArgs: []any{"42"},
		},
		{
			name: "not.eq becomes NOT (=)",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "1", Not: true},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE NOT ("id" = $1) LIMIT 100 OFFSET 0`,
			wantArgs: []any{"1"},
		},
		{
			name: "comparison operators",
			q: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalAnd,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "age", Op: query.OpGt, Value: "18"},
						query.Predicate{Column: "age", Op: query.OpLte, Value: "65"},
					},
				},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE ("age" > $1 AND "age" <= $2) LIMIT 100 OFFSET 0`,
			wantArgs: []any{"18", "65"},
		},
		{
			name: "like",
			q: query.Query{
				Filter: query.Predicate{Column: "name", Op: query.OpLike, Value: "%foo%"},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "name" LIKE $1 LIMIT 100 OFFSET 0`,
			wantArgs: []any{"%foo%"},
		},
		{
			name: "ilike uses native ILIKE on postgres",
			q: query.Query{
				Filter: query.Predicate{Column: "name", Op: query.OpILike, Value: "%foo%"},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "name" ILIKE $1 LIMIT 100 OFFSET 0`,
			wantArgs: []any{"%foo%"},
		},
		{
			name: "in expands to placeholders",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpIn, Value: "1,2,3"},
			},
			wantSQL:  `SELECT * FROM "public"."users" WHERE "id" IN ($1, $2, $3) LIMIT 100 OFFSET 0`,
			wantArgs: []any{"1", "2", "3"},
		},
		{
			name: "is null",
			q: query.Query{
				Filter: query.Predicate{Column: "deleted_at", Op: query.OpIs, Value: "null"},
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE "deleted_at" IS NULL LIMIT 100 OFFSET 0`,
		},
		{
			name: "is true",
			q: query.Query{
				Filter: query.Predicate{Column: "active", Op: query.OpIs, Value: "true"},
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE "active" IS TRUE LIMIT 100 OFFSET 0`,
		},
		{
			name: "is false",
			q: query.Query{
				Filter: query.Predicate{Column: "active", Op: query.OpIs, Value: "false"},
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE "active" IS FALSE LIMIT 100 OFFSET 0`,
		},
		{
			name: "nested and/or group",
			q: query.Query{
				Filter: query.FilterGroup{
					Op: query.LogicalOr,
					Nodes: []query.FilterNode{
						query.Predicate{Column: "id", Op: query.OpEq, Value: "1"},
						query.FilterGroup{
							Op: query.LogicalAnd,
							Nodes: []query.FilterNode{
								query.Predicate{Column: "age", Op: query.OpGt, Value: "18"},
								query.Predicate{Column: "deleted_at", Op: query.OpIs, Value: "null"},
							},
						},
					},
				},
			},
			wantSQL: `SELECT * FROM "public"."users" WHERE ` + //nolint:unqueryvet // builder output, not a runtime query
				`("id" = $1 OR ("age" > $2 AND "deleted_at" IS NULL)) LIMIT 100 OFFSET 0`,
			wantArgs: []any{"1", "18"},
		},
		{
			name: "order",
			q: query.Query{
				Order: []query.OrderItem{
					{Column: "name"},
					{Column: "id", Desc: true},
				},
			},
			wantSQL: `SELECT * FROM "public"."users" ORDER BY "name" ASC, "id" DESC LIMIT 100 OFFSET 0`,
		},
		{
			name: "limit and offset",
			q: query.Query{
				Limit:  new(20),
				Offset: new(40),
			},
			wantSQL: `SELECT * FROM "public"."users" LIMIT 20 OFFSET 40`,
		},
		{
			name: "limit clamps to max",
			q: query.Query{
				Limit: new(5000),
			},
			wantSQL: `SELECT * FROM "public"."users" LIMIT 1000 OFFSET 0`,
		},
		{
			name: "limit zero clamps to one",
			q: query.Query{
				Limit: new(0),
			},
			wantSQL: `SELECT * FROM "public"."users" LIMIT 1 OFFSET 0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, _, err := build.Select(tt.q, table, nil, d, 100, 1000)
			if err != nil {
				t.Fatalf("Select: %v", err)
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

func TestSelect_MySQL(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	table := usersTable()

	tests := []struct {
		name     string
		q        query.Query
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "default uses MySQL identifier quoting",
			q:       query.Query{},
			wantSQL: "SELECT * FROM `public`.`users` LIMIT 100 OFFSET 0",
		},
		{
			name: "eq filter uses ? placeholder",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpEq, Value: "42"},
			},
			wantSQL:  "SELECT * FROM `public`.`users` WHERE `id` = ? LIMIT 100 OFFSET 0",
			wantArgs: []any{"42"},
		},
		{
			name: "ilike falls back to LOWER LIKE LOWER",
			q: query.Query{
				Filter: query.Predicate{Column: "name", Op: query.OpILike, Value: "%FOO%"},
			},
			wantSQL:  "SELECT * FROM `public`.`users` WHERE LOWER(`name`) LIKE LOWER(?) LIMIT 100 OFFSET 0",
			wantArgs: []any{"%FOO%"},
		},
		{
			name: "in expands to comma-separated placeholders",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpIn, Value: "1,2,3"},
			},
			wantSQL:  "SELECT * FROM `public`.`users` WHERE `id` IN (?, ?, ?) LIMIT 100 OFFSET 0",
			wantArgs: []any{"1", "2", "3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, _, err := build.Select(tt.q, table, nil, d, 100, 1000)
			if err != nil {
				t.Fatalf("Select: %v", err)
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

func TestSelect_TableWithoutSchema(t *testing.T) {
	t.Parallel()

	table := &schema.Table{
		Name:    "items",
		Columns: []schema.Column{{Name: "id"}},
	}

	sql, _, _, err := build.Select(query.Query{}, table, nil, dialect.Postgres(), 100, 1000)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	want := `SELECT * FROM "items" LIMIT 100 OFFSET 0` //nolint:unqueryvet // builder output, not a runtime query
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
}

func TestSelect_RejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name         string
		defaultLimit int
		maxLimit     int
	}{
		{"defaultLimit zero", 0, 1000},
		{"defaultLimit negative", -1, 1000},
		{"maxLimit zero", 100, 0},
		{"maxLimit negative", 100, -1},
		{"defaultLimit exceeds maxLimit", 200, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := build.Select(query.Query{}, table, nil, d, tt.defaultLimit, tt.maxLimit)
			if err == nil {
				t.Errorf("Select: expected error, got nil")
			}
		})
	}
}

func TestSelect_Errors(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name string
		q    query.Query
	}{
		{
			name: "unknown column in select",
			q: query.Query{
				Select: []query.SelectItem{{Column: "ghost"}},
			},
		},
		{
			name: "unknown column in filter",
			q: query.Query{
				Filter: query.Predicate{Column: "ghost", Op: query.OpEq, Value: "1"},
			},
		},
		{
			name: "unknown column in order",
			q: query.Query{
				Order: []query.OrderItem{{Column: "ghost"}},
			},
		},
		{
			name: "in with empty element",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpIn, Value: "1,,3"},
			},
		},
		{
			name: "unknown operator triggers internal default branch",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.Operator(99), Value: "1"},
			},
		},
		{
			name: "select cannot mix * with named columns",
			q: query.Query{
				Select: []query.SelectItem{{Column: "*"}, {Column: "age"}},
			},
		},
		{
			name: "select cannot repeat * twice",
			q: query.Query{
				Select: []query.SelectItem{{Column: "*"}, {Column: "*"}},
			},
		},
		{
			name: "select cannot list the same column twice",
			q: query.Query{
				Select: []query.SelectItem{{Column: "id"}, {Column: "id"}},
			},
		},
		{
			name: "invalid is literal triggers internal default branch",
			q: query.Query{
				Filter: query.Predicate{Column: "id", Op: query.OpIs, Value: "maybe"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := build.Select(tt.q, table, nil, d, 100, 1000)
			if err == nil {
				t.Errorf("Select: expected error, got nil")
			}
		})
	}
}
