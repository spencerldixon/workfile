package workfile

import "slices"

type GateStat struct {
	Name              string
	Required, Failing int
	Reason            string // the requirement that fails for the most tickets
	ReasonCount       int
	Rules             []RuleStat  // every requirement in the gate, in policy order
	Stages            []StageStat // tickets the gate applies to, per stage in pipeline order
}

// RuleStat counts tickets failing one requirement of a gate.
type RuleStat struct {
	Text    string
	Failing int
}

type StageStat struct {
	Name           string
	Total, Failing int
}

// Rollup summarises assessments in pipeline order. Tickets that cannot be
// checked are counted apart so they never distort a percentage.
type Rollup struct {
	Checked, InPolicy, OutOfPolicy, Unchecked int
	Gates                                     []GateStat
	Stages                                    []StageStat
}

// GateOrder lists gates by the first forward move that requires them,
// walking stages in policy order.
func (p Policy) GateOrder() []string {
	var order []string
	for _, state := range p.States {
		for _, route := range p.Routes(state) {
			if route.Backward {
				continue
			}
			for _, gate := range route.Requires {
				if !slices.Contains(order, gate) {
					order = append(order, gate)
				}
			}
		}
	}
	return order
}

// FailsGate reports whether an earlier gate on the ticket's path failed.
func (a Assessment) FailsGate(gate string) bool {
	return slices.ContainsFunc(a.Earlier, func(c Check) bool { return c.Gate == gate && c.Outcome == "fail" })
}

// Rollup counts a gate against tickets whose policy path includes it and
// where it applied (not every rule skipped).
func (w *Workspace) Rollup(all []Assessment) Rollup {
	var r Rollup
	order := w.Policy.GateOrder()
	gates := make([]GateStat, len(order))
	reasons := make([]map[string]int, len(order))
	for i, name := range order {
		gates[i].Name = name
		reasons[i] = map[string]int{}
		for _, state := range w.Policy.States {
			gates[i].Stages = append(gates[i].Stages, StageStat{Name: state})
		}
	}
	r.Stages = make([]StageStat, len(w.Policy.States))
	for i, name := range w.Policy.States {
		r.Stages[i].Name = name
	}
	for _, a := range all {
		if a.Ticket.Error != "" {
			r.Unchecked++
			continue
		}
		r.Checked++
		violation := a.Violation()
		if violation {
			r.OutOfPolicy++
		} else {
			r.InPolicy++
		}
		if s := slices.Index(w.Policy.States, a.Ticket.State); s >= 0 {
			r.Stages[s].Total++
			if violation {
				r.Stages[s].Failing++
			}
		}
		for i, name := range order {
			applies, fails := false, false
			seen := map[string]bool{}
			for _, c := range a.Earlier {
				if c.Gate != name || c.Outcome == "skip" {
					continue
				}
				applies = true
				at := slices.IndexFunc(gates[i].Rules, func(r RuleStat) bool { return r.Text == c.Requirement })
				if at < 0 {
					gates[i].Rules = append(gates[i].Rules, RuleStat{Text: c.Requirement})
					at = len(gates[i].Rules) - 1
				}
				if c.Outcome == "fail" {
					fails = true
					if !seen[c.Requirement] {
						seen[c.Requirement] = true
						reasons[i][c.Requirement]++
						gates[i].Rules[at].Failing++
					}
				}
			}
			stage := slices.Index(w.Policy.States, a.Ticket.State)
			if applies {
				gates[i].Required++
				if stage >= 0 {
					gates[i].Stages[stage].Total++
				}
			}
			if fails {
				gates[i].Failing++
				if stage >= 0 {
					gates[i].Stages[stage].Failing++
				}
			}
		}
	}
	for i := range gates {
		for text, n := range reasons[i] {
			if n > gates[i].ReasonCount || n == gates[i].ReasonCount && text < gates[i].Reason {
				gates[i].Reason, gates[i].ReasonCount = text, n
			}
		}
		if gates[i].Required > 0 {
			r.Gates = append(r.Gates, gates[i])
		}
	}
	return r
}
