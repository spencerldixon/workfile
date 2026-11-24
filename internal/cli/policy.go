package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

var policyCommand = command{
	flags: []string{"dir", "json", "lint"},
	args: func(o options) error {
		if len(o.keys) > 0 {
			return errors.New("policy takes no positional arguments")
		}
		return nil
	},
	noWorkspace: true,
	run:         showPolicy,
}

func showPolicy(_ context.Context, e env) (int, error) {
	v, o := e.view, e.o
	v.width = max(v.width, fullWidth(v.out))
	w, err := workfile.LoadPolicy(o.dir)
	var pe *workfile.PolicyError
	var problems, warnings []workfile.Finding
	var gaps []workfile.Gap
	switch {
	case errors.As(err, &pe):
		for _, problem := range pe.Problems {
			problems = append(problems, workfile.Finding{Rule: "invalid-policy", Severity: "error", Message: problem})
		}
		w = nil
	case err != nil:
		return 0, err
	default:
		if !o.lint {
			return 0, inspectPolicy(v, w, o.json)
		}
		warnings, gaps = w.Policy.Lint(), w.Policy.Gaps()
	}
	code := 0
	if len(problems) > 0 {
		code = 1
	}
	if o.json && !o.lint {
		return code, encodeCleanJSON(v.out, policyReport{
			SchemaVersion: 1, Command: "policy", ExitCode: code, Problems: problems,
		})
	}
	if o.json {
		command := "policy"
		if o.legacyProve {
			command = "lint"
		}
		return code, writeLint(v.out, w, problems, warnings, gaps, code, command)
	}
	m := newLintModel(v, w, problems, warnings, gaps)
	if !o.lint {
		m.invocation = "wf policy"
	}
	m.gfx = v.outputGraphics()
	fmt.Fprint(v.out, m.render())
	return code, nil
}

type policyReport struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	ExitCode      int                `json:"exit_code"`
	PolicyHash    string             `json:"policy_hash,omitempty"`
	Workspace     *policyWorkspace   `json:"workspace,omitempty"`
	Problems      []workfile.Finding `json:"problems,omitempty"`
}

type policyWorkspace struct {
	Root      string                    `json:"root"`
	Name      string                    `json:"name,omitempty"`
	Tracker   string                    `json:"tracker"`
	Stages    []string                  `json:"stages"`
	Moves     []policyMove              `json:"moves"`
	Gates     map[string][]policyRule   `json:"gates"`
	Providers map[string]policyProvider `json:"providers"`
	Files     map[string]string         `json:"files,omitempty"`
}

type policyMove struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Backward bool     `json:"backward"`
	Requires []string `json:"requires"`
}

type policyRule struct {
	ID      string   `json:"id,omitempty"`
	If      string   `json:"if"`
	When    string   `json:"when,omitempty"`
	By      []string `json:"by,omitempty"`
	Message string   `json:"message,omitempty"`
}

// List public connection fields explicitly so credential settings stay private.
type policyProvider struct {
	Kind           string                    `json:"kind"`
	Site           string                    `json:"site,omitempty"`
	Org            string                    `json:"org,omitempty"`
	Scope          string                    `json:"scope,omitempty"`
	DefaultProject string                    `json:"default_project,omitempty"`
	Project        int                       `json:"project,omitempty"`
	Projects       workfile.Names            `json:"projects,omitempty"`
	Repos          workfile.Names            `json:"repos,omitempty"`
	SkipStatuses   workfile.Names            `json:"skip_statuses,omitempty"`
	KeepDoneFor    string                    `json:"keep_done_for,omitempty"`
	Statuses       map[string]workfile.Names `json:"statuses,omitempty"`
}

func describePolicy(w *workfile.Workspace) *policyWorkspace {
	p := w.Policy
	summary := &policyWorkspace{
		Root: filepath.Dir(w.Dir), Name: p.Name, Tracker: p.Tracker, Stages: p.States,
		Moves: []policyMove{}, Gates: make(map[string][]policyRule),
		Providers: make(map[string]policyProvider), Files: make(map[string]string),
	}
	for _, from := range p.States {
		for _, route := range p.Routes(from) {
			summary.Moves = append(summary.Moves, policyMove{
				From: from, To: route.To, Backward: route.Backward,
				Requires: append([]string{}, route.Requires...),
			})
		}
	}
	for name, rules := range p.Gates {
		summary.Gates[name] = make([]policyRule, 0, len(rules))
		for _, rule := range rules {
			summary.Gates[name] = append(summary.Gates[name], policyRule{
				ID: rule.ID, If: rule.If, When: rule.When, By: rule.By, Message: rule.Message,
			})
		}
	}
	for name, provider := range w.Providers {
		summary.Providers[name] = policyProvider{
			Kind: provider.Kind, Site: provider.Site, Org: provider.Org, Scope: provider.Scope,
			DefaultProject: provider.DefaultProject, Project: provider.Project,
			Projects: provider.Projects, Repos: provider.Repos, Statuses: provider.Statuses,
			SkipStatuses: provider.SkipStatuses, KeepDoneFor: provider.KeepDoneFor,
		}
	}
	for label, paths := range map[string][]string{
		"instructions": {"AGENTS.md"},
		"pr_template":  {".github/pull_request_template.md", "pull_request_template.md", "docs/pull_request_template.md"},
	} {
		for _, path := range paths {
			if info, err := os.Stat(filepath.Join(summary.Root, path)); err == nil && info.Mode().IsRegular() {
				summary.Files[label] = path
				break
			}
		}
	}
	return summary
}

func inspectPolicy(v view, w *workfile.Workspace, asJSON bool) error {
	summary := describePolicy(w)
	if asJSON {
		return encodeCleanJSON(v.out, policyReport{
			SchemaVersion: 1, Command: "policy", PolicyHash: w.PolicyHash(), Workspace: summary,
		})
	}
	m := newLintModel(v, w, nil, nil, nil)
	m.gfx = v.outputGraphics()
	m.policy = summary
	_, err := fmt.Fprint(v.out, m.render())
	return err
}

func (m *lintModel) policySections(p *policyWorkspace) [][]string {
	title := []string{m.t.accent("wf policy")}
	if p.Name != "" {
		title = append(title, m.wrap(p.Name, "", m.t.dim)...)
	}
	gates := []string{m.t.bold("Gates")}
	for _, name := range workfile.SortedKeys(p.Gates) {
		gates = append(gates, m.wrap(name, "  ", m.t.bold)...)
		for _, rule := range p.Gates[name] {
			parts := []string{rule.If}
			if rule.When != "" {
				parts = append(parts, "when "+rule.When)
			}
			if len(rule.By) > 0 {
				parts = append(parts, "by "+strings.Join(rule.By, ", "))
			}
			gates = append(gates, m.wrap(strings.Join(parts, " · "), "    ", m.t.dim)...)
		}
	}
	providers := []string{m.t.bold("Providers")}
	for _, name := range workfile.SortedKeys(p.Providers) {
		provider := p.Providers[name]
		details := []string{name, provider.Kind, provider.Site, provider.Org}
		if name == p.Tracker {
			details = append(details, "tracker")
		}
		details = slices.DeleteFunc(details, func(value string) bool { return value == "" })
		providers = append(providers, m.wrap(strings.Join(details, " · "), "  ", m.t.bold)...)
		if len(provider.Projects) > 0 {
			providers = append(providers, m.wrap("Projects "+strings.Join(provider.Projects, ", "), "    ", m.t.dim)...)
		}
		if provider.Project > 0 {
			providers = append(providers, m.wrap(fmt.Sprintf("Project #%d", provider.Project), "    ", m.t.dim)...)
		}
		if len(provider.Repos) > 0 {
			providers = append(providers, m.wrap("Repos "+strings.Join(provider.Repos, ", "), "    ", m.t.dim)...)
		}
		if provider.Scope != "" {
			providers = append(providers, m.wrap("Scope "+provider.Scope, "    ", m.t.dim)...)
		}
		for _, stage := range p.Stages {
			if statuses := provider.Statuses[stage]; len(statuses) > 0 {
				providers = append(providers, m.wrap(stage+" → "+strings.Join(statuses, ", "), "    ", m.t.dim)...)
			}
		}
	}
	sections := [][]string{title, m.flow()}
	if len(gates) > 1 {
		sections = append(sections, gates)
	}
	sections = append(sections, providers)
	if len(p.Files) > 0 {
		files := []string{m.t.bold("Project files")}
		for _, path := range workfile.SortedKeys(p.Files) {
			files = append(files, m.wrap(p.Files[path], "  ", m.t.dim)...)
		}
		sections = append(sections, files)
	}
	return sections
}
