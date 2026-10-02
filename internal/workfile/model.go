// Package workfile reads workflow policies and evaluates provider facts without network I/O.
package workfile

import "slices"

type Policy struct {
	Tracker     string                `yaml:"tracker"`
	States      []string              `yaml:"states"`
	Transitions map[string]Transition `yaml:"transitions"`
	Gates       map[string][]Rule     `yaml:"gates"`
}

type Transition struct {
	To       []string            `yaml:"to"`
	Requires []string            `yaml:"requires"`
	Routes   map[string][]string `yaml:"routes"`
	End      bool                `yaml:"end"`
}

type Route struct {
	To       string
	Backward bool
	Requires []string
}

// Routes keeps the shorthand for linear policies and makes each branch explicit.
// Legacy backward moves are ungated; routes can put requirements on returns too.
func (p Policy) Routes(state string) []Route {
	t := p.Transitions[state]
	targets := t.To
	if t.Routes != nil {
		targets = nil
		for _, target := range p.States {
			if _, ok := t.Routes[target]; ok {
				targets = append(targets, target)
			}
		}
	}
	var routes []Route
	for _, target := range targets {
		back := slices.Index(p.States, target) < slices.Index(p.States, state)
		var gates []string
		if !back || t.Routes != nil {
			gates = append(gates, t.Requires...)
		}
		gates = append(gates, t.Routes[target]...)
		routes = append(routes, Route{To: target, Backward: back, Requires: unique(gates)})
	}
	return routes
}

func unique(values []string) []string {
	var result []string
	for _, value := range values {
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

type Rule struct {
	If        string      `yaml:"if"`
	When      string      `yaml:"when"`
	By        []string    `yaml:"by"`
	Message   string      `yaml:"message"`
	Condition Expression  `yaml:"-"`
	Guard     *Expression `yaml:"-"`
}

type Provider struct {
	Kind     string            `yaml:"provider"`
	Site     string            `yaml:"site"`
	Scope    string            `yaml:"scope"`
	Statuses map[string]Names  `yaml:"statuses"`
	Org      string            `yaml:"org"`
	Lookback string            `yaml:"lookback"`
	Auth     map[string]string `yaml:"auth"`
}

type Workspace struct {
	Dir       string
	Policy    Policy
	Providers map[string]Provider
	People    map[string]map[string]string
}

// Actor preserves who supplied a value, such as a label or an approval.
type Actor struct {
	Value string
	ID    string
	Name  string
}

type Facts map[string]any

type Ticket struct {
	Key, Title, URL, State, Summary, Error string
	Assignee                               string
	Facts                                  Facts
	Records                                map[string][]Record
}

type Record struct {
	ID, URL, Title, Summary string
	Repository              string
	People                  []string
	Facts                   Facts
}

type Reason struct {
	Text, Action, Provider, Record, URL string
	OnTicket                            bool
}

// RuleResult keeps each PR's evidence, including passes and skips. The aggregate
// outcome alone cannot tell an engineer which of several PRs needs attention.
type RuleResult struct {
	Provider, Record, URL string
	Outcome, Observed     string
}

type Check struct {
	Gate, Outcome string // pass, fail, or skip
	Requirement   string
	Results       []RuleResult
	Reasons       []Reason
}

type Assessment struct {
	Ticket           Ticket
	Earlier, Current []Check
	Next             []string
	Fallback         string
	Routes           []RouteAssessment
}

type RouteAssessment struct {
	To       string
	Backward bool
	Checks   []Check
}

func (r RouteAssessment) Available() bool { return !failed(r.Checks) }

func (a Assessment) Violation() bool { return failed(a.Earlier) }
func (a Assessment) Blocked() bool {
	if a.Violation() || len(a.Routes) == 0 {
		return false
	}
	forward := slices.ContainsFunc(a.Routes, func(r RouteAssessment) bool { return !r.Backward })
	return !slices.ContainsFunc(a.Routes, func(r RouteAssessment) bool { return (!forward || !r.Backward) && r.Available() })
}

func failed(checks []Check) bool {
	return slices.ContainsFunc(checks, func(c Check) bool { return c.Outcome == "fail" })
}

func (w *Workspace) DisplayName(person string) string {
	if name := w.People[person]["name"]; name != "" {
		return name
	}
	return person
}

func (t Ticket) Involves(tracker string, who map[string]string) bool {
	if id := who[tracker]; id != "" && t.Assignee == id {
		return true
	}
	for instance, records := range t.Records {
		id := who[instance]
		if id == "" {
			continue
		}
		for _, r := range records {
			for _, person := range r.People {
				if person == id {
					return true
				}
			}
		}
	}
	return false
}
