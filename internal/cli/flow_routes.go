package cli

import (
	"slices"
	"strconv"
	"strings"
)

type flowRoute struct {
	from, to []int
	color    string
	gates    []string
	backward bool
}

// Join routes at a common endpoint only. A shared badge between multiple
// sources and destinations would imply moves the policy may not allow.
func flowRoutes(moves []flowMove) []flowRoute {
	result := make([]flowRoute, 0, len(moves))
	used := make([]bool, len(moves))
	for i, move := range moves {
		if used[i] {
			continue
		}
		route := flowRoute{
			from: []int{move.from}, to: []int{move.to}, color: move.color,
			gates: move.gates, backward: move.to < move.from,
		}
		if len(move.gates) > 0 {
			var sameFrom, sameTo []int
			for j := i + 1; j < len(moves); j++ {
				other := moves[j]
				if used[j] || other.color != move.color || (other.to < other.from) != route.backward || !sameFlowGates(move.gates, other.gates) {
					continue
				}
				switch {
				case move.from == other.from:
					sameFrom = append(sameFrom, j)
				case move.to == other.to:
					sameTo = append(sameTo, j)
				}
			}
			if len(sameFrom) > len(sameTo) {
				for _, j := range sameFrom {
					route.to = append(route.to, moves[j].to)
					used[j] = true
				}
			} else {
				for _, j := range sameTo {
					route.from = append(route.from, moves[j].from)
					used[j] = true
				}
			}
		}
		result = append(result, route)
	}
	return result
}

func sameFlowGates(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, gate := range a {
		if !slices.Contains(b, gate) {
			return false
		}
	}
	return true
}

func stageNumbers(indices []int) string {
	numbers := make([]string, len(indices))
	for i, index := range indices {
		numbers[i] = strconv.Itoa(index + 1)
	}
	return strings.Join(numbers, ", ")
}

// joinFlowRoutes puts a gate between the common stage and its branches.
// Routes on either side of the gate keep their original direction and colour.
func joinFlowRoutes(original *flowLayout, routes []flowRoute, inlineAll bool) *flowLayout {
	if !inlineAll && len(routes) == len(original.moves) {
		return original
	}
	joined := &flowLayout{nodes: slices.Clone(original.nodes), cols: original.cols, rows: original.rows}
	boundaries := make([]bool, original.cols)
	type gatePosition struct {
		boundary int
		backward bool
	}
	var gates []gatePosition
	connect := func(from, to int, route flowRoute, gates []string) {
		joined.moves = append(joined.moves, flowMove{from: from, to: to, color: route.color, gates: gates})
	}
	for _, route := range routes {
		from, to := route.from[0], route.to[0]
		if len(route.gates) == 0 || !inlineAll && len(route.from) == 1 && len(route.to) == 1 {
			connect(from, to, route, route.gates)
			continue
		}
		boundary := original.nodes[to].col - 1
		if len(route.to) > 1 {
			boundary = original.nodes[from].col
		}
		if route.backward {
			boundary = original.nodes[to].col
			if len(route.to) > 1 {
				boundary = original.nodes[from].col - 1
			}
		}
		// End-stage alignment can put a return's endpoints in the same column.
		// Keep that rare case on its original paths.
		if slices.ContainsFunc(route.from, func(i int) bool { return (original.nodes[i].col <= boundary) == route.backward }) ||
			slices.ContainsFunc(route.to, func(i int) bool { return (original.nodes[i].col > boundary) == route.backward }) {
			for _, source := range route.from {
				for _, target := range route.to {
					connect(source, target, route, route.gates)
				}
			}
			continue
		}
		boundaries[boundary] = true
		gates = append(gates, gatePosition{boundary: boundary, backward: route.backward})
		gate := len(joined.nodes)
		joined.nodes = append(joined.nodes, flowNode{name: gateLabel(route.gates), gates: route.gates, tone: route.color})
		for _, source := range route.from {
			connect(source, gate, route, nil)
		}
		for _, target := range route.to {
			connect(gate, target, route, nil)
		}
	}
	if len(gates) == 0 {
		return original
	}
	shift := make([]int, original.cols)
	for col := range shift {
		shift[col] = joined.cols - original.cols
		if boundaries[col] {
			joined.cols++
		}
	}
	for i := range original.nodes {
		n := &joined.nodes[i]
		n.col += shift[n.col]
	}
	nextRow, returnRow := make([]int, joined.cols), make([]int, joined.cols)
	for _, gate := range gates {
		if !gate.backward {
			returnRow[gate.boundary+shift[gate.boundary]+1]++
		}
	}
	for col := range returnRow {
		returnRow[col] = max(original.rows, returnRow[col])
	}
	for i, gate := range gates {
		node := &joined.nodes[len(original.nodes)+i]
		node.col = gate.boundary + shift[gate.boundary] + 1
		if gate.backward {
			node.row = returnRow[node.col]
			returnRow[node.col]++
		} else {
			node.row = nextRow[node.col]
			nextRow[node.col]++
		}
		node.home = node.row
		joined.rows = max(joined.rows, node.row+1)
	}
	joined.arrange()
	joined.optimise()
	return joined
}
