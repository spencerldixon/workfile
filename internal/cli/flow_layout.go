package cli

import (
	"slices"
	"sort"
)

// flowLayout places the stages of a policy in columns, for the diagram on the
// lint screen. A stage's column is how far along the workflow it is: the
// longest run of forward moves that leads to it. Stages in the same column are
// stacked, and end stages all share the last column, so two ways to finish line
// up. Moves run in the gaps between columns and over the top, never through a
// stage. The order of stacked stages and of the lines that share a gap is chosen
// to leave as few crossings and overlaps as possible.
type flowLayout struct {
	nodes []flowNode
	moves []flowMove
	cols  int // columns
	rows  int // the most stages in one column
	lanes int // runs above the stages, for moves that skip columns
	// uses lists, for each gap between columns, the vertical lines that run in
	// it, left to right. tops lists, for each stage, the lines that touch its
	// top edge, left to right.
	uses [][]flowUse
	tops [][]flowUse
}

type flowNode struct {
	name     string
	col, row int
	home     int // the row it would have in policy order
	broken   bool
}

const (
	moveStraight = iota // same row, next column
	moveElbow           // next column, another row
	moveLong            // skips a column: goes over the top
)

type flowMove struct {
	from, to int // node indexes
	color    string
	kind     int
	lane     int // long moves: 0 is nearest the stages
}

// flowUse is one end of a move: where it leaves its stage, or where it arrives.
type flowUse struct {
	move   int
	arrive bool
}

type flowPoint struct{ x, y int }

// newFlowLayout lays out stages in order. ends names the stages that finish a
// ticket. edges are the forward moves, already coloured.
func newFlowLayout(stages []string, ends map[string]bool, edges []pipelineEdge, broken map[string]bool) flowLayout {
	rank := make([]int, len(stages))
	for _, e := range edges {
		rank[e.to] = max(rank[e.to], rank[e.from]+1)
	}
	last := 0
	for _, r := range rank {
		last = max(last, r)
	}
	for i, stage := range stages {
		if ends[stage] {
			rank[i] = last
		}
	}
	l := flowLayout{cols: last + 1}
	perColumn := make([]int, l.cols)
	for i, stage := range stages {
		l.nodes = append(l.nodes, flowNode{name: stage, col: rank[i], row: perColumn[rank[i]], home: perColumn[rank[i]], broken: broken[stage]})
		perColumn[rank[i]]++
	}
	for _, n := range perColumn {
		l.rows = max(l.rows, n)
	}
	for _, e := range edges {
		l.moves = append(l.moves, flowMove{from: e.from, to: e.to, color: e.color})
	}
	l.arrange()
	l.optimise()
	return l
}

// arrange works out, from where the stages are, how each move runs, which lines
// share each gap and the top of each stage, and which lane each long move uses.
// Lines that share a gap start in move order; optimise reorders them.
func (l *flowLayout) arrange() {
	l.uses = make([][]flowUse, max(0, l.cols-1))
	l.tops = make([][]flowUse, len(l.nodes))
	for i := range l.moves {
		m := &l.moves[i]
		from, to := l.nodes[m.from], l.nodes[m.to]
		switch gap := to.col - from.col; {
		case gap == 1 && from.row == to.row:
			m.kind = moveStraight
		case gap == 1:
			m.kind = moveElbow
			l.uses[from.col] = append(l.uses[from.col], flowUse{i, false})
		default:
			m.kind = moveLong
			// A stage at the top of its column leaves and arrives by its top
			// edge. One lower down is boxed in, so it uses the gaps.
			if from.row == 0 {
				l.tops[m.from] = append(l.tops[m.from], flowUse{i, false})
			} else {
				l.uses[from.col] = append(l.uses[from.col], flowUse{i, false})
			}
			if to.row == 0 {
				l.tops[m.to] = append(l.tops[m.to], flowUse{i, true})
			} else {
				l.uses[to.col-1] = append(l.uses[to.col-1], flowUse{i, true})
			}
		}
	}
	// Along the top of a stage, lines coming in sit left of lines going out. Of
	// the lines going out, the one that goes furthest is leftmost, so it arcs
	// over the shorter ones; of those coming in, the nearest source is leftmost.
	for node, uses := range l.tops {
		sort.SliceStable(uses, func(a, b int) bool {
			x, y := uses[a], uses[b]
			if x.arrive != y.arrive {
				return x.arrive
			}
			if x.arrive {
				return l.nodes[l.moves[x.move].from].col > l.nodes[l.moves[y.move].from].col
			}
			return l.nodes[l.moves[x.move].to].col > l.nodes[l.moves[y.move].to].col
		})
		l.tops[node] = uses
	}
	// Shorter arcs get the lower lanes, so arcs nest instead of crossing.
	var long []int
	for i, m := range l.moves {
		if m.kind == moveLong {
			long = append(long, i)
		}
	}
	span := func(i int) int { return l.nodes[l.moves[i].to].col - l.nodes[l.moves[i].from].col }
	sort.SliceStable(long, func(a, b int) bool { return span(long[a]) < span(long[b]) })
	l.lanes = 0
	type taken struct{ low, high, lane int }
	var used []taken
	for _, i := range long {
		low, high := l.nodes[l.moves[i].from].col, l.nodes[l.moves[i].to].col
		lane := 0
		for slices.ContainsFunc(used, func(o taken) bool { return o.lane == lane && low <= o.high && o.low <= high }) {
			lane++
		}
		l.moves[i].lane = lane
		used = append(used, taken{low, high, lane})
		l.lanes = max(l.lanes, lane+1)
	}
}

// optimise reorders stacked stages, then the lines in each gap, to lower the
// cost of crossings and overlaps. Columns and gaps are small, so each is tried
// in every order, one at a time.
func (l *flowLayout) optimise() {
	for pass := 0; pass < 3; pass++ {
		changed := false
		for c := 0; c < l.cols; c++ {
			var members []int
			for i, n := range l.nodes {
				if n.col == c {
					members = append(members, i)
				}
			}
			if len(members) < 2 || len(members) > 6 {
				continue
			}
			before := l.cost()
			best, bestRows := before, rowsOf(l.nodes, members)
			permute(len(members), func(order []int) {
				for k, node := range members {
					l.nodes[node].row = order[k]
				}
				l.arrange()
				if cost := l.cost(); cost < best-1e-9 {
					best, bestRows = cost, rowsOf(l.nodes, members)
				}
			})
			for k, node := range members {
				l.nodes[node].row = bestRows[k]
			}
			l.arrange()
			changed = changed || best < before-1e-9
		}
		if !changed {
			break
		}
	}
	for g := range l.uses {
		original := slices.Clone(l.uses[g])
		if len(original) < 2 || len(original) > 6 {
			continue
		}
		best, bestOrder := l.cost(), slices.Clone(original)
		permute(len(original), func(order []int) {
			for k, o := range order {
				l.uses[g][k] = original[o]
			}
			if cost := l.cost(); cost < best-1e-9 {
				best, bestOrder = cost, slices.Clone(l.uses[g])
			}
		})
		copy(l.uses[g], bestOrder)
	}
}

func rowsOf(nodes []flowNode, members []int) []int {
	rows := make([]int, len(members))
	for k, node := range members {
		rows[k] = nodes[node].row
	}
	return rows
}

// permute calls f with every ordering of 0..n-1. f must not keep the slice.
func permute(n int, f func([]int)) {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	var rec func(k int)
	rec = func(k int) {
		if k == n {
			f(order)
			return
		}
		for i := k; i < n; i++ {
			order[k], order[i] = order[i], order[k]
			rec(k + 1)
			order[k], order[i] = order[i], order[k]
		}
	}
	rec(0)
}

// flowRun is one straight stretch of a move.
type flowRun struct {
	a, b flowPoint
	move int
}

func (r flowRun) horizontal() bool { return r.a.y == r.b.y }

// crosses reports whether a horizontal run passes through a vertical one,
// strictly inside both, so meeting at a corner does not count.
func (r flowRun) crosses(v flowRun) bool {
	return min(r.a.x, r.b.x) < v.a.x && v.a.x < max(r.a.x, r.b.x) &&
		min(v.a.y, v.b.y) < r.a.y && r.a.y < max(v.a.y, v.b.y)
}

// along is how far two parallel runs on the same line run together.
func (r flowRun) along(s flowRun) int {
	if r.horizontal() {
		return max(0, min(max(r.a.x, r.b.x), max(s.a.x, s.b.x))-max(min(r.a.x, r.b.x), min(s.a.x, s.b.x)))
	}
	return max(0, min(max(r.a.y, r.b.y), max(s.a.y, s.b.y))-max(min(r.a.y, r.b.y), min(s.a.y, s.b.y)))
}

// cost scores how hard the diagram is to read: lines that cross, lines that run
// along each other, and stages moved from their place in the policy. Moves that
// leave the same stage, or arrive at the same one, are expected to share a stretch.
func (l *flowLayout) cost() float64 {
	g := unitGeometry{l: l}
	var runs []flowRun
	for i := range l.moves {
		path := l.route(i, g)
		for k := 1; k < len(path); k++ {
			runs = append(runs, flowRun{path[k-1], path[k], i})
		}
	}
	cost := 0.0
	for _, n := range l.nodes {
		cost += 0.01 * float64(abs(n.row-n.home))
	}
	for i, r := range runs {
		for _, s := range runs[i+1:] {
			if r.move == s.move {
				continue
			}
			mr, ms := l.moves[r.move], l.moves[s.move]
			switch {
			case r.horizontal() && !s.horizontal():
				if r.crosses(s) {
					cost += 3
				}
			case !r.horizontal() && s.horizontal():
				if s.crosses(r) {
					cost += 3
				}
			case r.horizontal() == s.horizontal() && r.horizontal() && r.a.y == s.a.y,
				r.horizontal() == s.horizontal() && !r.horizontal() && r.a.x == s.a.x:
				if mr.from != ms.from && mr.to != ms.to {
					cost += 6 * float64(r.along(s))
				}
			}
		}
	}
	return cost
}

// flowGeometry says where things are, in whatever unit a drawing uses: cells
// for text, pixels for a picture.
type flowGeometry interface {
	startX(node int) int // where a move leaves the right of a stage
	endX(node int) int   // where a move arrives at the left of one
	gapX(gap, slot int) int
	topX(node, slot int) int
	topY(node int) int // where a move touches the top of a stage
	rowY(row int) int
	laneY(lane int) int
}

// slot finds where a move's end sits: in a gap, or along the top of a stage.
func (l *flowLayout) slot(move int, arrive bool) (top bool, id, index int) {
	for node, uses := range l.tops {
		if i := slices.Index(uses, flowUse{move, arrive}); i >= 0 {
			return true, node, i
		}
	}
	for gap, uses := range l.uses {
		if i := slices.Index(uses, flowUse{move, arrive}); i >= 0 {
			return false, gap, i
		}
	}
	return false, 0, 0
}

// route is the corners of a move, from the stage it leaves to the stage it
// reaches. It arrives going right, or going down onto the top of a stage.
func (l *flowLayout) route(i int, g flowGeometry) []flowPoint {
	m := l.moves[i]
	from, to := l.nodes[m.from], l.nodes[m.to]
	y0, y1 := g.rowY(from.row), g.rowY(to.row)
	start, end := flowPoint{g.startX(m.from), y0}, flowPoint{g.endX(m.to), y1}
	switch m.kind {
	case moveStraight:
		return []flowPoint{start, end}
	case moveElbow:
		_, gap, slot := l.slot(i, false)
		x := g.gapX(gap, slot)
		return []flowPoint{start, {x, y0}, {x, y1}, end}
	}
	lane := g.laneY(m.lane)
	var path []flowPoint
	if top, node, slot := l.slot(i, false); top {
		x := g.topX(node, slot)
		path = append(path, flowPoint{x, g.topY(m.from)}, flowPoint{x, lane})
	} else {
		x := g.gapX(node, slot)
		path = append(path, start, flowPoint{x, y0}, flowPoint{x, lane})
	}
	if top, node, slot := l.slot(i, true); top {
		x := g.topX(node, slot)
		return append(path, flowPoint{x, lane}, flowPoint{x, g.topY(m.to)})
	} else {
		x := g.gapX(node, slot)
		return append(path, flowPoint{x, lane}, flowPoint{x, y1}, end)
	}
}

// unitGeometry is a rough picture in whole numbers, for scoring a layout. Every
// stage is the same width, and every gap and row the same size.
type unitGeometry struct{ l *flowLayout }

const unitCol, unitStage, unitRow = 16, 8, 4

func (g unitGeometry) startX(node int) int { return g.endX(node) + unitStage }
func (g unitGeometry) endX(node int) int   { return g.l.nodes[node].col * unitCol }
func (g unitGeometry) gapX(gap, slot int) int {
	return gap*unitCol + unitStage + (slot+1)*(unitCol-unitStage)/(len(g.l.uses[gap])+1)
}
func (g unitGeometry) topX(node, slot int) int {
	return g.endX(node) + (slot+1)*unitStage/(len(g.l.tops[node])+1)
}
func (g unitGeometry) topY(node int) int  { return g.rowY(g.l.nodes[node].row) - 1 }
func (g unitGeometry) rowY(row int) int   { return g.l.lanes*2 + 2 + row*unitRow }
func (g unitGeometry) laneY(lane int) int { return (g.l.lanes - 1 - lane) * 2 }
