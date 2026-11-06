// Package workfile reads workflow policies and evaluates provider facts without network I/O.
package workfile

import "slices"

type Policy struct {
	// Name identifies the workspace across clones and computers. Personal files,
	// such as private automations, are kept under it. It is not part of the
	// policy hash: renaming a workspace does not change its rules.
	Name        string                `yaml:"name" json:"-"`
	Tracker     string                `yaml:"tracker"`
	States      []string              `yaml:"states"`
	Transitions map[string]Transition `yaml:"transitions"`
	Gates       map[string][]Rule     `yaml:"gates"`
	// Invariants name the gates every forward path to a stage must pass. wf
	// prove checks them. They describe the rules rather than add to them, so
	// they are not part of the policy hash.
	Invariants map[string][]string `yaml:"invariants" json:"-"`
}

type Transition struct {
	To                    []string            `yaml:"to"`
	Requires              []string            `yaml:"requires"`
	RequiresByDestination map[string][]string `yaml:"-" json:"RequiresByDestination,omitempty"`
	Routes                map[string][]string `yaml:"routes"`
	End                   bool                `yaml:"end"`
}

type Route struct {
	To       string
	Backward bool
	Requires []string
}

// Routes keeps the shorthand for linear policies and makes each branch explicit.
// Shared legacy gates skip returns; explicit destination gates apply to returns too.
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
		gates = append(gates, t.RequiresByDestination[target]...)
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
	ID        string      `yaml:"id"`
	If        string      `yaml:"if"`
	When      string      `yaml:"when"`
	By        []string    `yaml:"by"`
	Message   string      `yaml:"message"`
	Condition Expression  `yaml:"-"`
	Guard     *Expression `yaml:"-"`
}

type Provider struct {
	Kind  string `yaml:"provider"`
	Site  string `yaml:"site"`
	Scope string `yaml:"scope"`
	// DefaultProject turns a bare ticket number such as 2713 into APP-2713.
	DefaultProject string           `yaml:"default_project"`
	Statuses       map[string]Names `yaml:"statuses"`
	Org            string           `yaml:"org"`
	// Lookback is no longer used: PRs are found by ticket key, however old.
	// It is still accepted so older providers.yml files load.
	Lookback string            `yaml:"lookback"`
	Project  int               `yaml:"project,omitempty"`
	View     int               `yaml:"view,omitempty"`
	Auth     map[string]string `yaml:"auth"`
}

type Workspace struct {
	Dir       string
	Policy    Policy
	Providers map[string]Provider
	People    map[string]map[string]string
	// Automations are the shared files in .workfile/automations/, one per person.
	Automations []*AutomationFile
	// Settings are the person's own, from ~/.config/workfile/<name>/config.yml.
	Settings WorkspaceSettings
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
	// OutOfScope marks a ticket that was asked for by key but is hidden from
	// listings by the Jira scope, such as a sub-task or a parked ticket.
	OutOfScope  bool
	Assignee    string
	Facts       Facts
	Unavailable map[string]string
	Records     map[string][]Record
	// LinkedPRs are PRs the tracker knows are linked, as org/repo#42.
	LinkedPRs []string
}

type Record struct {
	ID, URL, Title, Summary string
	// Head is the latest commit, so an agent can tell whether a PR changed
	// after its automation fired.
	Head        string
	Repository  string
	People      []string
	Facts       Facts
	Unavailable map[string]string
}

type Reason struct {
	Text     string `json:"text"`
	Action   string `json:"action"`
	Provider string `json:"provider"`
	Record   string `json:"record"`
	URL      string `json:"url"`
	OnTicket bool   `json:"on_ticket"`
}

// RuleResult keeps each PR's evidence, including passes and skips. The aggregate
// outcome alone cannot tell an engineer which of several PRs needs attention.
type RuleResult struct {
	Provider string `json:"provider"`
	Record   string `json:"record"`
	URL      string `json:"url"`
	Outcome  string `json:"outcome"`
	Observed string `json:"observed"`
	Value    any    `json:"observed_value,omitempty"`
}

type Check struct {
	ID          string       `json:"id"`
	Gate        string       `json:"gate"`
	Outcome     string       `json:"outcome"` // pass, fail, skip, or unknown
	Requirement string       `json:"requirement"`
	Condition   Expression   `json:"condition"`
	When        *Expression  `json:"when,omitempty"`
	By          []string     `json:"by"`
	Results     []RuleResult `json:"results"`
	Reasons     []Reason     `json:"reasons"`
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

func (r RouteAssessment) Available() bool {
	return !failed(r.Checks) && !slices.ContainsFunc(r.Checks, func(c Check) bool { return c.Outcome == "unknown" })
}

func (a Assessment) Violation() bool { return failed(a.Earlier) }
func (a Assessment) Blocked() bool {
	if a.Ticket.Error != "" || a.Violation() || len(a.Routes) == 0 {
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
