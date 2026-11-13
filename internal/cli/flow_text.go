package cli

// textFlow draws a flowLayout with box characters. Stages are plain labels, and
// moves are lines between them. ok is false when it does not fit width.
func (v view) textFlow(l *flowLayout, width int) ([]row, bool) {
	// Columns are as wide as their longest name. The gap after a column is wide
	// enough for each move that runs in it to have its own vertical line.
	colW := make([]int, l.cols)
	for _, n := range l.nodes {
		colW[n.col] = max(colW[n.col], len([]rune(n.name)))
	}
	gapW := func(roomy bool, g int) int {
		if roomy {
			return max(5, 2*len(l.uses[g])+3)
		}
		return max(4, len(l.uses[g])+3)
	}
	var g textGeometry
	for _, roomy := range []bool{true, false} {
		g = textGeometry{l: l, x0: make([]int, l.cols), gaps: make([]int, max(0, l.cols-1))}
		g.base = 0
		if l.lanes > 0 {
			g.base = l.lanes + 1
		}
		x := 1
		for c := range l.cols {
			g.x0[c] = x
			x += colW[c]
			if c < len(g.gaps) {
				g.gaps[c] = gapW(roomy, c)
				x += g.gaps[c]
			}
		}
		g.width = x
		if x <= width {
			break
		}
		if !roomy {
			return nil, false
		}
	}
	g.colW = colW
	height := g.base + (l.rows-1)*2 + 1
	grid := make([][]flowCell, height)
	for y := range grid {
		grid[y] = make([]flowCell, g.width)
	}
	// Quiet moves first, so a gap shows on top where lines cross.
	for _, name := range []string{muted, green, red} {
		for i, m := range l.moves {
			if m.color != name {
				continue
			}
			path := l.route(i, g)
			for k := 1; k < len(path); k++ {
				a, b := path[k-1], path[k]
				for x, y := a.x, a.y; x != b.x || y != b.y; {
					nx, ny := x+sign(b.x-x), y+sign(b.y-y)
					grid[y][x].dirs |= dirTo(nx-x, ny-y)
					grid[ny][nx].dirs |= dirTo(x-nx, y-ny)
					grid[y][x].style, grid[ny][nx].style = m.color, m.color
					x, y = nx, ny
				}
			}
			end, before := path[len(path)-1], path[len(path)-2]
			grid[end.y][end.x].arrow = "→"
			if end.x == before.x {
				grid[end.y][end.x].arrow = "↓"
			}
		}
	}
	for _, n := range l.nodes {
		style := "1"
		if n.broken {
			style = "1;" + red
		}
		for i, r := range []rune(n.name) {
			grid[g.rowY(n.row)][g.x0[n.col]+i] = flowCell{text: string(r), style: style}
		}
	}
	rows := make([]row, height)
	for y, cells := range grid {
		var line row
		for _, c := range cells {
			text, style := c.glyph(), c.style
			if c.text == "" && c.arrow == "" && c.dirs == 0 {
				style = ""
			}
			if n := len(line); n > 0 && line[n-1].style == style {
				line[n-1].text += text
			} else {
				line = append(line, cell{text: text, style: style})
			}
		}
		rows[y] = line
	}
	return rows, true
}

type textGeometry struct {
	l     *flowLayout
	x0    []int // left edge of each column
	colW  []int
	gaps  []int
	width int
	base  int // the row of the first stage
}

func (g textGeometry) startX(node int) int {
	n := g.l.nodes[node]
	return g.x0[n.col] + len([]rune(n.name))
}
func (g textGeometry) endX(node int) int { return g.x0[g.l.nodes[node].col] - 1 }
func (g textGeometry) gapX(gap, slot int) int {
	left := g.x0[gap] + g.colW[gap]
	return min(left+1+slot*2, left+g.gaps[gap]-2)
}
func (g textGeometry) rowY(row int) int   { return g.base + row*2 }
func (g textGeometry) laneY(lane int) int { return g.l.lanes - 1 - lane }
func (g textGeometry) topY(node int) int  { return g.rowY(g.l.nodes[node].row) - 1 }

// Lines along the top of a stage are spread over its name.
func (g textGeometry) topX(node, slot int) int {
	n := g.l.nodes[node]
	width := len([]rune(n.name))
	return g.x0[n.col] + min(width-1, (slot+1)*width/(len(g.l.tops[node])+1))
}

const (
	dirUp = 1 << iota
	dirDown
	dirLeft
	dirRight
)

func dirTo(dx, dy int) int {
	switch {
	case dx > 0:
		return dirRight
	case dx < 0:
		return dirLeft
	case dy > 0:
		return dirDown
	}
	return dirUp
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// flowCell is one character of the diagram: a letter of a stage name, or the
// lines through it as a set of directions.
type flowCell struct {
	text  string
	style string
	dirs  int
	arrow string // "→" or "↓" where a move arrives
}

func (c flowCell) glyph() string {
	switch {
	case c.text != "":
		return c.text
	case c.arrow != "":
		return c.arrow
	}
	switch c.dirs {
	case 0:
		return " "
	case dirUp | dirDown, dirUp, dirDown:
		return "│"
	case dirLeft | dirRight, dirLeft, dirRight:
		return "─"
	case dirDown | dirRight:
		return "┌"
	case dirDown | dirLeft:
		return "┐"
	case dirUp | dirRight:
		return "└"
	case dirUp | dirLeft:
		return "┘"
	case dirUp | dirDown | dirRight:
		return "├"
	case dirUp | dirDown | dirLeft:
		return "┤"
	case dirLeft | dirRight | dirDown:
		return "┬"
	case dirLeft | dirRight | dirUp:
		return "┴"
	}
	return "┼"
}
