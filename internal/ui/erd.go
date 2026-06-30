package ui

import (
	"math"
	"sort"

	"github.com/mickamy/adms/internal/schema"
)

// erdColumn is one key column shown inside a table node: PK and/or FK
// columns only, since an ER overview cares about identity and relations,
// not every column.
type erdColumn struct {
	Name string
	PK   bool
	FK   bool
}

// erdNode is a table box. X/Y is the top-left corner in SVG user units;
// the layout works in centers and converts at the end.
type erdNode struct {
	Name    string
	Columns []erdColumn
	X, Y    float64
	W, H    float64
}

func (n erdNode) cx() float64 { return n.X + n.W/2 }
func (n erdNode) cy() float64 { return n.Y + n.H/2 }

// erdEdge is a foreign-key relationship drawn from one node's border to
// another's, with the arrowhead at the referenced (To) end. Endpoints are
// filled in once positions are known.
type erdEdge struct {
	From, To       int
	X1, Y1, X2, Y2 float64
	Self           bool
}

// erdView is the laid-out diagram handed to the template.
type erdView struct {
	Nodes  []erdNode
	Edges  []erdEdge
	Width  float64
	Height float64
}

// HasNodes reports whether there is anything to draw, so the template can
// show an empty state instead of a blank canvas.
func (v erdView) HasNodes() bool { return len(v.Nodes) > 0 }

const (
	erdCharW    = 7.0  // approx width of one character at the node font size
	erdPadX     = 16.0 // horizontal padding inside a node box
	erdHeaderH  = 26.0 // table-name header height
	erdRowH     = 18.0 // per-column row height
	erdRowPadY  = 8.0  // vertical padding below the last column row
	erdMinW     = 96.0
	erdMargin   = 40.0  // canvas margin around the laid-out graph
	erdIdealLen = 200.0 // preferred edge length (also drives node spacing)
	erdIters    = 500
)

// buildERD turns the introspected schema into a laid-out diagram: one node
// per table, one edge per foreign key. Layout is a deterministic
// Fruchterman-Reingold force simulation (fixed seed-free init + fixed
// iteration count), so the same schema always produces the same picture.
func buildERD(tables []schema.Table) erdView {
	nodes, index := erdNodes(tables)
	edges := erdEdges(tables, index)

	if len(nodes) == 0 {
		return erdView{}
	}

	cx, cy := layoutForce(nodes, edges)
	w, h := placeNodes(nodes, cx, cy)
	resolveEdges(nodes, edges)

	return erdView{Nodes: nodes, Edges: edges, Width: w, Height: h}
}

// erdNodes builds the node list (sorted by name for a stable order) and a
// bare-name → index map for edge resolution.
func erdNodes(tables []schema.Table) ([]erdNode, map[string]int) {
	sorted := make([]schema.Table, len(tables))
	copy(sorted, tables)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	nodes := make([]erdNode, 0, len(sorted))
	index := make(map[string]int, len(sorted))

	for _, t := range sorted {
		index[t.Name] = len(nodes)
		nodes = append(nodes, erdNode{Name: t.Name, Columns: keyColumns(t)})
	}

	for i := range nodes {
		sizeNode(&nodes[i])
	}

	return nodes, index
}

// keyColumns returns the PK and single-column-FK columns of a table, in
// column order, each flagged so the template can mark them.
func keyColumns(t schema.Table) []erdColumn {
	pk := make(map[string]struct{}, len(t.PrimaryKey))
	for _, c := range t.PrimaryKey {
		pk[c] = struct{}{}
	}

	fk := make(map[string]struct{}, len(t.ForeignKeys))
	for _, f := range t.ForeignKeys {
		if len(f.Columns) == 1 {
			fk[f.Columns[0]] = struct{}{}
		}
	}

	cols := make([]erdColumn, 0, len(pk)+len(fk))
	for _, c := range t.Columns {
		_, isPK := pk[c.Name]
		_, isFK := fk[c.Name]
		if isPK || isFK {
			cols = append(cols, erdColumn{Name: c.Name, PK: isPK, FK: isFK})
		}
	}

	return cols
}

// sizeNode sets a node's box dimensions from its text content.
func sizeNode(n *erdNode) {
	widest := float64(len(n.Name))
	for _, c := range n.Columns {
		// +3 leaves room for the "PK"/"FK" marker the template appends.
		if w := float64(len(c.Name) + 3); w > widest {
			widest = w
		}
	}

	n.W = math.Max(erdMinW, widest*erdCharW+erdPadX*2)
	n.H = erdHeaderH + float64(len(n.Columns))*erdRowH + erdRowPadY
}

// erdEdges builds one edge per foreign key whose target table is part of
// the diagram. Self-references are flagged so the template can draw a loop.
func erdEdges(tables []schema.Table, index map[string]int) []erdEdge {
	var edges []erdEdge

	for _, t := range tables {
		from, ok := index[t.Name]
		if !ok {
			continue
		}

		for _, fk := range t.ForeignKeys {
			to, ok := index[bareTableName(fk.Table)]
			if !ok {
				continue
			}

			edges = append(edges, erdEdge{From: from, To: to, Self: from == to})
		}
	}

	return edges
}

// layoutForce runs Fruchterman-Reingold over node centers and returns the
// resulting center coordinates. Initial placement is a deterministic
// golden-angle spiral, so there is no randomness and no symmetric
// stalemate.
func layoutForce(nodes []erdNode, edges []erdEdge) (cx, cy []float64) {
	n := len(nodes)
	cx = make([]float64, n)
	cy = make([]float64, n)

	const goldenAngle = 2.399963229728653 // ~137.5° in radians
	for i := range nodes {
		r := erdIdealLen * math.Sqrt(float64(i)+0.5)
		theta := float64(i) * goldenAngle
		cx[i] = r * math.Cos(theta)
		cy[i] = r * math.Sin(theta)
	}

	if n == 1 {
		cx[0], cy[0] = 0, 0
		return cx, cy
	}

	k := erdIdealLen
	temp := erdIdealLen * 2
	cooling := math.Pow(0.01, 1.0/float64(erdIters))

	dx := make([]float64, n)
	dy := make([]float64, n)

	for range erdIters {
		for i := range dx {
			dx[i], dy[i] = 0, 0
		}

		// Repulsion between every pair.
		for i := range n {
			for j := i + 1; j < n; j++ {
				ddx := cx[i] - cx[j]
				ddy := cy[i] - cy[j]
				dist := math.Hypot(ddx, ddy)
				if dist < 0.01 {
					dist = 0.01
				}

				force := k * k / dist
				ux, uy := ddx/dist, ddy/dist
				dx[i] += ux * force
				dy[i] += uy * force
				dx[j] -= ux * force
				dy[j] -= uy * force
			}
		}

		// Attraction along edges.
		for _, e := range edges {
			if e.Self {
				continue
			}

			ddx := cx[e.From] - cx[e.To]
			ddy := cy[e.From] - cy[e.To]
			dist := math.Hypot(ddx, ddy)
			if dist < 0.01 {
				dist = 0.01
			}

			force := dist * dist / k
			ux, uy := ddx/dist, ddy/dist
			dx[e.From] -= ux * force
			dy[e.From] -= uy * force
			dx[e.To] += ux * force
			dy[e.To] += uy * force
		}

		// Mild gravity toward the origin keeps disconnected nodes from
		// drifting away.
		for i := range n {
			dx[i] -= cx[i] * 0.02
			dy[i] -= cy[i] * 0.02
		}

		// Move, capped by the current temperature.
		for i := range n {
			dist := math.Hypot(dx[i], dy[i])
			if dist < 0.01 {
				continue
			}

			step := math.Min(dist, temp)
			cx[i] += dx[i] / dist * step
			cy[i] += dy[i] / dist * step
		}

		temp *= cooling
	}

	return cx, cy
}

// placeNodes converts center coordinates into top-left box corners,
// shifting everything into positive space with a margin, and returns the
// canvas size.
func placeNodes(nodes []erdNode, cx, cy []float64) (width, height float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for i := range nodes {
		x := cx[i] - nodes[i].W/2
		y := cy[i] - nodes[i].H/2
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x+nodes[i].W)
		maxY = math.Max(maxY, y+nodes[i].H)
	}

	for i := range nodes {
		nodes[i].X = cx[i] - nodes[i].W/2 - minX + erdMargin
		nodes[i].Y = cy[i] - nodes[i].H/2 - minY + erdMargin
	}

	return maxX - minX + erdMargin*2, maxY - minY + erdMargin*2
}

// resolveEdges clips each edge to the borders of its endpoint boxes so the
// line touches the boxes rather than their centers.
func resolveEdges(nodes []erdNode, edges []erdEdge) {
	for i := range edges {
		e := &edges[i]
		if e.Self {
			n := nodes[e.From]
			e.X1, e.Y1 = n.X+n.W, n.Y+n.H*0.3
			e.X2, e.Y2 = n.X+n.W, n.Y+n.H*0.7

			continue
		}

		from, to := nodes[e.From], nodes[e.To]
		e.X1, e.Y1 = borderPoint(from, to.cx(), to.cy())
		e.X2, e.Y2 = borderPoint(to, from.cx(), from.cy())
	}
}

// borderPoint returns where the ray from a node's center toward (tx, ty)
// crosses the node's box border.
func borderPoint(n erdNode, tx, ty float64) (float64, float64) {
	cx, cy := n.cx(), n.cy()
	dx, dy := tx-cx, ty-cy
	if dx == 0 && dy == 0 {
		return cx, cy
	}

	hw, hh := n.W/2, n.H/2

	scale := math.Inf(1)
	if dx != 0 {
		scale = math.Min(scale, hw/math.Abs(dx))
	}
	if dy != 0 {
		scale = math.Min(scale, hh/math.Abs(dy))
	}

	return cx + dx*scale, cy + dy*scale
}
