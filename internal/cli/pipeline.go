package cli

import (
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"

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
		if a.Violation() || a.Blocked() {
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

// Every drawn edge comes from policy. Repeated nodes end a joined path;
// backward edges end at their target, so cycles never recurse indefinitely.
func (v view) pipeline(p workfile.Policy, a workfile.Assessment) []row {
	seen := map[string]bool{}
	toward := map[string]bool{a.Ticket.State: true}
	for i := len(p.States) - 1; i >= 0; i-- {
		for _, r := range p.Routes(p.States[i]) {
			if !r.Backward && toward[r.To] {
				toward[p.States[i]] = true
			}
		}
	}
	// Only forward moves are drawn. Returns are implied, and the Transitions
	// box below lists the ones that are open.
	var draw func(string, string) []row
	draw = func(from, state string) []row {
		label := v.stateLabel(a, from, state, state == a.Ticket.State)
		first := row{label}
		if seen[state] {
			return []row{first}
		}
		seen[state] = true
		outgoing := slices.DeleteFunc(p.Routes(state), func(r workfile.Route) bool { return r.Backward })
		// Put the path to this ticket first.
		slices.SortStableFunc(outgoing, func(x, y workfile.Route) int {
			left, right := toward[x.To], toward[y.To]
			if left != right {
				if left {
					return -1
				}
				return 1
			}
			return 0
		})
		if len(outgoing) == 0 {
			return []row{first}
		}
		offset := rowWidth(first) + 2
		var result []row
		for i, r := range outgoing {
			child := draw(state, r.To)
			if i == 0 {
				arrow := " → "
				if len(outgoing) > 1 {
					arrow = " ─┬→ "
				}
				result = append(result, append(append(first, styled(arrow, muted)), child[0]...))
			} else {
				arrow := "├→ "
				if i == len(outgoing)-1 {
					arrow = "└→ "
				}
				line := row{styled(strings.Repeat(" ", offset)+arrow, muted)}
				result = append(result, append(line, child[0]...))
			}
			for _, line := range child[1:] {
				indent := strings.Repeat(" ", offset)
				if len(outgoing) == 1 {
					indent = strings.Repeat(" ", rowWidth(first)+3)
				} else if i < len(outgoing)-1 {
					indent += "│  "
				} else {
					indent += "   "
				}
				result = append(result, append(row{styled(indent, muted)}, line...))
			}
		}
		return result
	}
	result := draw("", p.States[0])
	if !slices.ContainsFunc(result, func(r row) bool { return rowWidth(r) > v.innerWidth() }) {
		return result
	}
	// Too wide to draw as a tree: one line per state naming where it can go
	// forward. Each state leads one line at most, and returns stay implied.
	labelWidth := 0
	for _, state := range p.States {
		labelWidth = max(labelWidth, runewidth.StringWidth(state))
		if !v.color && state == a.Ticket.State {
			labelWidth = max(labelWidth, runewidth.StringWidth(state+" (current)"))
		}
	}
	result = nil
	for _, state := range p.States {
		var forward []workfile.Route
		for _, r := range p.Routes(state) {
			if !r.Backward {
				forward = append(forward, r)
			}
		}
		if len(forward) == 0 {
			continue
		}
		label := v.stateLabel(a, "", state, state == a.Ticket.State)
		line := row{label, plain(strings.Repeat(" ", max(1, labelWidth-rowWidth(row{label})))), styled("→ ", muted)}
		for i, r := range forward {
			if i > 0 {
				line = append(line, styled(", ", muted))
			}
			line = append(line, v.stateLabel(a, state, r.To, false))
		}
		result = append(result, line)
	}
	return result
}
