package workfile

import (
	"fmt"
	"slices"
)

// Move is one forward move between two stages, and the gates it requires.
type Move struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Requires []string `json:"requires"`
}

// forward lists the forward moves in pipeline order. A move is forward only
// when its stage comes later in states, so forward moves never loop, and one
// pass in that order sees every path. Assess reads the graph the same way.
func (p Policy) forward() []Move {
	var result []Move
	for _, state := range p.States {
		for _, route := range p.Routes(state) {
			if !route.Backward {
				result = append(result, Move{From: state, To: route.To, Requires: append([]string{}, route.Requires...)})
			}
		}
	}
	return result
}

// gateOrder lists gates in the order the pipeline first requires them.
func (p Policy) gateOrder(gates []string) []string {
	var order []string
	for _, m := range p.forward() {
		order = append(order, m.Requires...)
	}
	order = unique(order)
	result := slices.Clone(gates)
	slices.SortStableFunc(result, func(a, b string) int { return slices.Index(order, a) - slices.Index(order, b) })
	return result
}

// Guarantees gives, for each stage, the gates every forward path to it passes.
// Assess accepts a stage when any one path passes, so these are the only gates
// a ticket in that stage is sure to have passed.
func (p Policy) Guarantees() map[string][]string {
	must := map[string][]string{p.States[0]: nil}
	for _, m := range p.forward() {
		gates, reached := must[m.From]
		if !reached {
			continue
		}
		arriving := unique(append(slices.Clone(gates), m.Requires...))
		if previous, seen := must[m.To]; seen {
			arriving = slices.DeleteFunc(slices.Clone(previous), func(g string) bool { return !slices.Contains(arriving, g) })
		}
		must[m.To] = arriving
	}
	for stage, gates := range must {
		must[stage] = p.gateOrder(gates)
	}
	return must
}

// Gap is a gate a policy asks for on some routes into a stage but not on all of
// them: a ticket can reach Stage by a route that skips Gate. Assess accepts a
// stage when any one route passes, so the gate protects nothing there.
type Gap struct {
	Stage string `json:"stage"`
	Gate  string `json:"gate"`
	// Guarded are the moves into Stage that require Gate.
	Guarded []Move `json:"guarded"`
	// Open are the moves into Stage that a ticket can take without having
	// passed Gate.
	Open []Move `json:"open"`
}

// Gaps finds every gate required directly on a move into a stage that some
// route to the stage can skip. A gate a ticket passed earlier does not count as
// skipped, so asking for refinement only at the start is not a gap.
func (p Policy) Gaps() []Gap {
	must := p.Guarantees()
	moves := p.forward()
	var gaps []Gap
	for _, stage := range p.States {
		var asked []string
		for _, m := range moves {
			if m.To == stage {
				asked = append(asked, m.Requires...)
			}
		}
		for _, gate := range p.gateOrder(unique(asked)) {
			if slices.Contains(must[stage], gate) {
				continue
			}
			gap := Gap{Stage: stage, Gate: gate, Guarded: []Move{}, Open: []Move{}}
			for _, m := range moves {
				switch {
				case m.To != stage:
				case slices.Contains(m.Requires, gate):
					gap.Guarded = append(gap.Guarded, m)
				case !slices.Contains(must[m.From], gate):
					gap.Open = append(gap.Open, m)
				}
			}
			gaps = append(gaps, gap)
		}
	}
	return gaps
}

// Finding is one plain mistake a policy can have.
type Finding struct {
	Rule     string `json:"rule"`     // "unused-gate"
	Severity string `json:"severity"` // "error" or "warning"
	Message  string `json:"message"`  // plain English, says what to do
}

// Lint finds plain mistakes a valid policy can still have. Problems that make
// a policy invalid are reported by Load as a *PolicyError, not here.
func (p Policy) Lint() []Finding {
	required := map[string]bool{}
	for _, gate := range p.Transitions[AnyStage].Requires {
		required[gate] = true
	}
	for _, state := range p.States {
		t := p.Transitions[state]
		for _, gate := range t.Requires {
			required[gate] = true
		}
		for _, gates := range t.RequiresByDestination {
			for _, gate := range gates {
				required[gate] = true
			}
		}
		for _, route := range p.Routes(state) {
			for _, gate := range route.Requires {
				required[gate] = true
			}
		}
	}
	var unused []string
	for gate := range p.Gates {
		if !required[gate] {
			unused = append(unused, gate)
		}
	}
	slices.Sort(unused)
	var findings []Finding
	for _, gate := range unused {
		findings = append(findings, Finding{
			Rule:     "unused-gate",
			Severity: "warning",
			Message:  fmt.Sprintf("Gate %s is defined but no move requires it. Require it on a move, or remove it.", gate),
		})
	}
	return findings
}
