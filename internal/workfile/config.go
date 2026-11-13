package workfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

var namePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
var ruleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]*$`)
var credentialPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
var workspaceNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var orgPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)
var projectPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var yamlLinePattern = regexp.MustCompile(`\bline [0-9]+`)

// Names accepts either a single Jira status or a list of statuses.
type Names []string

func (n *Names) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		*n = []string{node.Value}
		return nil
	}
	var values []string
	if node.Kind != yaml.SequenceNode {
		return errors.New("expected a status name or list")
	}
	if err := node.Decode(&values); err != nil {
		return err
	}
	*n = values
	return nil
}

func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("--dir must name an existing directory")
	}
	for {
		candidate := filepath.Join(dir, ".workfile")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no .workfile/ found here or in a parent directory; copy example/.workfile to your project")
		}
		dir = parent
	}
}

func Load(start string) (*Workspace, error) {
	dir, err := Find(start)
	if err != nil {
		return nil, err
	}
	w := &Workspace{Dir: dir}
	if err = ReadYAML(filepath.Join(dir, "policy.yml"), &w.Policy, false); err != nil {
		return nil, err
	}
	if err = ReadYAML(filepath.Join(dir, "providers.yml"), &w.Providers, false); err != nil {
		return nil, err
	}
	if err = ReadYAML(filepath.Join(dir, "people.yml"), &w.People, true); err != nil {
		return nil, err
	}
	if err = w.validate(); err != nil {
		return nil, err
	}
	// Read after validation, so the name is safe to use as a folder.
	if w.Settings, err = ReadWorkspaceSettings(w.Policy.Name); err != nil {
		return nil, err
	}
	return w, nil
}

// YAML parser errors can contain pasted secrets. Report the file without echoing its values.
func ReadYAML(path string, target any, optional bool) error {
	data, err := os.ReadFile(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		location := path
		if line := yamlLinePattern.FindString(err.Error()); line != "" {
			location += " (" + line + ")"
		}
		return fmt.Errorf("%s: invalid YAML, unknown field, or wrong value type; check the documented format", location)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: use one YAML document per file", path)
	}
	return nil
}

func (w *Workspace) validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	p := &w.Policy
	if p.Name != "" && !ValidName(p.Name) {
		add("policy.yml: name must be at most 64 lowercase letters, digits and single dashes, such as example-web")
	}
	if len(p.States) == 0 {
		add("policy.yml: states must list the workflow in order")
	}
	seen := map[string]bool{}
	for _, state := range p.States {
		if state == "" || seen[state] {
			add("policy.yml: states must be nonempty and unique")
		}
		seen[state] = true
		t, ok := p.Transitions[state]
		if !ok {
			add("policy.yml: missing transition for %s", state)
			continue
		}
		if t.End && (len(t.To) > 0 || len(t.Requires) > 0 || t.RequiresByDestination != nil || t.Routes != nil) {
			add("policy.yml: terminal state %s cannot have to, routes or requires", state)
		}
		if t.Routes != nil && len(t.To) > 0 {
			add("policy.yml: %s must use either to or routes", state)
		}
		if t.RequiresByDestination != nil && t.Routes != nil {
			add("policy.yml: %s must use destination-specific requires with to, not routes", state)
		}
		for _, destination := range SortedKeys(t.RequiresByDestination) {
			if !slices.Contains(t.To, destination) {
				add("policy.yml: %s.requires refers to %s, which is not in to", state, destination)
			}
		}
		targets := append(slices.Clone(t.To), SortedKeys(t.Routes)...)
		if !t.End && len(targets) == 0 {
			add("policy.yml: %s needs a transition or end: true", state)
		}
		for i, target := range targets {
			if !slices.Contains(p.States, target) {
				add("policy.yml: %s refers to unknown state %s", state, target)
			}
			if target == state || slices.Contains(targets[:i], target) {
				add("policy.yml: %s has a duplicate or self transition to %s", state, target)
			}
		}
		gates := slices.Clone(t.Requires)
		for _, requirements := range t.RequiresByDestination {
			gates = append(gates, requirements...)
		}
		for _, requirements := range t.Routes {
			gates = append(gates, requirements...)
		}
		for _, gate := range gates {
			if _, ok := p.Gates[gate]; !ok {
				add("policy.yml: %s requires unknown gate %s", state, gate)
			}
		}
	}
	if len(p.States) > 0 {
		reached := map[string]bool{p.States[0]: true}
		for _, state := range p.States {
			if !reached[state] {
				add("policy.yml: %s needs a forward path from %s", state, p.States[0])
				continue
			}
			for _, route := range p.Routes(state) {
				if !route.Backward {
					reached[route.To] = true
				}
			}
		}
	}
	for state := range p.Transitions {
		if !seen[state] {
			add("policy.yml: transition %s is not in states", state)
		}
	}
	for _, stage := range SortedKeys(p.Invariants) {
		gates := p.Invariants[stage]
		if !seen[stage] {
			add("policy.yml: invariants refers to unknown state %s", stage)
		}
		if len(gates) == 0 {
			add("policy.yml: invariants.%s needs at least one gate", stage)
		}
		for i, gate := range gates {
			if _, ok := p.Gates[gate]; !ok {
				add("policy.yml: invariants.%s refers to unknown gate %s", stage, gate)
			}
			if slices.Contains(gates[:i], gate) {
				add("policy.yml: invariants.%s lists %s twice", stage, gate)
			}
		}
	}
	for _, name := range SortedKeys(w.Providers) {
		provider := w.Providers[name]
		if !namePattern.MatchString(name) {
			add("providers.yml: instance names must use lowercase letters, digits and underscores")
		}
		if provider.Kind == "" {
			provider.Kind = name
		}
		w.Providers[name] = provider
		kind, known := Kinds[provider.Kind]
		role := w.RoleOf(name)
		switch {
		case !known:
			add("providers.yml: %s must use a known provider (%s)", name, strings.Join(SortedKeys(Kinds), ", "))
		case role == nil && w.IsTracker(name):
			// Reported below, once, as a policy.yml problem.
		case role == nil:
			add("providers.yml: %s is a %s connection and cannot be used as a code host; only the tracker can be one", name, kind.Label)
		default:
			for _, setting := range provider.settings() {
				if !slices.Contains(role.Settings, setting) {
					add("providers.yml: %s.%s is not a setting of a %s connection", name, setting, kind.Label)
				}
			}
			provider.Site = strings.TrimSuffix(strings.TrimPrefix(provider.Site, "https://"), "/")
			if role.Check != nil {
				role.Check(name, provider, add)
			}
			if w.IsTracker(name) {
				statuses := map[string]bool{}
				for _, state := range p.States {
					if len(provider.Statuses[state]) == 0 {
						add("providers.yml: %s.statuses needs %s", name, state)
					}
					for _, status := range provider.Statuses[state] {
						key := strings.ToLower(strings.TrimSpace(status))
						if key == "" || statuses[key] {
							add("providers.yml: %s.statuses must contain nonempty, unique statuses", name)
						}
						statuses[key] = true
					}
				}
				for state := range provider.Statuses {
					if !seen[state] {
						add("providers.yml: %s.statuses has unknown state %s", name, state)
					}
				}
			}
		}
		for _, field := range kind.Auth {
			if !credentialPattern.MatchString(provider.Auth[field]) {
				add("providers.yml: %s.auth.%s must reference a credential as ${NAME}", name, field)
			}
		}
		for field := range provider.Auth {
			if known && !slices.Contains(kind.Auth, field) {
				add("providers.yml: %s has an unknown auth field", name)
			}
		}
		w.Providers[name] = provider
	}
	if kind, ok := Kinds[w.Providers[p.Tracker].Kind]; !ok || kind.Tracker == nil {
		add("policy.yml: tracker must name a configured connection whose provider can be a tracker")
	}
	for person, identities := range w.People {
		for instance, id := range identities {
			if instance == "name" {
				continue
			}
			if _, ok := w.Providers[instance]; !ok {
				add("people.yml: %s refers to unknown instance %s", person, instance)
			}
			if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "<>") {
				add("people.yml: %s needs a real %s identity", person, instance)
			}
		}
	}
	ruleIDs := map[string]bool{}
	for _, gate := range SortedKeys(p.Gates) {
		rules := p.Gates[gate]
		if gate == "" || len(rules) == 0 {
			add("policy.yml: each gate needs a name and at least one rule")
		}
		for i := range rules {
			r := &rules[i]
			at := fmt.Sprintf("policy.yml: gates.%s rule %d", gate, i+1)
			if r.ID != "" && (!ruleIDPattern.MatchString(r.ID) || len(r.ID) > 128) {
				add("%s: id must be at most 128 ASCII letters, digits, dots, underscores, colons, slashes or hyphens, starting with a letter or digit", at)
			}
			id := ruleID(gate, *r)
			if ruleIDs[id] {
				add("%s: duplicate rule ID %s; give distinct rules explicit id values", at, id)
			}
			ruleIDs[id] = true
			expr, err := w.parseExpression(r.If)
			if err != nil {
				add("%s: %s", at, err)
				continue
			}
			r.Condition = expr
			if r.When != "" {
				guard, err := w.parseExpression(r.When)
				if err != nil {
					add("%s when: %s", at, err)
				} else {
					r.Guard = &guard
				}
				if expr.Record() && guard.Record() && expr.Provider != guard.Provider {
					add("%s: a rule cannot combine records from two linked providers", at)
				}
			}
			if len(r.By) > 0 {
				if role := w.RoleOf(expr.Provider); role == nil || !slices.Contains(role.ByFacts, expr.Fact) {
					add("%s: by is not supported for %s.%s", at, expr.Provider, expr.Fact)
				}
				for _, person := range r.By {
					if w.People[person][expr.Provider] == "" {
						add("%s: %s needs a %s identity in people.yml", at, person, expr.Provider)
					}
				}
			}
		}
		p.Gates[gate] = rules
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return errors.New(strings.Join(slices.Compact(problems), "\n  "))
	}
	return nil
}

// ValidName reports whether a workspace name is safe to use as a folder name.
// The name comes from the repository, so it must never climb out of a folder.
func ValidName(name string) bool { return len(name) <= 64 && workspaceNamePattern.MatchString(name) }

func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
