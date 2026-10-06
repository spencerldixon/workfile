package cli

import (
	"fmt"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

func (v view) stateLabel(a workfile.Assessment, from, state string, current bool) cell {
	color := muted
	if from == a.Ticket.State {
		for _, r := range a.Routes {
			if r.To == state {
				color = routeColor(r)
				break
			}
		}
	}
	if current {
		color = green
		if a.Ticket.Error != "" || a.Violation() || a.Blocked() {
			color = red
		}
		color = "1;4;" + color
		if !v.color {
			state += " (current)"
		}
	} else if color != muted {
		color = "1;" + color
	}
	return styled(state, color)
}

type pipelineEdge struct {
	from, to, lane int
	color          string
}

func (e pipelineEdge) low() int       { return min(e.from, e.to) }
func (e pipelineEdge) high() int      { return max(e.from, e.to) }
func (e pipelineEdge) backward() bool { return e.to < e.from }

// Adjacent forward edges belong to the main chain. Shortcuts and returns use
// separate sets of lanes, reusing a lane only when their spans do not touch.
func pipelineEdges(p workfile.Policy, a workfile.Assessment) []pipelineEdge {
	indices := map[string]int{}
	for i, state := range p.States {
		indices[state] = i
	}
	var edges []pipelineEdge
	for from, state := range p.States {
		for _, route := range p.Routes(state) {
			to, ok := indices[route.To]
			if !ok || to == from {
				continue
			}
			e := pipelineEdge{from: from, to: to, lane: -1, color: muted}
			if state == a.Ticket.State {
				for _, r := range a.Routes {
					if r.To == route.To {
						e.color = routeColor(r)
						break
					}
				}
			}
			if to != from+1 {
				e.lane = 0
				for slices.ContainsFunc(edges, func(other pipelineEdge) bool {
					return other.lane == e.lane && other.backward() == e.backward() && e.low() <= other.high() && other.low() <= e.high()
				}) {
					e.lane++
				}
			}
			edges = append(edges, e)
		}
	}
	return edges
}

func (v view) pipelineLabels(p workfile.Policy, a workfile.Assessment) []cell {
	labels := make([]cell, len(p.States))
	for i, state := range p.States {
		labels[i] = v.stateLabel(a, a.Ticket.State, state, state == a.Ticket.State)
		// A short marker keeps the chain compact and works without colour. The
		// underlined current state remains unchanged in coloured output.
		labels[i].text = clean(state)
		if state == a.Ticket.State {
			labels[i].text += "*"
		}
	}
	return labels
}

func chainArrow(edges []pipelineEdge, from int, gap int) cell {
	color := muted
	found := false
	for _, e := range edges {
		if e.from == from && e.to == from+1 {
			color, found = e.color, true
			break
		}
	}
	if !found {
		return plain(strings.Repeat(" ", gap))
	}
	switch gap {
	case 5:
		return styled(" ──→ ", color)
	case 3:
		return styled(" → ", color)
	default:
		return styled("→", color)
	}
}

func (v view) pipeline(p workfile.Policy, a workfile.Assessment) []row {
	if len(p.States) == 0 {
		return nil
	}
	labels := v.pipelineLabels(p, a)
	edges := pipelineEdges(p, a)
	for _, gap := range []int{5, 3, 1} {
		var chain row
		anchors := make([]int, len(labels))
		for i, label := range labels {
			if i > 0 {
				chain = append(chain, chainArrow(edges, i-1, gap))
			}
			anchors[i] = rowWidth(chain) + (rowWidth(row{label})-1)/2
			chain = append(chain, label)
		}
		width := rowWidth(chain)
		if width > v.innerWidth() {
			continue
		}
		result := pipelineArcs(edges, anchors, width, true)
		result = append(result, chain)
		result = append(result, pipelineArcs(edges, anchors, width, false)...)
		if slices.Contains(p.States, a.Ticket.State) {
			result = append(result, textRows("* current stage", "", muted, "", v.innerWidth())...)
		}
		return result
	}
	return v.pipelineReferences(p, a, labels, edges)
}

// Draw forward shortcuts above the chain and returns below it. Each endpoint
// is anchored to the centre of its one state label. Plain vertical strokes at
// crossings are bridges, not junctions; arrowheads touch destination labels.
func pipelineArcs(edges []pipelineEdge, anchors []int, width int, above bool) []row {
	var arcs []pipelineEdge
	lanes := 0
	for _, e := range edges {
		if e.lane < 0 || e.backward() == above {
			continue
		}
		arcs = append(arcs, e)
		lanes = max(lanes, e.lane+1)
	}
	if lanes == 0 {
		return nil
	}
	height := lanes * 2
	grid := make([][]cell, height)
	for y := range grid {
		grid[y] = make([]cell, width)
		for x := range grid[y] {
			grid[y][x] = styled(" ", muted)
		}
	}
	arcRow := func(e pipelineEdge) int {
		if above {
			return height - 2 - e.lane*2
		}
		return 1 + e.lane*2
	}
	// Horizontal runs go first, so a crossing vertical run remains visually
	// distinct rather than inventing a junction between unrelated routes.
	for _, e := range arcs {
		y := arcRow(e)
		left, right := anchors[e.low()], anchors[e.high()]
		for x := left + 1; x < right; x++ {
			grid[y][x] = styled("─", e.color)
		}
		arrow := "→"
		if e.backward() {
			arrow = "←"
		}
		grid[y][(left+right)/2] = styled(arrow, e.color)
	}
	for _, e := range arcs {
		y := arcRow(e)
		for _, x := range []int{anchors[e.from], anchors[e.to]} {
			start, end := 0, y
			if above {
				start, end = y+1, height
			}
			for at := start; at < end; at++ {
				grid[at][x] = styled("│", e.color)
			}
		}
	}
	for _, e := range arcs {
		y := arcRow(e)
		left, right := anchors[e.low()], anchors[e.high()]
		leftCorner, rightCorner := "└", "┘"
		if above {
			leftCorner, rightCorner = "┌", "┐"
		}
		// Routes sharing an endpoint really do join at that stage; unlike
		// unrelated crossings, these need a visible branch junction.
		if grid[y][left].text == "│" {
			leftCorner = "├"
		}
		if grid[y][right].text == "│" {
			rightCorner = "┤"
		}
		grid[y][left], grid[y][right] = styled(leftCorner, e.color), styled(rightCorner, e.color)
		if above {
			grid[height-1][anchors[e.to]] = styled("↓", e.color)
		} else {
			grid[0][anchors[e.to]] = styled("↑", e.color)
		}
	}
	// Keep a direction arrow on each arc even when its midpoint crosses a
	// vertical connection belonging to another route.
	for _, e := range arcs {
		y := arcRow(e)
		left, right := anchors[e.low()], anchors[e.high()]
		arrow := "→"
		if e.backward() {
			arrow = "←"
		}
		found := false
		for x := left + 1; x < right; x++ {
			found = found || grid[y][x].text == arrow
		}
		if found {
			continue
		}
		middle := (left + right) / 2
		for distance := 0; distance < right-left; distance++ {
			for _, x := range []int{middle - distance, middle + distance} {
				if x > left && x < right && grid[y][x].text == "─" {
					grid[y][x] = styled(arrow, e.color)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	result := make([]row, len(grid))
	for y, cells := range grid {
		for _, c := range cells {
			line := result[y]
			if len(line) > 0 && line[len(line)-1].style == c.style {
				result[y][len(line)-1].text += c.text
			} else {
				result[y] = append(line, c)
			}
		}
	}
	return result
}

// A graph wider than the terminal becomes horizontal strips of numbered
// labels. Cross-strip moves, shortcuts and returns refer to those numbers;
// names are still printed once, and no connector implies an unconfigured move.
func (v view) pipelineReferences(p workfile.Policy, a workfile.Assessment, labels []cell, edges []pipelineEdge) []row {
	result := textRows("Links use stage numbers.", "", muted, "", v.innerWidth())
	groups := make([]int, len(labels))
	var strip row
	group := 0
	for i, label := range labels {
		label.text = fmt.Sprintf("%d.%s", i+1, label.text)
		var addition row
		if len(strip) > 0 {
			addition = append(addition, chainArrow(edges, i-1, 3))
		}
		addition = append(addition, label)
		if len(strip) > 0 && rowWidth(strip)+rowWidth(addition) > v.innerWidth() {
			result = append(result, splitRow(strip, v.innerWidth())...)
			strip = nil
			group++
			addition = row{label}
		}
		groups[i] = group
		strip = append(strip, addition...)
	}
	if len(strip) > 0 {
		result = append(result, splitRow(strip, v.innerWidth())...)
	}
	for from := range labels {
		prefix := styled(fmt.Sprintf("%d ", from+1), muted)
		var references row
		for _, e := range edges {
			if e.from != from || e.to == from+1 && groups[e.from] == groups[e.to] {
				continue
			}
			arrow := "→ "
			if e.backward() {
				arrow = "↩ "
			}
			link := styled(arrow+fmt.Sprint(e.to+1), e.color)
			if len(references) == 0 {
				references = row{prefix}
			}
			addition := row{link}
			if len(references) > 1 {
				addition = append(row{styled(" · ", muted)}, addition...)
			}
			if rowWidth(references)+rowWidth(addition) > v.innerWidth() {
				result = append(result, references)
				references = row{prefix}
				addition = row{link}
				if rowWidth(references)+rowWidth(addition) > v.innerWidth() {
					result = append(result, references)
					references = nil
				}
			}
			references = append(references, addition...)
		}
		if len(references) > 0 {
			result = append(result, splitRow(references, v.innerWidth())...)
		}
	}
	if slices.Contains(p.States, a.Ticket.State) {
		result = append(result, textRows("* current stage", "", muted, "", v.innerWidth())...)
	}
	return result
}
