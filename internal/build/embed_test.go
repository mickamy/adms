package build_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mickamy/adms/internal/build"
	"github.com/mickamy/adms/internal/dialect"
	"github.com/mickamy/adms/internal/query"
	"github.com/mickamy/adms/internal/schema"
)

type testLookup map[string]*schema.Table

func (l testLookup) Table(name string) (*schema.Table, bool) {
	t, ok := l[name]
	return t, ok
}

func usersAndPostsLookup() testLookup {
	users := &schema.Table{
		Schema:     "public",
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id"},
			{Name: "name"},
			{Name: "email"},
		},
	}

	posts := &schema.Table{
		Schema:     "public",
		Name:       "posts",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id"},
			{Name: "user_id"},
			{Name: "title"},
		},
		ForeignKeys: []schema.ForeignKey{
			{Table: "public.users", Columns: []string{"user_id"}, References: []string{"id"}},
		},
	}

	return testLookup{"users": users, "posts": posts}
}

func TestSelect_EmbedPostgres(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	lookup := usersAndPostsLookup()
	users, _ := lookup.Table("users")
	posts, _ := lookup.Table("posts")

	tests := []struct {
		name    string
		parent  *schema.Table
		q       query.Query
		wantSQL string
	}{
		{
			name:   "one-to-many embed with named columns",
			parent: users,
			q: query.Query{
				Select: []query.SelectItem{
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
			wantSQL: `SELECT "id", "name", (SELECT COALESCE(json_agg(json_build_object(` +
				`'id', "posts"."id", 'title', "posts"."title") ORDER BY "posts"."id"), '[]'::json) ` +
				`FROM "public"."posts" WHERE "posts"."user_id" = "users"."id") AS "posts" ` +
				`FROM "public"."users" LIMIT 100 OFFSET 0`,
		},
		{
			name:   "one-to-many embed with asterisk expands all child columns",
			parent: users,
			q: query.Query{
				Select: []query.SelectItem{
					{Column: "id"},
					{Embed: &query.Embed{
						Relation: "posts",
						Items:    []query.SelectItem{{Column: "*"}},
					}},
				},
			},
			wantSQL: `SELECT "id", (SELECT COALESCE(json_agg(json_build_object(` +
				`'id', "posts"."id", 'user_id', "posts"."user_id", 'title', "posts"."title") ` +
				`ORDER BY "posts"."id"), '[]'::json) ` +
				`FROM "public"."posts" WHERE "posts"."user_id" = "users"."id") AS "posts" ` +
				`FROM "public"."users" LIMIT 100 OFFSET 0`,
		},
		{
			name:   "many-to-one embed with alias",
			parent: posts,
			q: query.Query{
				Select: []query.SelectItem{
					{Column: "id"},
					{Column: "title"},
					{Alias: "author", Embed: &query.Embed{
						Relation: "users",
						Items: []query.SelectItem{
							{Column: "id"},
							{Column: "name"},
						},
					}},
				},
			},
			wantSQL: `SELECT "id", "title", (SELECT json_build_object(` +
				`'id', "users"."id", 'name', "users"."name") ` +
				`FROM "public"."users" WHERE "users"."id" = "posts"."user_id" LIMIT 1) AS "author" ` +
				`FROM "public"."posts" LIMIT 100 OFFSET 0`,
		},
		{
			name:   "embed with no inner items defaults to every child column",
			parent: users,
			q: query.Query{
				Select: []query.SelectItem{
					{Column: "id"},
					{Embed: &query.Embed{Relation: "posts"}},
				},
			},
			wantSQL: `SELECT "id", (SELECT COALESCE(json_agg(json_build_object(` +
				`'id', "posts"."id", 'user_id', "posts"."user_id", 'title', "posts"."title") ` +
				`ORDER BY "posts"."id"), '[]'::json) ` +
				`FROM "public"."posts" WHERE "posts"."user_id" = "users"."id") AS "posts" ` +
				`FROM "public"."users" LIMIT 100 OFFSET 0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sql, args, _, err := build.Select(tt.q, tt.parent, lookup, d, 100, 1000)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}

			if sql != tt.wantSQL {
				t.Errorf("sql =\n  %s\nwant\n  %s", sql, tt.wantSQL)
			}

			if !reflect.DeepEqual(args, []any(nil)) {
				t.Errorf("args = %#v, want nil", args)
			}
		})
	}
}

func TestSelect_EmbedEscapesSingleQuoteInColumnName(t *testing.T) {
	t.Parallel()

	users := &schema.Table{
		Schema:     "public",
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns:    []schema.Column{{Name: "id"}},
	}
	posts := &schema.Table{
		Schema:     "public",
		Name:       "posts",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id"},
			{Name: "user_id"},
			{Name: "o'brien"},
		},
		ForeignKeys: []schema.ForeignKey{
			{Table: "public.users", Columns: []string{"user_id"}, References: []string{"id"}},
		},
	}
	lookup := testLookup{"users": users, "posts": posts}

	q := query.Query{
		Select: []query.SelectItem{{
			Embed: &query.Embed{
				Relation: "posts",
				Items: []query.SelectItem{
					{Column: "id"},
					{Column: "o'brien"},
				},
			},
		}},
	}

	sql, _, _, err := build.Select(q, users, lookup, dialect.Postgres(), 100, 1000)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	if !strings.Contains(sql, "'o''brien'") {
		t.Errorf("SQL = %s\nwant it to contain 'o''brien' (escaped single quote)", sql)
	}
}

func TestSelect_EmbedMySQL(t *testing.T) {
	t.Parallel()

	d := dialect.MySQL()
	lookup := usersAndPostsLookup()
	users, _ := lookup.Table("users")

	q := query.Query{
		Select: []query.SelectItem{
			{Column: "id"},
			{Embed: &query.Embed{
				Relation: "posts",
				Items: []query.SelectItem{
					{Column: "id"},
					{Column: "title"},
				},
			}},
		},
	}

	wantSQL := "SELECT `id`, " +
		"(SELECT COALESCE(JSON_ARRAYAGG(JSON_OBJECT('id', `posts`.`id`, 'title', `posts`.`title`)), JSON_ARRAY()) " +
		"FROM `public`.`posts` WHERE `posts`.`user_id` = `users`.`id`) AS `posts` " +
		"FROM `public`.`users` LIMIT 100 OFFSET 0"

	sql, _, _, err := build.Select(q, users, lookup, d, 100, 1000)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	if sql != wantSQL {
		t.Errorf("sql =\n  %s\nwant\n  %s", sql, wantSQL)
	}
}

func TestSelect_EmbedErrors(t *testing.T) {
	t.Parallel()

	d := dialect.Postgres()
	lookup := usersAndPostsLookup()
	users, _ := lookup.Table("users")

	standalone := &schema.Table{
		Schema:  "public",
		Name:    "standalone",
		Columns: []schema.Column{{Name: "id"}},
	}

	tests := []struct {
		name   string
		parent *schema.Table
		lookup build.SchemaLookup
		q      query.Query
	}{
		{
			name:   "embed without lookup",
			parent: users,
			lookup: nil,
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{Relation: "posts"}},
				},
			},
		},
		{
			name:   "embed on unknown relation",
			parent: users,
			lookup: lookup,
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{Relation: "ghost"}},
				},
			},
		},
		{
			name:   "embed with no foreign-key path",
			parent: standalone,
			lookup: testLookup{"standalone": standalone, "users": users},
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{Relation: "users"}},
				},
			},
		},
		{
			name:   "embed with unknown column",
			parent: users,
			lookup: lookup,
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{
						Relation: "posts",
						Items:    []query.SelectItem{{Column: "ghost"}},
					}},
				},
			},
		},
		{
			name:   "embed with nested embed is rejected",
			parent: users,
			lookup: lookup,
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{
						Relation: "posts",
						Items: []query.SelectItem{
							{Embed: &query.Embed{Relation: "comments"}},
						},
					}},
				},
			},
		},
		{
			name:   "embed duplicate alias",
			parent: users,
			lookup: lookup,
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{Relation: "posts"}},
					{Alias: "posts", Embed: &query.Embed{Relation: "posts"}},
				},
			},
		},
		{
			name:   "ambiguous one-to-many: two FKs from child to parent",
			parent: users,
			lookup: testLookup{
				"users": users,
				"messages": &schema.Table{
					Schema:     "public",
					Name:       "messages",
					PrimaryKey: []string{"id"},
					Columns: []schema.Column{
						{Name: "id"},
						{Name: "sender_id"},
						{Name: "recipient_id"},
					},
					ForeignKeys: []schema.ForeignKey{
						{Table: "public.users", Columns: []string{"sender_id"}, References: []string{"id"}},
						{Table: "public.users", Columns: []string{"recipient_id"}, References: []string{"id"}},
					},
				},
			},
			q: query.Query{
				Select: []query.SelectItem{
					{Embed: &query.Embed{Relation: "messages"}},
				},
			},
		},
		{
			name:   "star and embed alias collide on base column name",
			parent: users,
			lookup: lookup,
			q: query.Query{
				Select: []query.SelectItem{
					{Column: "*"},
					{Alias: "id", Embed: &query.Embed{Relation: "posts"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := build.Select(tt.q, tt.parent, tt.lookup, d, 100, 1000)
			if err == nil {
				t.Errorf("Select: expected error, got nil")
			}
		})
	}
}
