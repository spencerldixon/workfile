package workfile

import (
	"net/url"
	"slices"
	"strconv"
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
	Label    string                // the name for the scope line, if not the kind's Label
	Scope    func(Provider) string // what a tracker connection reads, for the scope line
	Settings []string              // allowed providers.yml settings in this role
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
			Settings: []string{"site", "scope", "default_project", "statuses", "projects", "skip_statuses", "keep_done_for"},
			Check:    checkJira,
			Scope:    func(p Provider) string { return strings.Join(p.Projects, ", ") },
			Facts:    slices.Clone(TrackerFacts),
			ByFacts:  []string{"labels"},
		},
	},
	"github": {
		Label: "GitHub",
		Auth:  []string{"token"},
		Keys:  func(p Provider) Keys { return githubKeys{org: p.Org} },
		Tracker: &Role{
			Settings: []string{"org", "project", "statuses", "default_project", "repos", "skip_statuses", "keep_done_for", "scope"},
			Check:    checkGitHubTracker,
			Label:    "GitHub Issues",
			Scope: func(p Provider) string {
				return "project " + strconv.Itoa(p.Project) + " · " + reposText(p)
			},
			Facts: slices.Clone(TrackerFacts),
		},
		CodeHost: &Role{
			Settings: []string{"org", "lookback", "repos"},
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

// reposText says which repositories a connection reads.
func reposText(p Provider) string {
	if p.AllRepos() {
		return "all repos in " + p.Org
	}
	return "repos " + strings.Join(p.Repos, ", ")
}

// ScopeLine says what Workfile reads, in one line: the tracker and its scope,
// then the repositories its code host reads. For example
// "Jira · APP, WEB · repos web, api, mobile". It is "" when there is no tracker.
func (w *Workspace) ScopeLine() string {
	tracker, ok := w.Providers[w.Policy.Tracker]
	if !ok {
		return ""
	}
	label := w.TrackerLabel()
	scope := ""
	if role := Kinds[tracker.Kind].Tracker; role != nil {
		if role.Label != "" {
			label = role.Label
		}
		if role.Scope != nil {
			scope = role.Scope(tracker)
		}
	}
	parts := []string{label}
	if scope != "" {
		parts = append(parts, scope)
	}
	if !strings.Contains(scope, "repos") {
		for _, name := range SortedKeys(w.Providers) {
			host := w.Providers[name]
			if name != w.Policy.Tracker && Kinds[host.Kind].CodeHost != nil && len(host.Repos) > 0 {
				parts = append(parts, reposText(host))
			}
		}
	}
	return strings.Join(parts, " · ")
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
		"projects":        len(p.Projects) != 0,
		"skip_statuses":   len(p.SkipStatuses) != 0,
		"keep_done_for":   p.KeepDoneFor != "",
		"repos":           len(p.Repos) != 0,
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
	if len(p.Projects) == 0 {
		add("providers.yml: %s.projects is required, such as projects: [APP]. scope is now an extra filter on top.", name)
	}
	for _, project := range p.Projects {
		if !projectPattern.MatchString(project) {
			add("providers.yml: %s.projects must be Jira project keys such as APP", name)
			break
		}
	}
	checkKeepDoneFor(name, p, add)
	if p.DefaultProject != "" && !projectPattern.MatchString(p.DefaultProject) {
		add("providers.yml: %s.default_project must be a Jira project key such as APP", name)
	}
}

// checkKeepDoneFor requires keep_done_for, such as 14d or 2w.
func checkKeepDoneFor(name string, p Provider, add func(string, ...any)) {
	if !keepDonePattern.MatchString(p.KeepDoneFor) {
		add("providers.yml: %s.keep_done_for is required, such as keep_done_for: 14d", name)
	}
}

// checkRepoNames checks each listed repository name, or the single value all.
func checkRepoNames(name string, p Provider, add func(string, ...any)) {
	if len(p.Repos) > 1 && slices.Contains(p.Repos, "all") {
		add("providers.yml: %s.repos can say all, or list repositories, not both", name)
		return
	}
	for _, repo := range p.Repos {
		if repo != "all" && !repoNamePattern.MatchString(repo) {
			add("providers.yml: %s.repos must be repository names such as web, or all", name)
			return
		}
	}
}

func checkGitHub(name string, p Provider, add func(string, ...any)) {
	checkGitHubOrg(name, p, add)
	if len(p.Repos) == 0 {
		add("providers.yml: %s.repos is required: list the team's repositories, such as repos: [web, api], or write repos: all", name)
	}
	checkRepoNames(name, p, add)
}

func checkGitHubOrg(name string, p Provider, add func(string, ...any)) {
	if p.View != 0 {
		add("providers.yml: %s.view is no longer read. Write the filter in providers.yml: repos, skip_statuses, keep_done_for and scope.", name)
	}
	if !orgPattern.MatchString(p.Org) {
		add("providers.yml: %s.org must be a GitHub organisation name", name)
	}
}
