package build_test

import (
	"reflect"
	"testing"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/dialect"
)

func TestInsert_Postgres(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name          string
		rows          []map[string]any
		withReturning bool
		wantSQL       string
		wantArgs      []any
	}{
		{
			name: "single row sorts keys deterministically",
			rows: []map[string]any{
				{"name": "alice", "age": 30},
			},
			wantSQL:  `INSERT INTO "public"."users" ("age", "name") VALUES ($1, $2)`,
			wantArgs: []any{30, "alice"},
		},
		{
			name: "single row with RETURNING",
			rows: []map[string]any{
				{"id": 1, "name": "alice"},
			},
			withReturning: true,
			wantSQL:       `INSERT INTO "public"."users" ("id", "name") VALUES ($1, $2) RETURNING *`,
			wantArgs:      []any{1, "alice"},
		},
		{
			name: "bulk insert reuses column list",
			rows: []map[string]any{
				{"id": 1, "name": "alice"},
				{"id": 2, "name": "bob"},
			},
			wantSQL:  `INSERT INTO "public"."users" ("id", "name") VALUES ($1, $2), ($3, $4)`,
			wantArgs: []any{1, "alice", 2, "bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, err := build.Insert(table, tt.rows, d, tt.withReturning)
			if err != nil {
				t.Fatalf("Insert: %v", err)
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

func TestInsert_MySQL(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	table := usersTable()

	t.Run("uses backtick quotes and ? placeholders", func(t *testing.T) {
		t.Parallel()

		sql, args, err := build.Insert(table, []map[string]any{
			{"id": 1, "name": "alice"},
		}, d, false)
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}

		want := "INSERT INTO `public`.`users` (`id`, `name`) VALUES (?, ?)"
		if sql != want {
			t.Errorf("sql =\n  %s\nwant\n  %s", sql, want)
		}

		if !reflect.DeepEqual(args, []any{1, "alice"}) {
			t.Errorf("args = %#v", args)
		}
	})

	t.Run("withReturning is ignored on dialects without RETURNING", func(t *testing.T) {
		t.Parallel()

		sql, _, err := build.Insert(table, []map[string]any{
			{"id": 1, "name": "alice"},
		}, d, true)
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}

		if got := sql; got[len(got)-len(" RETURNING *"):] == " RETURNING *" {
			t.Errorf("MySQL Insert should not append RETURNING *; got %s", sql)
		}
	})
}

func TestInsert_Errors(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	table := usersTable()

	tests := []struct {
		name string
		rows []map[string]any
	}{
		{
			name: "rows is empty",
			rows: nil,
		},
		{
			name: "row has no columns",
			rows: []map[string]any{{}},
		},
		{
			name: "unknown column",
			rows: []map[string]any{{"ghost": 1}},
		},
		{
			name: "bulk row has fewer keys",
			rows: []map[string]any{
				{"id": 1, "name": "alice"},
				{"id": 2},
			},
		},
		{
			name: "bulk row has different keys",
			rows: []map[string]any{
				{"id": 1, "name": "alice"},
				{"id": 2, "email": "bob@example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := build.Insert(table, tt.rows, d, false)
			if err == nil {
				t.Errorf("Insert: expected error, got nil")
			}
		})
	}
}
