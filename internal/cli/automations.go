package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"workfile/internal/automation"
	"workfile/internal/workfile"
)

// automationSetup is what `wf automations` and `wf watch` both need: whose
// automations run here, what was saved last time, and how to run them.
type automationSetup struct {
	personal workfile.Personal
	who      map[string]string
	set      *automation.Set
	store    automation.Store
	trust    automation.Trust
	runner   automation.Runner
}

// loadAutomations finds the person's automations. Their identity is only
// asked for when it is needed: to find their shared file without me.person,
// or when needIdentity is set because scope: mine needs it.
func loadAutomations(ctx context.Context, w *workfile.Workspace, client source, needIdentity bool) (*automationSetup, error) {
	personal, err := workfile.ReadPersonal()
	if err != nil {
		return nil, err
	}
	s := &automationSetup{personal: personal, store: automation.Store{Dir: automation.StateDir(w)}}
	if needIdentity || personal.Person == "" && len(w.Automations) > 0 {
		// A list still works without the network; it just cannot tell whose
		// shared file is whose, and says how to set me.person.
		if s.who, err = identity(ctx, w, client); err != nil && needIdentity {
			return nil, err
		}
	}
	if s.trust, err = s.store.Trust(); err != nil {
		return nil, err
	}
	s.set = automation.Find(ctx, w, personal, s.who, s.trust)
	s.runner = automation.NewRunner(w, personal)
	s.runner.Trust = s.trust
	return s, nil
}

var automationsCommand = command{flags: []string{"dir", "all", "log"}, args: automationsArgs, run: automations}

func automationsArgs(o options) error {
	if len(o.keys) > 1 || len(o.keys) == 1 && o.keys[0] != "init" && o.keys[0] != "trust" {
		return errors.New("automations takes init, trust or nothing")
	}
	if len(o.keys) == 1 && (o.all || o.log != "") {
		return fmt.Errorf("automations %s takes no options", o.keys[0])
	}
	if o.all && o.log != "" {
		return errors.New("choose either --all or --log")
	}
	return nil
}

func automations(ctx context.Context, e env) (int, error) {
	v, w, client, o := e.view, e.w, e.client, e.o
	switch {
	case len(o.keys) == 1 && o.keys[0] == "init":
		return initAutomations(ctx, v, w)
	case o.all:
		fmt.Fprint(v.out, newEveryoneModel(v, w).render())
		return 0, nil
	}
	s, err := loadAutomations(ctx, w, client, false)
	if err != nil {
		return 2, err
	}
	switch {
	case len(o.keys) == 1 && o.keys[0] == "trust":
		return trustAutomations(v, s)
	case o.log != "":
		return showLog(v, s, o.log)
	}
	state, err := s.store.State()
	if err != nil {
		return 2, err
	}
	m := newAutomationsModel(v, w, s, state)
	fmt.Fprint(v.out, m.render())
	if len(m.problems()) > 0 {
		return 1, nil
	}
	return 0, nil
}

// screen holds what every drawn-once screen needs: the view and its theme.
type screen struct {
	v view
	t theme
}

func newScreen(v view) screen { return screen{v: v, t: newTheme(v.color, v.laneBackground())} }

func (s screen) panelWidth() int { return max(14, s.v.width-len(ticketIndent)) }
func (s screen) width() int      { return s.panelWidth() - 4 }

func (s screen) wrap(text string, style func(string) string) []string {
	lines := wrapped(text, s.width())
	for i, line := range lines {
		lines[i] = style(line)
	}
	return lines
}

// indented wraps text under a hanging indent.
func (s screen) indented(text, indent string, style func(string) string) []string {
	lines := wrapped(text, max(1, s.width()-runewidth.StringWidth(indent)))
	for i, line := range lines {
		lines[i] = indent + style(line)
	}
	return lines
}

// tagged puts a tag at the right edge of a line, or under it when the line is
// too narrow for both.
func (s screen) tagged(left, tag string, styleLeft, styleTag func(string) string) []string {
	if tag == "" {
		return s.wrap(left, styleLeft)
	}
	gap := s.width() - runewidth.StringWidth(left) - runewidth.StringWidth(tag)
	if gap >= 2 {
		return []string{styleLeft(left) + strings.Repeat(" ", gap) + styleTag(tag)}
	}
	return append(s.wrap(left, styleLeft), s.indented(tag, "  ", styleTag)...)
}

func (s screen) title(text string) string { return s.t.dim(strings.ToUpper(text)) }
func (s screen) verdict(text string, ok bool) string {
	if ok {
		return s.t.good("✓ ") + s.t.bold(strings.ToUpper(text))
	}
	return s.t.bad("✗ ") + s.t.bold(strings.ToUpper(text))
}

// mark is a sentence-case title with an icon, for blocks about one thing,
// such as an automation that finished.
func (s screen) mark(text string, ok bool) string {
	if ok {
		return s.t.good("✓ ") + s.t.bold(text)
	}
	return s.t.bad("✗ ") + s.t.bold(text)
}

func plainText(s string) string { return s }

// automationsModel draws `wf automations`. It answers: what will run for me
// in this workspace, and is anything stopping it? Problems come first, each
// with what to do. It is drawn once and printed.
type automationsModel struct {
	screen
	w     *workfile.Workspace
	s     *automationSetup
	state *automation.State
}

func newAutomationsModel(v view, w *workfile.Workspace, s *automationSetup, state *automation.State) *automationsModel {
	return &automationsModel{screen: newScreen(v), w: w, s: s, state: state}
}

func (m *automationsModel) Init() tea.Cmd                       { return nil }
func (m *automationsModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *automationsModel) View() tea.View                      { return tea.NewView(m.render()) }

func (m *automationsModel) render() string {
	sections := [][]string{m.heading(), m.problemPanel()}
	set := m.s.set
	if set.Shared != nil {
		sections = append(sections, m.file(set.Shared, set.SharedProblem))
	}
	if set.Private != nil {
		sections = append(sections, m.file(set.Private, set.PrivateProblem))
	}
	if set.Shared == nil && set.Private == nil {
		sections = append(sections, m.start())
	}
	return panels(m.t, m.panelWidth(), sections)
}

func (m *automationsModel) heading() []string {
	name := m.w.Policy.Name
	if name == "" {
		name = filepath.Base(automation.Root(m.w))
	}
	lines := []string{m.t.bold("Automations") + m.t.dim(" · "+name)}
	var facts []string
	if m.s.set.Person != "" {
		facts = append(facts, "for "+m.w.DisplayName(m.s.set.Person))
	}
	entries := m.s.set.Entries()
	ready := 0
	for _, e := range entries {
		if e.Blocked == "" {
			ready++
		}
	}
	facts = append(facts, quantity(len(entries), "automation", "automations"))
	if len(entries) > 0 {
		facts = append(facts, fmt.Sprintf("%d can run", ready))
	}
	return append(lines, m.wrap(strings.Join(facts, " · "), m.t.dim)...)
}

// problems lists what stops automations running, each as something to do.
func (m *automationsModel) problems() []string {
	set := m.s.set
	var problems []string
	if set.Shared != nil && set.SharedProblem != "" {
		problems = append(problems, "Shared file "+relative(m.w, set.Shared.Path)+": "+set.SharedProblem)
	}
	if set.PrivateProblem != "" {
		problems = append(problems, "Private file "+home(set.PrivatePath)+": "+set.PrivateProblem)
	}
	if set.Person == "" && set.SharedProblem != "" && len(m.w.Automations) > 0 {
		problems = append(problems, set.SharedProblem)
	}
	for _, e := range set.Entries() {
		if e.Blocked == automation.NoAgentProblem {
			problems = append(problems, "Set agent in "+home(workfile.PersonalPath("config.yml"))+" to run automations that ask an agent, such as agent: [claude, -p].")
			break
		}
	}
	return problems
}

func (m *automationsModel) problemPanel() []string {
	problems := m.problems()
	if len(problems) == 0 {
		return nil
	}
	lines := []string{m.verdict("Needs attention", false)}
	for i, problem := range problems {
		prefix := fmt.Sprintf("%d. ", i+1)
		for j, line := range wrapped(problem, max(1, m.width()-len(prefix))) {
			if j == 0 {
				lines = append(lines, m.t.bad(prefix)+line)
			} else {
				lines = append(lines, strings.Repeat(" ", len(prefix))+line)
			}
		}
	}
	// A file never approved is listed in full below; only show real changes.
	if !m.s.set.NeverApproved {
		for _, change := range m.s.set.Changed {
			lines = append(lines, "")
			lines = append(lines, m.changeLines(change)...)
		}
	}
	return lines
}

// changeLines shows what changed in a shared automation since approval.
func (m *automationsModel) changeLines(c automation.Change) []string {
	lines := []string{m.t.bold(c.ID)}
	if c.Before == nil {
		lines = append(lines, m.indented("new: "+approvedText(c.After), "  ", plainText)...)
		return lines
	}
	b, a := *c.Before, c.After
	for _, field := range []struct{ name, before, after string }{
		{"run", strings.Join(automation.Quote(b.Run), " "), strings.Join(automation.Quote(a.Run), " ")},
		{"agent", oneLine(b.Agent), oneLine(a.Agent)},
		{"mode", b.Mode, a.Mode},
		{"scope", scopeText(b.Scope), scopeText(a.Scope)},
	} {
		if field.before != field.after {
			lines = append(lines, m.indented(field.name+" was: "+orNone(field.before), "  ", m.t.dim)...)
			lines = append(lines, m.indented(field.name+" now: "+orNone(field.after), "  ", plainText)...)
		}
	}
	for _, path := range automation.ChangedScripts(b.Scripts, a.Scripts) {
		lines = append(lines, m.indented("script changed: "+path, "  ", plainText)...)
	}
	return lines
}

func approvedText(a automation.Approved) string {
	action := "runs " + strings.Join(automation.Quote(a.Run), " ")
	if a.Agent != "" {
		action = "asks your agent: " + oneLine(a.Agent)
	}
	return action + " · " + a.Mode + " · " + scopeText(a.Scope)
}

func scopeText(scope string) string {
	if scope == "all" {
		return "every ticket"
	}
	return "your work"
}

func orNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// oneLine shortens a prompt to its first sentence, or about 60 characters.
func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if i := strings.Index(text, ". "); i >= 0 && i < 80 {
		return text[:i+1] + " …"
	}
	if runewidth.StringWidth(text) <= 70 {
		return text
	}
	cut := runewidth.Truncate(text, 60, "")
	if i := strings.LastIndex(cut, " "); i > 30 {
		cut = cut[:i]
	}
	return cut + " …"
}

func (m *automationsModel) file(f *workfile.AutomationFile, problem string) []string {
	path := relative(m.w, f.Path)
	if f.Private {
		path = home(f.Path)
	}
	label := "Shared with the team"
	if f.Private {
		label = "Private"
	}
	lines := []string{m.verdict(label, problem == "")}
	lines = append(lines, m.wrap(path, m.t.dim)...)
	if problem != "" {
		lines = append(lines, m.wrap("Not running here: see Needs attention.", m.t.bad)...)
	}
	for _, a := range f.Automations {
		lines = append(lines, "")
		lines = append(lines, m.automation(a)...)
		if problem == "" && a.Agent != "" && m.s.set.NoAgent {
			lines = append(lines, m.indented("not running: "+automation.NoAgentProblem, "  ", m.t.bad)...)
		}
	}
	return lines
}

func (m *automationsModel) automation(a workfile.Automation) []string {
	lines := m.tagged(a.ID, a.EffectiveMode(), m.t.bold, m.t.dim)
	when := a.Describe()
	if len(a.Stage) > 0 {
		when += " · in " + strings.Join(a.Stage, " or ")
	}
	if a.When != "" {
		when += " · if " + strings.TrimSpace(a.When)
	}
	if a.Everyone() {
		when += " · every ticket"
	}
	lines = append(lines, m.indented(when, "  ", plainText)...)
	lines = append(lines, m.indented(actionText(a), "  ", plainText)...)
	if last, ok := m.state.Last(a.ID); ok {
		lines = append(lines, m.indented(lastText(last), "  ", m.t.dim)...)
	}
	return lines
}

func actionText(a workfile.Automation) string {
	if a.Agent != "" {
		return "asks your agent: " + oneLine(a.Agent)
	}
	return "runs " + strings.Join(automation.Quote(a.Run), " ")
}

func lastText(e automation.Event) string {
	text := "last " + e.Outcome + " · " + e.Subject() + " · " + when(e.At)
	if e.Detail != "" {
		text += " · " + e.Detail
	}
	return text
}

// when writes a time briefly: the time today, or the day and time.
func when(t time.Time) string {
	t = t.Local()
	if y, mo, d := time.Now().Date(); t.Year() == y && t.Month() == mo && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("2 Jan 15:04")
}

func (m *automationsModel) start() []string {
	lines := []string{m.title("Get started")}
	lines = append(lines, m.wrap("No automations here yet. An automation says \"when this happens to my work, do that\".", plainText)...)
	lines = append(lines, "")
	if m.w.Policy.Name == "" {
		lines = append(lines, m.wrap("Add name: to policy.yml, such as name: example-web, to keep private automations for this workspace. Then run wf automations init.", plainText)...)
	} else {
		lines = append(lines, m.wrap("Run wf automations init to create a private file that only you can see.", plainText)...)
	}
	lines = append(lines, m.wrap("To share yours with the team, add .workfile/automations/<you>.yml.", plainText)...)
	return append(lines, m.wrap("See docs/automations.md.", m.t.dim)...)
}

// relative writes a path inside the workspace from its root.
func relative(w *workfile.Workspace, path string) string {
	if rel, err := filepath.Rel(automation.Root(w), path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// home writes a path under the home folder with ~.
func home(path string) string {
	if dir, err := os.UserHomeDir(); err == nil && dir != "" && strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "~" + path[len(dir):]
	}
	return path
}

// everyoneModel draws `wf automations --all`: every shared file, by person,
// for sharing ideas. Private files never appear.
type everyoneModel struct {
	screen
	w *workfile.Workspace
}

func newEveryoneModel(v view, w *workfile.Workspace) *everyoneModel {
	return &everyoneModel{screen: newScreen(v), w: w}
}

func (m *everyoneModel) render() string {
	sections := [][]string{{m.t.bold("Shared automations") + m.t.dim(" · "+quantity(len(m.w.Automations), "person", "people"))}}
	if len(m.w.Automations) == 0 {
		sections = append(sections, m.wrap("Nobody has shared automations here yet. Add .workfile/automations/<you>.yml to share yours.", plainText))
	}
	for _, f := range m.w.Automations {
		lines := []string{m.t.bold(m.w.DisplayName(f.Person))}
		lines = append(lines, m.wrap(relative(m.w, f.Path), m.t.dim)...)
		for _, a := range f.Automations {
			lines = append(lines, "")
			lines = append(lines, (&automationsModel{screen: m.screen, state: &automation.State{}}).automation(a)...)
		}
		sections = append(sections, lines)
	}
	return panels(m.t, m.panelWidth(), sections)
}

func trustAutomations(v view, s *automationSetup) (int, error) {
	if s.set.Shared == nil {
		if s.set.SharedProblem != "" {
			return 2, errors.New(s.set.SharedProblem)
		}
		return 2, errors.New("you have no shared automations file in this workspace; private files need no approval")
	}
	m := newAutomationsModel(v, nil, s, &automation.State{})
	lines := []string{m.verdict("Approved", true)}
	if len(s.set.Changed) == 0 {
		lines = append(lines, m.wrap("Nothing changed since you last approved this file.", m.t.dim)...)
	}
	for _, change := range s.set.Changed {
		lines = append(lines, "")
		lines = append(lines, m.changeLines(change)...)
	}
	if err := s.store.SaveTrust(s.trust.Approve(s.set.Shared, s.runner.Root)); err != nil {
		return 2, err
	}
	fmt.Fprint(v.out, panels(m.t, m.panelWidth(), [][]string{lines}))
	return 0, nil
}

func showLog(v view, s *automationSetup, id string) (int, error) {
	state, err := s.store.State()
	if err != nil {
		return 2, err
	}
	last, ok := state.Last(id)
	if !ok || last.Log == "" {
		return 2, fmt.Errorf("%s has no run log yet", id)
	}
	f, err := os.Open(last.Log)
	if err != nil {
		return 2, fmt.Errorf("the log for %s is gone; logs are kept for 14 days", id)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fmt.Fprintln(v.out, clean(scanner.Text()))
	}
	return 0, scanner.Err()
}

func initAutomations(ctx context.Context, v view, w *workfile.Workspace) (int, error) {
	if w.Policy.Name == "" {
		return 2, errors.New("add name: to policy.yml first, such as name: example-web; private files are kept under it")
	}
	path := workfile.PrivateAutomationsPath(w.Policy.Name)
	if _, err := os.Stat(path); err == nil {
		return 2, fmt.Errorf("%s already exists", home(path))
	}
	remotes, err := automation.Remotes(ctx, automation.Root(w))
	if err != nil || len(remotes) == 0 {
		return 2, errors.New("this folder has no git remotes, or is not a git repository; private files need a git repository")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return 2, err
	}
	if err := os.WriteFile(path, []byte(privateTemplate(w, remotes)), 0600); err != nil {
		return 2, err
	}
	s := newScreen(v)
	lines := []string{s.verdict("Created your private automations", true)}
	lines = append(lines, s.wrap(home(path), s.t.bold)...)
	lines = append(lines, "")
	lines = append(lines, s.wrap("Only you can see it. It runs only in "+strings.Join(remotes, ", ")+".", plainText)...)
	lines = append(lines, s.wrap("Edit it, then run wf watch --dry-run to see what would fire.", plainText)...)
	fmt.Fprint(v.out, panels(s.t, s.panelWidth(), [][]string{lines}))
	return 0, nil
}

// privateTemplate is a starting file that runs nothing: one automation in
// observe mode, using a stage from this workspace's own policy.
func privateTemplate(w *workfile.Workspace, remotes []string) string {
	stage := w.Policy.States[len(w.Policy.States)-1]
	id := func(stage string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '-'
		}, stage)
	}
	for _, state := range w.Policy.States[min(1, len(w.Policy.States)-1):] {
		if routes := w.Policy.Routes(state); len(routes) > 0 && !routes[0].Backward {
			stage = routes[0].To
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Your private automations for %s. Only you can see this file.\n", w.Policy.Name)
	b.WriteString("# It runs only in the repositories listed under repos.\n")
	b.WriteString("# The format is in docs/automations.md.\n\nrepos:\n")
	for _, remote := range remotes {
		fmt.Fprintf(&b, "  - %s\n", remote)
	}
	fmt.Fprintf(&b, `
automations:
  # A starting point: report when one of your tickets can move to %[1]s.
  # observe only reports. Change mode to ask or auto when you are ready.
  - id: ready-for-%[2]s
    on:
      ready: %[1]s
    run: [echo, "{ticket} can move to {to}"]
    mode: observe
`, stage, id(stage))
	return b.String()
}
