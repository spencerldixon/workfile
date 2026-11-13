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

// Proof says whether every forward path to Stage passes Gate.
type Proof struct {
	Stage string `json:"stage"`
	Gate  string `json:"gate"`
	Holds bool   `json:"holds"`
	// Bypass is the shortest forward path that reaches Stage without Gate.
	// It is empty, not nil, when Stage is where tickets start.
	Bypass []Move `json:"bypass,omitempty"`
	// Fixes are moves on the bypass where requiring Gate closes every bypass,
	// and which no path that already passes Gate uses.
	Fixes []Move `json:"fixes,omitempty"`
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

// Prove checks each invariant in pipeline order. A gate holds at a stage
// exactly when the stage cannot be reached once every move that requires the
// gate is taken away.
func (p Policy) Prove() []Proof {
	var proofs []Proof
	for _, stage := range p.States {
		for _, gate := range p.Invariants[stage] {
			proof := Proof{Stage: stage, Gate: gate, Holds: true}
			if path, found := p.bypass(stage, gate, Move{}); found {
				proof.Holds, proof.Bypass, proof.Fixes = false, path, p.fixes(stage, gate, path)
			}
			proofs = append(proofs, proof)
		}
	}
	return proofs
}

// bypass finds the shortest forward path to stage that never requires gate,
// treating extra as if it required gate too.
func (p Policy) bypass(stage, gate string, extra Move) ([]Move, bool) {
	start := p.States[0]
	via := map[string]Move{}
	length := map[string]int{start: 0}
	for _, m := range p.forward() {
		n, reached := length[m.From]
		if !reached || slices.Contains(m.Requires, gate) || m.From == extra.From && m.To == extra.To {
			continue
		}
		if previous, seen := length[m.To]; !seen || n+1 < previous {
			length[m.To], via[m.To] = n+1, m
		}
	}
	if _, reached := length[stage]; !reached {
		return nil, false
	}
	path := []Move{}
	for at := stage; at != start; at = via[at].From {
		path = append(path, via[at])
	}
	slices.Reverse(path)
	return path, true
}

// fixes lists the moves on path that would close every bypass if they required
// gate. A move that a path through gate already uses is left out: requiring it
// there would ask twice on that path, which is rarely what a team means.
func (p Policy) fixes(stage, gate string, path []Move) []Move {
	moves := p.forward()
	// passed: some path from the start reaches this stage through gate.
	passed := map[string]bool{}
	for _, m := range moves {
		if passed[m.From] || slices.Contains(m.Requires, gate) {
			passed[m.To] = true
		}
	}
	// ahead: some path from this stage reaches the target through gate.
	ahead, leads := map[string]bool{}, map[string]bool{stage: true}
	for _, m := range slices.Backward(moves) {
		if leads[m.To] {
			leads[m.From] = true
			ahead[m.From] = ahead[m.From] || ahead[m.To] || slices.Contains(m.Requires, gate)
		}
	}
	var result []Move
	for _, m := range path {
		if passed[m.From] || ahead[m.To] {
			continue
		}
		if _, open := p.bypass(stage, gate, m); !open {
			result = append(result, m)
		}
	}
	return result
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
	if len(p.Invariants) == 0 {
		findings = append(findings, Finding{
			Rule:     "no-invariants",
			Severity: "warning",
			Message:  "No invariants. Add one, such as done: [code_review], to prove no path skips a gate.",
		})
	}
	return findings
}
