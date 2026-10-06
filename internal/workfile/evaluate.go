package workfile

import (
	"fmt"
	"slices"
	"strings"
)

// Assess uses current facts, not transition history. On a branching board a
// state is justified when at least one forward path to it passes its gates.
func (w *Workspace) Assess(ticket Ticket) Assessment {
	a := Assessment{Ticket: ticket}
	if ticket.Error != "" {
		return a
	}
	p := w.Policy
	index := slices.Index(p.States, ticket.State)
	if index < 0 {
		a.Ticket.Error = "ticket has an unknown workflow state"
		return a
	}
	cache := map[string][]Check{}
	checks := func(names []string) []Check {
		var result []Check
		for _, name := range unique(names) {
			if _, ok := cache[name]; !ok {
				cache[name] = w.checkGates([]string{name}, ticket)
			}
			result = append(result, cache[name]...)
		}
		return result
	}
	// Prefer a proven path, then a path that could pass if unknown evidence
	// becomes available, then a path with the fewest known failures.
	type pathCost struct{ failed, unknown int }
	paths := map[string][]string{p.States[0]: {}}
	costs := map[string]pathCost{p.States[0]: {}}
	firstFailure := map[string]string{}
	for _, state := range p.States {
		cost, reached := costs[state]
		if !reached {
			continue
		}
		for _, route := range p.Routes(state) {
			if route.Backward {
				continue
			}
			nextCost := cost
			for _, check := range checks(route.Requires) {
				switch check.Outcome {
				case "fail":
					nextCost.failed++
				case "unknown":
					nextCost.unknown++
				}
			}
			previous, seen := costs[route.To]
			if !seen || nextCost.failed < previous.failed || nextCost.failed == previous.failed && nextCost.unknown < previous.unknown {
				costs[route.To] = nextCost
				paths[route.To] = unique(append(slices.Clone(paths[state]), route.Requires...))
				firstFailure[route.To] = firstFailure[state]
				if cost.failed == 0 && cost.unknown == 0 && (nextCost.failed > 0 || nextCost.unknown > 0) {
					firstFailure[route.To] = state
				}
			}
		}
	}
	if _, ok := paths[ticket.State]; !ok {
		a.Ticket.Error = "workflow state has no forward path from the first state"
		return a
	}
	a.Earlier = checks(paths[ticket.State])
	var current []string
	for _, route := range p.Routes(ticket.State) {
		current = append(current, route.Requires...)
		prerequisites := paths[ticket.State]
		if route.Backward {
			prerequisites = paths[route.To]
		} else {
			a.Next = append(a.Next, route.To)
		}
		names := append(slices.Clone(prerequisites), route.Requires...)
		a.Routes = append(a.Routes, RouteAssessment{To: route.To, Backward: route.Backward, Checks: checks(names)})
	}
	a.Current = checks(current)
	a.Fallback = firstFailure[ticket.State]
	// An unknown alternative must not hide a destination whose requirements
	// are all known to pass. Unknown entry evidence always prevents assessment.
	unknown := func(checks []Check) bool {
		return slices.ContainsFunc(checks, func(c Check) bool { return c.Outcome == "unknown" })
	}
	cannotCheck := unknown(a.Earlier)
	forward := slices.ContainsFunc(a.Routes, func(r RouteAssessment) bool { return !r.Backward })
	available, uncertain := false, false
	for _, route := range a.Routes {
		if forward && route.Backward {
			continue
		}
		available = available || route.Available()
		uncertain = uncertain || unknown(route.Checks)
	}
	if cannotCheck || uncertain && !available {
		a.Ticket.Error = "Required evidence is unavailable; inspect the unknown checks before moving this ticket."
	}
	return a
}

func (w *Workspace) checkGates(gates []string, ticket Ticket) []Check {
	var checks []Check
	for _, gate := range gates {
		for _, rule := range w.Policy.Gates[gate] {
			check := Check{ID: ruleID(gate, rule), Gate: gate, Outcome: "skip", Requirement: w.requirement(rule), Condition: rule.Condition, When: rule.Guard, By: append([]string{}, rule.By...), Results: []RuleResult{}, Reasons: []Reason{}}
			instance := ""
			if rule.Condition.Record() {
				instance = rule.Condition.Provider
			}
			if rule.Guard != nil && rule.Guard.Record() {
				instance = rule.Guard.Provider
			}
			records := []Record{{}}
			if instance != "" {
				records = ticket.Records[instance]
				if len(records) == 0 {
					status, observed := "skip", "No linked PRs; per-PR requirements do not apply. Require a PR count separately."
					if reason := ticket.Unavailable[instance+".prs"]; reason != "" {
						status, observed = "unknown", reason
						check.Outcome = "unknown"
					}
					check.Results = append(check.Results, RuleResult{Provider: instance, Record: ticket.Key, URL: ticket.URL, Outcome: status, Observed: observed})
				}
			}
			for _, record := range records {
				result := RuleResult{Provider: rule.Condition.Provider, Record: record.ID, URL: record.URL, Outcome: "skip"}
				if instance != "" {
					result.Provider = instance
				}
				if record.ID == "" {
					result.Record, result.URL = ticket.Key, ticket.URL
				}
				unknown := ""
				if rule.Guard != nil {
					unknown = w.unavailable(*rule.Guard, ticket, record)
				}
				if unknown != "" {
					result.Outcome, result.Observed = "unknown", unknown
					if check.Outcome != "fail" {
						check.Outcome = "unknown"
					}
					check.Results = append(check.Results, result)
					continue
				}
				if rule.Guard != nil && !rule.Guard.Evaluate(w.subject(*rule.Guard, ticket, record, nil)) {
					result.Observed = "Only required when " + conditionText(*rule.Guard)
					check.Results = append(check.Results, result)
					continue
				}
				if unknown = w.unavailable(rule.Condition, ticket, record); unknown != "" {
					result.Outcome, result.Observed = "unknown", unknown
					if check.Outcome != "fail" {
						check.Outcome = "unknown"
					}
					check.Results = append(check.Results, result)
					continue
				}
				value := w.subject(rule.Condition, ticket, record, rule.By)
				result.Observed = w.observation(rule, ticket, record, value)
				if _, numeric := rule.Condition.Value.(int); numeric {
					result.Value = size(value)
				} else if rule.Condition.Fact != "description" {
					result.Value = value
					if values, ok := value.([]string); ok {
						result.Value = append([]string{}, values...)
					}
				}
				if rule.Guard != nil {
					result.Observed += " · " + conditionEvidence(*rule.Guard, w.subject(*rule.Guard, ticket, record, nil))
				}
				result.Outcome = "pass"
				if !rule.Condition.Evaluate(value) {
					result.Outcome = "fail"
					check.Outcome = "fail"
					check.Reasons = append(check.Reasons, Reason{
						Text: w.explain(rule, ticket, record, value), Action: w.nextAction(rule, ticket, record),
						Provider: instance, Record: record.ID, URL: record.URL,
						OnTicket: !rule.Condition.Record(),
					})
				} else if check.Outcome == "skip" {
					check.Outcome = "pass"
				}
				check.Results = append(check.Results, result)
			}
			for _, result := range check.Results {
				if result.Outcome == "unknown" {
					check.Reasons = append(check.Reasons, Reason{Text: result.Observed, Action: "Restore access to the required evidence, then check again.", Provider: result.Provider, Record: result.Record, URL: result.URL, OnTicket: result.Record == ticket.Key})
				}
			}
			checks = append(checks, check)
		}
	}
	return checks
}

func (w *Workspace) subject(e Expression, ticket Ticket, record Record, by []string) any {
	var value any
	if e.Provider == w.Policy.Tracker {
		value = ticket.Facts[e.Fact]
	} else if e.Fact == "prs" {
		return len(ticket.Records[e.Provider])
	} else {
		value = record.Facts[e.Fact]
	}
	if actors, ok := value.([]Actor); ok {
		values := []string{}
		for _, actor := range actors {
			allowed := len(by) == 0
			for _, person := range by {
				allowed = allowed || actor.ID != "" && w.People[person][e.Provider] == actor.ID
			}
			if allowed {
				values = append(values, actor.Value)
			}
		}
		return values
	}
	return value
}

func (w *Workspace) explain(r Rule, ticket Ticket, record Record, value any) string {
	if r.Message != "" {
		id := record.ID
		if id == "" {
			id = ticket.Key
		}
		return strings.NewReplacer("{record}", id, "{ticket}", ticket.Key).Replace(r.Message)
	}
	e := r.Condition
	noun := strings.ReplaceAll(e.Fact, "_", " ")
	if e.Fact == "prs" {
		noun = "linked PRs"
	}
	if e.Fact == "summary" {
		noun = "title"
	}
	var message string
	if number, ok := e.Value.(int); ok {
		comparison := map[string]string{">": "more than", ">=": "at least", "<": "fewer than", "<=": "at most", "==": "exactly", "!=": "anything except"}[e.Op]
		if _, ok := value.(string); ok {
			message = fmt.Sprintf("%s is %d characters; needs %s %d", noun, size(value), comparison, number)
		} else {
			if number == 1 {
				noun = strings.TrimSuffix(noun, "s")
			}
			message = fmt.Sprintf("needs %s %d %s", comparison, number, noun)
			if len(r.By) == 0 {
				message += fmt.Sprintf("; has %d", size(value))
			}
		}
	} else {
		switch e.Op {
		case "contains":
			message = fmt.Sprintf("%s must include %s", noun, e.Value)
		case "contains_any":
			message = fmt.Sprintf("%s must include one of %s", noun, strings.Join(e.Value.([]string), ", "))
		case "excludes":
			message = fmt.Sprintf("%s must not include %s", noun, e.Value)
		case "matches":
			message = fmt.Sprintf("%s must match %s", noun, e.Value)
		case "==":
			message = fmt.Sprintf("%s must be %s (currently %s)", noun, e.Value, displayValue(value))
		case "!=":
			message = fmt.Sprintf("%s must not be %s", noun, e.Value)
		}
	}
	if passingCI(e) {
		message = ciEvidence(value)
	}
	if len(r.By) > 0 {
		names := make([]string, len(r.By))
		for i, person := range r.By {
			names[i] = w.DisplayName(person)
		}
		message += " from " + strings.Join(names, " or ")
	}
	if r.Guard != nil {
		message += " because " + conditionText(*r.Guard)
	}
	if record.ID != "" {
		message = record.ID + ": " + message
	}
	return message
}

func conditionText(e Expression) string {
	noun := strings.ReplaceAll(e.Fact, "_", " ")
	switch e.Op {
	case "contains":
		return fmt.Sprintf("%s include %s", noun, e.Value)
	case "contains_any":
		return fmt.Sprintf("%s include %s", noun, strings.Join(e.Value.([]string), " or "))
	case "excludes":
		return fmt.Sprintf("%s exclude %s", noun, e.Value)
	case "matches":
		return fmt.Sprintf("%s match %s", noun, e.Value)
	case "==":
		return fmt.Sprintf("%s is %v", noun, e.Value)
	case "!=":
		return fmt.Sprintf("%s is not %v", noun, e.Value)
	default:
		comparison := map[string]string{">": "more than", ">=": "at least", "<": "less than", "<=": "at most"}[e.Op]
		return fmt.Sprintf("%s measures %s %v", noun, comparison, e.Value)
	}
}

func displayValue(value any) string {
	if value == nil || value == "" {
		return "empty"
	}
	return fmt.Sprint(value)
}
