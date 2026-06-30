package ui_test

import (
	"math"
	"testing"

	"github.com/mickamy/adms/internal/schema"
	"github.com/mickamy/adms/internal/ui"
)

func TestBuildERD_NodesEdgesAndKeyColumns(t *testing.T) {
	t.Parallel()

	v := ui.BuildERD([]schema.Table{
		{
			Name:       "posts",
			PrimaryKey: []string{"id"},
			Columns:    []schema.Column{{Name: "id"}, {Name: "user_id"}, {Name: "title"}},
			ForeignKeys: []schema.ForeignKey{
				{Table: "public.users", Columns: []string{"user_id"}, References: []string{"id"}},
			},
		},
		{
			Name:       "users",
			PrimaryKey: []string{"id"},
			Columns:    []schema.Column{{Name: "id"}, {Name: "name"}},
		},
	})

	if len(v.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(v.Nodes))
	}

	// Nodes are sorted by name for a stable layout.
	if v.Nodes[0].Name != "posts" || v.Nodes[1].Name != "users" {
		t.Errorf("node order = %q, %q; want posts, users", v.Nodes[0].Name, v.Nodes[1].Name)
	}

	if len(v.Edges) != 1 || v.Edges[0].Self {
		t.Fatalf("edges = %+v, want one non-self edge", v.Edges)
	}

	// posts shows only its key columns: id (PK) and user_id (FK), not title.
	posts := v.Nodes[0]
	if len(posts.Columns) != 2 {
		t.Fatalf("posts key columns = %d, want 2", len(posts.Columns))
	}

	if posts.Columns[0].Name != "id" || !posts.Columns[0].PK {
		t.Errorf("posts col0 = %+v, want id PK", posts.Columns[0])
	}

	if posts.Columns[1].Name != "user_id" || !posts.Columns[1].FK {
		t.Errorf("posts col1 = %+v, want user_id FK", posts.Columns[1])
	}

	if v.Width <= 0 || v.Height <= 0 {
		t.Errorf("canvas = %vx%v, want positive", v.Width, v.Height)
	}

	if e := v.Edges[0]; e.X1 == 0 && e.Y1 == 0 && e.X2 == 0 && e.Y2 == 0 {
		t.Error("edge endpoints were not resolved")
	}
}

func TestBuildERD_Deterministic(t *testing.T) {
	t.Parallel()

	build := func() ui.ERDView {
		return ui.BuildERD([]schema.Table{
			{Name: "a", ForeignKeys: []schema.ForeignKey{{Table: "b", Columns: []string{"x"}, References: []string{"id"}}}},
			{Name: "b"},
			{Name: "c", ForeignKeys: []schema.ForeignKey{{Table: "a", Columns: []string{"y"}, References: []string{"id"}}}},
		})
	}

	v1, v2 := build(), build()
	if len(v1.Nodes) != len(v2.Nodes) {
		t.Fatalf("node counts differ: %d vs %d", len(v1.Nodes), len(v2.Nodes))
	}

	for i := range v1.Nodes {
		if v1.Nodes[i].X != v2.Nodes[i].X || v1.Nodes[i].Y != v2.Nodes[i].Y {
			t.Errorf("node %d position is not deterministic: (%v,%v) vs (%v,%v)",
				i, v1.Nodes[i].X, v1.Nodes[i].Y, v2.Nodes[i].X, v2.Nodes[i].Y)
		}
	}
}

func TestBuildERD_Empty(t *testing.T) {
	t.Parallel()

	if v := ui.BuildERD(nil); v.HasNodes() {
		t.Error("empty schema should produce no nodes")
	}
}

func TestBuildERD_SingleTable(t *testing.T) {
	t.Parallel()

	v := ui.BuildERD([]schema.Table{
		{Name: "solo", PrimaryKey: []string{"id"}, Columns: []schema.Column{{Name: "id"}}},
	})

	if len(v.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(v.Nodes))
	}

	if len(v.Edges) != 0 {
		t.Errorf("edges = %d, want 0", len(v.Edges))
	}

	if v.Width <= 0 || v.Height <= 0 {
		t.Errorf("canvas = %vx%v, want positive", v.Width, v.Height)
	}
}

func TestBuildERD_SelfReference(t *testing.T) {
	t.Parallel()

	v := ui.BuildERD([]schema.Table{
		{
			Name:       "node",
			PrimaryKey: []string{"id"},
			Columns:    []schema.Column{{Name: "id"}, {Name: "parent_id"}},
			ForeignKeys: []schema.ForeignKey{
				{Table: "node", Columns: []string{"parent_id"}, References: []string{"id"}},
			},
		},
	})

	if len(v.Edges) != 1 || !v.Edges[0].Self {
		t.Fatalf("edges = %+v, want one self edge", v.Edges)
	}

	// A self-loop is drawn down the right edge of the box, so both ends
	// share the same x.
	if e := v.Edges[0]; e.X1 != e.X2 {
		t.Errorf("self-loop x mismatch: %v vs %v", e.X1, e.X2)
	}
}

func TestBuildERD_SkipsForeignKeyToHiddenTable(t *testing.T) {
	t.Parallel()

	// The FK target is not part of the exposed schema, so no edge is drawn.
	v := ui.BuildERD([]schema.Table{
		{
			Name:        "orders",
			ForeignKeys: []schema.ForeignKey{{Table: "private.secrets", Columns: []string{"s_id"}, References: []string{"id"}}},
		},
	})

	if len(v.Edges) != 0 {
		t.Errorf("edges = %d, want 0 (target table is not exposed)", len(v.Edges))
	}
}

func TestKeyColumns(t *testing.T) {
	t.Parallel()

	cols := ui.KeyColumns(schema.Table{
		PrimaryKey: []string{"id"},
		Columns:    []schema.Column{{Name: "id"}, {Name: "a_id"}, {Name: "plain"}},
		ForeignKeys: []schema.ForeignKey{
			{Columns: []string{"a_id"}, References: []string{"id"}},
			// Composite FK is ignored for the single-column key markers.
			{Columns: []string{"c1", "c2"}, References: []string{"x", "y"}},
		},
	})

	if len(cols) != 2 {
		t.Fatalf("key columns = %+v, want id and a_id only", cols)
	}

	if !cols[0].PK || cols[1].Name != "a_id" || !cols[1].FK {
		t.Errorf("key columns = %+v, want id PK then a_id FK", cols)
	}
}

func TestBorderPoint(t *testing.T) {
	t.Parallel()

	// Box at (0,0) sized 100x50, so its center is (50, 25).
	const eps = 1e-9

	if x, _ := ui.BorderPoint(0, 0, 100, 50, 1000, 25); math.Abs(x-100) > eps {
		t.Errorf("right border x = %v, want 100", x)
	}

	if _, y := ui.BorderPoint(0, 0, 100, 50, 50, -1000); math.Abs(y) > eps {
		t.Errorf("top border y = %v, want 0", y)
	}

	if x, y := ui.BorderPoint(0, 0, 100, 50, 50, 25); x != 50 || y != 25 {
		t.Errorf("target-at-center = (%v,%v), want (50,25)", x, y)
	}
}
