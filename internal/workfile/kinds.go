package workfile

import (
	"net/url"
	"slices"
	"strings"
)

// Keys turns ticket keys into text and back. It is pure: no network.
type Keys interface {
	Parse(input, project string) (string, error)
	Search(key string) string
	Mentions(text string) []string
	Links(body string) []string
}

// Kind describes one kind of connection in providers.yml.
type Kind struct {
	Label    string              // the name people read: "Jira"
	Auth     []string            // required credentials, each a ${NAME}
	Keys     func(Provider) Keys // needed if it can be a tracker
	Tracker  *Role               // nil if it cannot be a tracker
	CodeHost *Role               // nil if it cannot be a code host
}

// Role is what a kind can do for a workspace.
type Role struct {
	Settings []string // allowed providers.yml settings in this role
	Check    func(name string, p Provider, add func(string, ...any))
	Facts    []string // TrackerFacts or CodeHostFacts, plus any of its own
	ByFacts  []string // facts that say who supplied them, for `by` rules
}

// TrackerFacts are the facts every tracker supplies.
var TrackerFacts = []string{"summary", "description", "headings", "labels", "assignee", "type", "project"}

// CodeHostFacts are the facts every code host supplies.
var CodeHostFacts = []string{"state", "draft", "checks", "approvals", "author", "review_requests", "labels"}

// Kinds is the table of known kinds.
var Kinds = map[string]Kind{
	"jira": {
		Label: "Jira",
		Auth:  []string{"token", "email"},
		Keys:  func(p Provider) Keys { return dashKeys{site: p.Site, path: "/browse/"} },
		Tracker: &Role{
			Settings: []string{"site", "scope", "default_project", "statuses"},
			Check:    checkJira,
			Facts:    slices.Clone(TrackerFacts),
			ByFacts:  []string{"labels"},
		},
	},
	"github": {
		Label: "GitHub",
		Auth:  []string{"token"},
		Keys:  func(p Provider) Keys { return githubKeys{org: p.Org} },
		Tracker: &Role{
			Settings: []string{"org", "project", "view", "statuses", "default_project"},
			Check:    checkGitHubTracker,
			Facts:    slices.Clone(TrackerFacts),
		},
		CodeHost: &Role{
			Settings: []string{"org", "lookback"},
			Check:    checkGitHub,
			Facts:    append(slices.Clone(CodeHostFacts), "fresh_approvals", "merge_queue"),
			ByFacts:  []string{"approvals", "fresh_approvals"},
		},
	},
}

// RoleOf is the connection's role: the tracker's if it is the tracker, else the
// code host's. It is nil if the connection is unknown or its kind lacks the role.
func (w *Workspace) RoleOf(name string) *Role {
	provider, ok := w.Providers[name]
	if !ok {
		return nil
	}
	kind, ok := Kinds[provider.Kind]
	if !ok {
		return nil
	}
	if w.IsTracker(name) {
		return kind.Tracker
	}
	return kind.CodeHost
}

// IsTracker reports whether the policy names this connection as its tracker.
func (w *Workspace) IsTracker(name string) bool { return name != "" && name == w.Policy.Tracker }

// TrackerLabel is the tracker's name for people to read.
func (w *Workspace) TrackerLabel() string {
	if kind, ok := Kinds[w.Providers[w.Policy.Tracker].Kind]; ok && kind.Label != "" {
		return kind.Label
	}
	return "tracker"
}

// Settings lists the names of the settings that are set.
func (p Provider) Settings() []string { return p.settings() }

// settings lists the names of the settings that are set.
func (p Provider) settings() []string {
	var set []string
	for name, used := range map[string]bool{
		"site":            p.Site != "",
		"scope":           p.Scope != "",
		"default_project": p.DefaultProject != "",
		"statuses":        len(p.Statuses) != 0,
		"org":             p.Org != "",
		"lookback":        p.Lookback != "",
		"project":         p.Project != 0,
		"view":            p.View != 0,
	} {
		if used {
			set = append(set, name)
		}
	}
	slices.Sort(set)
	return set
}

func checkJira(name string, p Provider, add func(string, ...any)) {
	u, err := url.Parse("https://" + p.Site)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(p.Site, "<> ") {
		add("providers.yml: %s.site must be a Jira Cloud hostname", name)
	}
	if strings.TrimSpace(p.Scope) == "" {
		add("providers.yml: %s.scope is required", name)
	}
	if p.DefaultProject != "" && !projectPattern.MatchString(p.DefaultProject) {
		add("providers.yml: %s.default_project must be a Jira project key such as APP", name)
	}
}

func checkGitHub(name string, p Provider, add func(string, ...any)) {
	if !orgPattern.MatchString(p.Org) {
		add("providers.yml: %s.org must be a GitHub organisation name", name)
	}
}
