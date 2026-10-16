package workfile

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Automation is one "when this happens to my work, do that". The policy says
// what is allowed; an automation reacts to changes the policy can see and
// starts a command or an agent the person chose. It never skips a gate.
type Automation struct {
	ID    string   `yaml:"id"`
	On    Trigger  `yaml:"on"`
	Stage Names    `yaml:"stage"`
	When  string   `yaml:"when"`
	Scope string   `yaml:"scope"`
	Run   []string `yaml:"run"`
	Agent string   `yaml:"agent"`
	Mode  string   `yaml:"mode"`

	// Condition is the parsed becomes trigger; Guard is the parsed when.
	Condition *Expression `yaml:"-"`
	Guard     *Expression `yaml:"-"`
}

// Trigger names exactly one change to watch for.
type Trigger struct {
	Ready   string `yaml:"ready"`
	Enters  string `yaml:"enters"`
	Becomes string `yaml:"becomes"`
}

// AutomationFile is one person's automations. A shared file lives in
// .workfile/automations/<person>.yml; a private file lives in the person's own
// config folder and lists the repositories it may run in.
type AutomationFile struct {
	Repos       []string     `yaml:"repos"`
	Automations []Automation `yaml:"automations"`

	Path    string `yaml:"-"`
	Person  string `yaml:"-"` // the shared file's person in people.yml
	Private bool   `yaml:"-"`
}

const (
	ModeObserve = "observe"
	ModeAsk     = "ask"
	ModeAuto    = "auto"
)

// EffectiveMode is the mode with its default. A new automation only reports.
func (a Automation) EffectiveMode() string {
	if a.Mode == "" {
		return ModeObserve
	}
	return a.Mode
}

// Everyone reports whether the automation looks at every ticket in the search,
// not only the person's own work.
func (a Automation) Everyone() bool { return a.Scope == "all" }

// PerRecord is true when the trigger is about each linked PR, so it fires once
// for each PR that changes.
func (a Automation) PerRecord() bool { return a.Condition != nil && a.Condition.Record() }

// Trigger describes the trigger in a few words, for lists and logs, such as
// "can move to done" or "github.approvals >= 1 turns true".
func (a Automation) Trigger() string {
	switch {
	case a.On.Ready != "":
		return "can move to " + a.On.Ready
	case a.On.Enters != "":
		return "enters " + a.On.Enters
	default:
		return strings.TrimSpace(a.On.Becomes) + " turns true"
	}
}

// Describe says when the automation fires, as a sentence part, such as
// "when it can move to done".
func (a Automation) Describe() string {
	if a.On.Becomes != "" {
		return "when " + a.Trigger()
	}
	return "when it " + a.Trigger()
}

// Label names the file for people: "shared" or "private".
func (f *AutomationFile) Label() string {
	if f.Private {
		return "private"
	}
	return "shared"
}

var workspaceNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var placeholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

// ValidName reports whether a workspace name is safe to use as a folder name.
// The name comes from the repository, so it must never climb out of a folder.
func ValidName(name string) bool { return len(name) <= 64 && workspaceNamePattern.MatchString(name) }

// Placeholders lists the values an automation's action can use, which depend
// on its trigger: {to} only for ready, {record} only for a trigger about PRs.
func (a Automation) Placeholders() []string {
	names := []string{"ticket", "title", "url", "stage", "automation"}
	if a.On.Ready != "" {
		names = append(names, "to")
	}
	if a.PerRecord() {
		names = append(names, "record", "record_url")
	}
	return names
}

// loadSharedAutomations reads .workfile/automations/. The folder is optional.
func (w *Workspace) loadSharedAutomations() error {
	dir := filepath.Join(w.Dir, "automations")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if strings.HasSuffix(name, ".yaml") {
			return fmt.Errorf("automations/%s: name automation files <person>.yml", name)
		}
		if !strings.HasSuffix(name, ".yml") {
			continue
		}
		f := &AutomationFile{Path: filepath.Join(dir, name), Person: strings.TrimSuffix(name, ".yml")}
		if err := ReadYAML(f.Path, f, false); err != nil {
			return err
		}
		w.Automations = append(w.Automations, f)
	}
	return nil
}

// ReadPrivateAutomations reads a private automations file. It returns nil
// without an error when the file does not exist.
func (w *Workspace) ReadPrivateAutomations(path string) (*AutomationFile, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	f := &AutomationFile{Path: path, Private: true}
	if err := ReadYAML(path, f, false); err != nil {
		return nil, err
	}
	if problems := w.CheckAutomations(f); len(problems) > 0 {
		return f, errors.New(strings.Join(problems, "\n  "))
	}
	return f, nil
}

// SharedFile is a person's shared file, or nil.
func (w *Workspace) SharedFile(person string) *AutomationFile {
	for _, f := range w.Automations {
		if f.Person == person {
			return f
		}
	}
	return nil
}

// CheckAutomations reports every mistake in one file. Mistakes are errors, not
// guesses, the same as policy mistakes. It parses expressions in place.
func (w *Workspace) CheckAutomations(f *AutomationFile) []string {
	var problems []string
	at := "automations/" + f.Person + ".yml"
	if f.Private {
		at = f.Path
	}
	add := func(format string, args ...any) { problems = append(problems, at+": "+fmt.Sprintf(format, args...)) }
	if f.Private {
		if len(f.Repos) == 0 {
			add("repos must list the git remotes this file may run in, such as github.com/example-team/web")
		}
		for _, repo := range f.Repos {
			if NormalizeRemote(repo) == "" {
				add("repos has %q, which is not a git remote such as github.com/example-team/web", repo)
			}
		}
	} else {
		if _, ok := w.People[f.Person]; !ok {
			add("%s is not a person in people.yml; name the file after the person", f.Person)
		}
		if len(f.Repos) > 0 {
			add("repos belongs in a private file; a shared file always runs in its own repository")
		}
	}
	if len(f.Automations) == 0 {
		add("automations must list at least one automation")
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	for _, state := range w.Policy.States {
		for _, route := range w.Policy.Routes(state) {
			targets[route.To] = true
		}
	}
	for i := range f.Automations {
		a := &f.Automations[i]
		name := a.ID
		if name == "" {
			name = fmt.Sprintf("automation %d", i+1)
		}
		bad := func(format string, args ...any) { add("%s: %s", name, fmt.Sprintf(format, args...)) }
		if !ruleIDPattern.MatchString(a.ID) || len(a.ID) > 128 {
			bad("id must be at most 128 letters, digits, dots, underscores, colons, slashes or hyphens, starting with a letter or digit")
		}
		if seen[a.ID] {
			bad("id is used twice in this file")
		}
		seen[a.ID] = true
		triggers := 0
		for _, set := range []string{a.On.Ready, a.On.Enters, a.On.Becomes} {
			if strings.TrimSpace(set) != "" {
				triggers++
			}
		}
		if triggers != 1 {
			bad("on needs exactly one of ready, enters or becomes")
		}
		if a.On.Ready != "" && !targets[a.On.Ready] {
			bad("ready: no move in policy.yml leads to %s", a.On.Ready)
		}
		if a.On.Enters != "" && !slices.Contains(w.Policy.States, a.On.Enters) {
			bad("enters: %s is not a stage in policy.yml", a.On.Enters)
		}
		a.Condition, a.Guard = nil, nil
		if a.On.Becomes != "" {
			if expr, err := w.parseExpression(a.On.Becomes); err != nil {
				bad("becomes: %s", err)
			} else {
				a.Condition = &expr
			}
		}
		if a.When != "" {
			if expr, err := w.parseExpression(a.When); err != nil {
				bad("when: %s", err)
			} else {
				a.Guard = &expr
				if a.Condition != nil && a.Condition.Record() && expr.Record() && expr.Provider != a.Condition.Provider {
					bad("becomes and when cannot be about PRs from two providers")
				}
			}
		}
		for _, stage := range a.Stage {
			if !slices.Contains(w.Policy.States, stage) {
				bad("stage: %s is not a stage in policy.yml", stage)
			}
		}
		if a.Scope != "" && a.Scope != "mine" && a.Scope != "all" {
			bad("scope must be mine or all")
		}
		if a.Mode != "" && a.Mode != ModeObserve && a.Mode != ModeAsk && a.Mode != ModeAuto {
			bad("mode must be observe, ask or auto")
		}
		hasRun, hasAgent := len(a.Run) > 0, strings.TrimSpace(a.Agent) != ""
		if hasRun == hasAgent {
			bad("needs exactly one of run or agent")
		}
		if hasRun && strings.TrimSpace(a.Run[0]) == "" {
			bad("run must start with a program")
		}
		allowed := a.Placeholders()
		for _, text := range append(slices.Clone(a.Run), a.Agent) {
			for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
				if !slices.Contains(allowed, match[1]) {
					bad("{%s} is not available here; use %s", match[1], braces(allowed))
				}
			}
		}
	}
	return slices.Compact(problems)
}

func braces(names []string) string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = "{" + name + "}"
	}
	return strings.Join(out, ", ")
}

// CheckUniqueIDs reports an id used in both of a person's files. The same id
// is an error, not an override, so it is always clear which one ran.
func CheckUniqueIDs(shared, private *AutomationFile) []string {
	if shared == nil || private == nil {
		return nil
	}
	var problems []string
	for _, a := range private.Automations {
		if slices.ContainsFunc(shared.Automations, func(b Automation) bool { return a.ID == b.ID }) {
			problems = append(problems, fmt.Sprintf("%s: %s is also in automations/%s.yml; ids must be unique across your shared and private files", private.Path, a.ID, shared.Person))
		}
	}
	return problems
}

var scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// NormalizeRemote turns any form of a git remote into host/path, so
// git@github.com:example-team/web.git and https://github.com/example-team/web
// compare equal. It returns "" for something that is not a remote.
func NormalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" || strings.ContainsAny(remote, " \t\n") {
		return ""
	}
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" || u.Scheme == "file" {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if m := scpRemote.FindStringSubmatch(remote); m != nil && !strings.HasPrefix(remote, "/") && !strings.HasPrefix(remote, ".") {
		host, path = m[1], m[2]
	} else if first, rest, ok := strings.Cut(remote, "/"); ok && strings.Contains(first, ".") {
		host, path = first, rest
	} else {
		return ""
	}
	path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if !hostPattern.MatchString(host) || path == "" || strings.Contains(path, "..") {
		return ""
	}
	return strings.ToLower(host + "/" + path)
}
