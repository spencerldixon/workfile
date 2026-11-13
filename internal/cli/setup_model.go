package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	bhelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupStep int

const (
	stepWelcome setupStep = iota
	stepFolder
	stepSite
	stepEmail
	stepJiraToken
	stepJiraTest
	stepAskGitHub
	stepOrg
	stepGitHubToken
	stepGitHubTest
	stepRepos
	stepProjects
	stepStatuses
	stepPeople
	stepReview
	stepSaving
	stepDone
)

const (
	jiraTokenPage   = "https://id.atlassian.com/manage-profile/security/api-tokens"
	githubTokenPage = "https://github.com/settings/tokens/new?scopes=repo,read:org&description=workfile"
)

type setupDeps struct {
	dir         string
	background  string
	credentials string
	open        func(string) error
	connect     func(*workfile.Workspace, map[string]string) setupSource
}

type stageItem struct {
	index int // position in the statuses Jira returned
	on    bool
}

// Messages. Network results carry the id of the request that started them, so
// an answer that arrives after the person went back is ignored.
type (
	animTickMsg struct{}
	openedMsg   struct{ err error }
	testedMsg   struct {
		id  int
		err error
	}
	reposMsg struct {
		id    int
		repos []provider.SetupRepo
		err   error
	}
	projectsMsg struct {
		id       int
		projects []provider.SetupProject
		err      error
	}
	statusesMsg struct {
		id       int
		statuses []string
		err      error
	}
	peopleMsg struct {
		id     int
		people []provider.SetupPerson
		err    error
	}
	savedMsg struct {
		id  int
		err error
	}
)

type setupModel struct {
	ctx  context.Context
	v    view
	t    theme
	deps setupDeps

	width   int
	animate bool
	frame   int

	step    setupStep
	loading bool
	loadID  int
	err     string // shown under the current screen
	opened  string // "", "ok" or "fail": did the browser open?

	input       textinput.Model
	spin        spinner.Model
	help        bhelp.Model
	projectPick picker
	repoPick    picker

	// Answers.
	dir, site, email, jiraToken string
	github                      bool
	org, githubToken            string
	projects                    []string
	repos                       []string
	statuses                    []string
	items                       []stageItem
	cursor                      int
	people                      []provider.SetupPerson
	peopleErr                   bool

	// Connection.
	key    string
	w      *workfile.Workspace
	values map[string]string
	client setupSource

	statusFor    string
	haveProjects bool
	haveRepos    bool

	saved bool
	fatal error
}

func newSetupModel(ctx context.Context, v view, d setupDeps) *setupModel {
	t := newTheme(v.color, d.background)
	faint := t.style(func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	bold := t.style(func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) })
	cyan := t.style(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(lipgloss.ANSIColor(6)) })

	// Every style is set here, so the terminal's own 16 colours decide the look
	// and none of the components' default hex colours are used.
	inputStyles := textinput.Styles{
		Focused: textinput.StyleState{Prompt: cyan, Placeholder: faint},
		Blurred: textinput.StyleState{Prompt: faint, Placeholder: faint, Text: faint},
		Cursor:  textinput.CursorStyle{Shape: tea.CursorBar, Blink: true, BlinkSpeed: 530 * time.Millisecond},
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 512
	ti.SetStyles(inputStyles)

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(cyan))

	h := bhelp.New()
	h.Styles = bhelp.Styles{ShortKey: bold, ShortDesc: faint, ShortSeparator: faint, Ellipsis: faint}
	h.ShortSeparator = " · "

	return &setupModel{
		ctx: ctx, v: v, t: t, deps: d,
		width:   v.width,
		animate: v.color,
		input:   ti, spin: sp, help: h,
		projectPick: newPicker(t, inputStyles, faint),
		repoPick:    newPicker(t, inputStyles, faint),
		dir:         d.dir,
		key:         fmt.Sprintf("WF_%X", time.Now().UnixNano()),
	}
}

// ---- layout ----------------------------------------------------------------

func (m *setupModel) panelWidth() int { return max(24, min(m.width, 76)) }
func (m *setupModel) inner() int      { return m.panelWidth() - 4 }

func (m *setupModel) resize() {
	m.input.SetWidth(max(8, m.inner()-4))
	m.help.SetWidth(max(10, m.width-2))
	m.projectPick.size(m.inner())
	m.repoPick.size(m.inner())
}

// ---- tea.Model -------------------------------------------------------------

func (m *setupModel) Init() tea.Cmd {
	m.resize()
	if m.animate {
		return animTick()
	}
	m.frame = animLast
	return nil
}

func animTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(20, msg.Width)
		m.resize()
		return m, nil
	case animTickMsg:
		if m.frame < animLast && (m.step == stepWelcome || m.step == stepDone) {
			m.frame++
			return m, animTick()
		}
		return m, nil
	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case openedMsg:
		m.opened = "ok"
		if msg.err != nil {
			m.opened = "fail"
		}
		return m, nil
	case testedMsg:
		return m, m.tested(msg)
	case reposMsg:
		return m, m.gotRepos(msg)
	case projectsMsg:
		return m, m.gotProjects(msg)
	case statusesMsg:
		return m, m.gotStatuses(msg)
	case peopleMsg:
		return m, m.gotPeople(msg)
	case savedMsg:
		return m, m.gotSaved(msg)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, m.handleKey(msg)
	}
	return m.passThrough(msg)
}

// passThrough sends other messages (cursor blink, pastes) to the focused widget.
func (m *setupModel) passThrough(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.isInput() {
		m.input, cmd = m.input.Update(msg)
	} else if m.step == stepProjects && !m.loading {
		m.projectPick.list, cmd = m.projectPick.list.Update(msg)
	} else if m.step == stepRepos && !m.loading {
		m.repoPick.list, cmd = m.repoPick.list.Update(msg)
	}
	return m, cmd
}

func (m *setupModel) isInput() bool {
	switch m.step {
	case stepFolder, stepSite, stepEmail, stepJiraToken, stepOrg, stepGitHubToken:
		return true
	}
	return false
}

func (m *setupModel) isToken() bool { return m.step == stepJiraToken || m.step == stepGitHubToken }

// ---- keys ------------------------------------------------------------------

func (m *setupModel) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if m.loading {
		if k == "esc" && m.step != stepSaving {
			m.loading = false
			m.loadID++
			return m.goBack()
		}
		return nil
	}
	switch m.step {
	case stepWelcome:
		if m.frame < animLast {
			m.frame = animLast
			return nil
		}
		if k == "enter" {
			return m.focus(stepFolder, false)
		}
	case stepFolder, stepSite, stepEmail, stepJiraToken, stepOrg, stepGitHubToken:
		switch k {
		case "enter":
			return m.submit()
		case "esc":
			return m.goBack()
		case "ctrl+o":
			if m.isToken() {
				return m.openPage()
			}
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.err = ""
		return cmd
	case stepAskGitHub:
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.github = !m.github
		case "y":
			m.github = true
		case "n":
			m.github = false
		case "enter":
			return m.afterChoice()
		case "esc":
			return m.goBack()
		}
	case stepProjects:
		return m.pickKey(&m.projectPick, msg, m.chooseProjects)
	case stepRepos:
		return m.pickKey(&m.repoPick, msg, m.chooseRepos)
	case stepStatuses:
		return m.statusKey(k)
	case stepReview:
		switch k {
		case "enter":
			return m.save()
		case "esc":
			return m.goBack()
		}
	case stepDone:
		switch k {
		case "enter", "q", "esc":
			return tea.Quit
		}
	}
	return nil
}

// pickKey drives a multi-select step. Enter continues only with a pick.
func (m *setupModel) pickKey(p *picker, msg tea.KeyPressMsg, next func() tea.Cmd) tea.Cmd {
	if msg.String() == "esc" && p.list.FilterState() == list.Unfiltered {
		return m.goBack()
	}
	m.err = ""
	cmd, done := p.key(msg)
	if done {
		return next()
	}
	return cmd
}

func (m *setupModel) statusKey(k string) tea.Cmd {
	m.err = ""
	switch k {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.items)-1, m.cursor+1)
	case " ", "space", "x":
		m.items[m.cursor].on = !m.items[m.cursor].on
	case "a":
		all := true
		for _, it := range m.items {
			all = all && it.on
		}
		for i := range m.items {
			m.items[i].on = !all
		}
	case "shift+up", "K":
		if m.cursor > 0 {
			m.items[m.cursor], m.items[m.cursor-1] = m.items[m.cursor-1], m.items[m.cursor]
			m.cursor--
		}
	case "shift+down", "J":
		if m.cursor < len(m.items)-1 {
			m.items[m.cursor], m.items[m.cursor+1] = m.items[m.cursor+1], m.items[m.cursor]
			m.cursor++
		}
	case "enter":
		if len(m.chosen()) == 0 {
			m.err = "Keep at least one status."
			return nil
		}
		return m.loadPeople()
	case "esc":
		return m.goBack()
	}
	return nil
}

// chosen is the indexes of the ticked statuses, in the order shown.
func (m *setupModel) chosen() []int {
	var order []int
	for _, it := range m.items {
		if it.on {
			order = append(order, it.index)
		}
	}
	return order
}

// ---- moving between steps --------------------------------------------------

func (m *setupModel) goBack() tea.Cmd {
	m.err = ""
	switch m.step {
	case stepFolder:
		m.step = stepWelcome
		return nil
	case stepSite:
		return m.focus(stepFolder, false)
	case stepEmail:
		return m.focus(stepSite, false)
	case stepJiraToken, stepJiraTest:
		return m.focus(stepEmail, false)
	case stepAskGitHub:
		return m.focus(stepJiraToken, false)
	case stepOrg:
		m.step = stepAskGitHub
		return nil
	case stepGitHubToken, stepGitHubTest:
		return m.focus(stepOrg, false)
	case stepRepos:
		return m.focus(stepGitHubToken, false)
	case stepProjects:
		if m.github {
			return m.showRepos()
		}
		m.step = stepAskGitHub
		return nil
	case stepStatuses:
		return m.showProjects()
	case stepPeople, stepReview, stepSaving:
		m.step = stepStatuses
		return nil
	}
	return nil
}

func (m *setupModel) focus(step setupStep, open bool) tea.Cmd {
	m.step, m.err, m.loading = step, "", false
	in := &m.input
	in.Reset()
	in.EchoMode = textinput.EchoNormal
	switch step {
	case stepFolder:
		in.SetValue(m.dir)
		in.Placeholder = ""
	case stepSite:
		in.SetValue(m.site)
		in.Placeholder = "example-team.atlassian.net"
	case stepEmail:
		in.SetValue(m.email)
		in.Placeholder = "alice@example.com"
	case stepJiraToken:
		in.EchoMode, in.EchoCharacter = textinput.EchoPassword, '*'
		in.SetValue(m.jiraToken)
		in.Placeholder = "Paste your token"
	case stepOrg:
		in.SetValue(m.org)
		in.Placeholder = "example-team"
	case stepGitHubToken:
		in.EchoMode, in.EchoCharacter = textinput.EchoPassword, '*'
		in.SetValue(m.githubToken)
		in.Placeholder = "Paste your token"
	}
	in.CursorEnd()
	m.resize()
	cmds := []tea.Cmd{in.Focus(), textinput.Blink}
	if open && m.isToken() {
		m.opened = ""
		cmds = append(cmds, m.openPage())
	}
	return tea.Batch(cmds...)
}

func (m *setupModel) openPage() tea.Cmd {
	target := jiraTokenPage
	if m.step == stepGitHubToken {
		target = githubTokenPage
	}
	open := m.deps.open
	return func() tea.Msg { return openedMsg{err: open(target)} }
}

func setupToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00\"'") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", errors.New("paste only the token, on one line, without quotes")
	}
	return value, nil
}

func (m *setupModel) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		m.err = "A value is required."
		if m.isToken() {
			m.err = "Paste your token to continue."
		}
		return nil
	}
	var err error
	switch m.step {
	case stepFolder:
		if m.dir, err = setupDirectory(value); err == nil {
			return m.focus(stepSite, false)
		}
	case stepSite:
		if m.site, err = setupSite(value); err == nil {
			return m.focus(stepEmail, false)
		}
	case stepEmail:
		if m.email, err = setupEmail(value); err == nil {
			return m.focus(stepJiraToken, true)
		}
	case stepJiraToken:
		if m.jiraToken, err = setupToken(value); err == nil {
			return m.test(stepJiraTest)
		}
	case stepOrg:
		if m.org, err = setupOrg(value); err == nil {
			return m.focus(stepGitHubToken, true)
		}
	case stepGitHubToken:
		if m.githubToken, err = setupToken(value); err == nil {
			return m.test(stepGitHubTest)
		}
	}
	if err != nil {
		m.err = capitalise(err.Error()) + "."
	}
	return nil
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m *setupModel) afterChoice() tea.Cmd {
	if m.github {
		return m.focus(stepOrg, false)
	}
	m.githubToken = ""
	m.rebuild() // drops GitHub if it was set up and then declined
	return m.showProjects()
}

// ---- network ---------------------------------------------------------------

// rebuild creates the workspace and client from the answers so far.
func (m *setupModel) rebuild() {
	ref := func(suffix string) string { return "${" + m.key + suffix + "}" }
	m.values = map[string]string{m.key + "_JIRA_EMAIL": m.email, m.key + "_JIRA_TOKEN": m.jiraToken}
	m.w = &workfile.Workspace{
		Policy:    workfile.Policy{Tracker: "jira"},
		Providers: map[string]workfile.Provider{"jira": {Kind: "jira", Site: m.site, Auth: map[string]string{"email": ref("_JIRA_EMAIL"), "token": ref("_JIRA_TOKEN")}}},
		People:    map[string]map[string]string{},
	}
	if m.github && m.githubToken != "" {
		m.values[m.key+"_GITHUB_TOKEN"] = m.githubToken
		m.w.Providers["github"] = workfile.Provider{Kind: "github", Org: m.org, Repos: workfile.Names{"all"}, Auth: map[string]string{"token": ref("_GITHUB_TOKEN")}}
	}
	m.client = m.deps.connect(m.w, m.values)
}

func (m *setupModel) begin(step setupStep) int {
	m.step, m.err, m.loading = step, "", true
	m.loadID++
	return m.loadID
}

func (m *setupModel) test(step setupStep) tea.Cmd {
	m.rebuild()
	id := m.begin(step)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		var err error
		if step == stepJiraTest {
			_, err = client.Identity(ctx, "jira")
		} else {
			_, err = client.Test(ctx, "github")
		}
		return testedMsg{id, err}
	})
}

func (m *setupModel) tested(msg testedMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		text := "Could not connect to " + map[setupStep]string{stepJiraTest: "Jira", stepGitHubTest: "GitHub"}[m.step] + ": " + clean(msg.err.Error())
		back := stepOrg
		if m.step == stepJiraTest {
			back = stepSite
		}
		cmd := m.focus(back, false)
		m.err = text + " Check the details and try again."
		return cmd
	}
	// A new connection means anything discovered before is out of date.
	m.haveProjects, m.haveRepos, m.statusFor = false, false, ""
	if m.step == stepJiraTest {
		m.step = stepAskGitHub
		return nil
	}
	return m.showRepos()
}

func (m *setupModel) showRepos() tea.Cmd {
	if m.haveRepos {
		m.step, m.err = stepRepos, ""
		return nil
	}
	id := m.begin(stepRepos)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		r, err := client.SetupRepos(ctx)
		return reposMsg{id, r, err}
	})
}

func (m *setupModel) gotRepos(msg reposMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		m.fatal = msg.err
		return tea.Quit
	}
	items := []pickItem{{pickAll, "All repositories in " + clean(m.org)}}
	for _, r := range msg.repos {
		items = append(items, pickItem{clean(r.Name), clean(r.Name)})
	}
	m.repoPick.setItems(items)
	m.haveRepos = true
	m.step = stepRepos
	m.resize()
	return nil
}

// chooseRepos keeps the picked repositories, or all of them, and moves on.
func (m *setupModel) chooseRepos() tea.Cmd {
	if !anyPicked(m.repoPick.picked) {
		m.err = "Pick at least one repository."
		return nil
	}
	m.repos = m.repoPick.chosen()
	return m.showProjects()
}

func (m *setupModel) showProjects() tea.Cmd {
	if m.haveProjects {
		m.step, m.err = stepProjects, ""
		return nil
	}
	if m.client == nil {
		m.rebuild()
	}
	id := m.begin(stepProjects)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		p, err := client.SetupProjects(ctx)
		return projectsMsg{id, p, err}
	})
}

func (m *setupModel) gotProjects(msg projectsMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		m.fatal = msg.err
		return tea.Quit
	}
	if len(msg.projects) == 0 {
		m.fatal = errors.New("no visible Jira projects; check token permissions")
		return tea.Quit
	}
	items := make([]pickItem, len(msg.projects))
	for i, p := range msg.projects {
		items[i] = pickItem{clean(p.Key), clean(p.Key) + "  " + clean(p.Name)}
	}
	m.projectPick.setItems(items)
	m.haveProjects = true
	m.step = stepProjects
	m.resize()
	return nil
}

// chooseProjects keeps the picked projects and reads their statuses.
func (m *setupModel) chooseProjects() tea.Cmd {
	if !anyPicked(m.projectPick.picked) {
		m.err = "Pick at least one project."
		return nil
	}
	m.projects = m.projectPick.chosen()
	key := strings.Join(m.projects, ",")
	if m.statusFor == key && len(m.items) > 0 {
		m.step = stepStatuses
		return nil
	}
	id := m.begin(stepStatuses)
	ctx, client, projects := m.ctx, m.client, m.projects
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		var lists [][]string
		for _, project := range projects {
			s, err := client.SetupStatuses(ctx, project)
			if err != nil {
				return statusesMsg{id: id, err: err}
			}
			lists = append(lists, s)
		}
		return statusesMsg{id: id, statuses: mergeStatuses(lists)}
	})
}

// mergeStatuses joins each project's statuses, dropping repeats and keeping the
// order they were first seen in.
func mergeStatuses(lists [][]string) []string {
	var merged []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				merged = append(merged, s)
			}
		}
	}
	return merged
}

func (m *setupModel) gotStatuses(msg statusesMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		m.fatal = msg.err
		return tea.Quit
	}
	m.statuses, m.statusFor, m.cursor = msg.statuses, strings.Join(m.projects, ","), 0
	m.items = make([]stageItem, len(msg.statuses))
	for i := range msg.statuses {
		m.items[i] = stageItem{index: i, on: true}
	}
	return nil
}

func (m *setupModel) loadPeople() tea.Cmd {
	id := m.begin(stepPeople)
	ctx, client, projects := m.ctx, m.client, m.projects
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		var people []provider.SetupPerson
		var failed error
		seen := map[string]bool{}
		for _, project := range projects {
			found, err := client.SetupPeople(ctx, project)
			if err != nil {
				failed = err
				continue
			}
			for _, person := range found {
				if !seen[person.AccountID] {
					seen[person.AccountID] = true
					people = append(people, person)
				}
			}
		}
		return peopleMsg{id, people, failed}
	})
}

func (m *setupModel) gotPeople(msg peopleMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	m.people, m.peopleErr = msg.people, msg.err != nil
	m.step = stepReview
	return nil
}

func (m *setupModel) save() tea.Cmd {
	setupApply(m.w, m.projects, m.statuses, m.chosen(), m.repos, m.people)
	id := m.begin(stepSaving)
	dir, w, credentials, values := m.dir, m.w, m.deps.credentials, m.values
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		return savedMsg{id, saveSetup(dir, w, credentials, values)}
	})
}

func (m *setupModel) gotSaved(msg savedMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		m.step = stepReview
		m.err = "Could not save: " + clean(msg.err.Error())
		return nil
	}
	m.saved, m.step, m.frame = true, stepDone, 0
	if m.animate {
		return animTick()
	}
	m.frame = animLast
	return nil
}

// setupApply writes the chosen stages, repositories and people into the
// workspace. The projects are already set. Every scope setting is written
// explicitly, and scope itself never is. Statuses that were unticked become
// skip_statuses, empty when none were.
func setupApply(w *workfile.Workspace, projects []string, statuses []string, order []int, repos []string, people []provider.SetupPerson) {
	p := w.Providers["jira"]
	p.Projects, p.KeepDoneFor, p.Scope = workfile.Names(projects), "14d", ""
	kept := map[int]bool{}
	for _, n := range order {
		kept[n] = true
	}
	p.SkipStatuses = workfile.Names{}
	for i, name := range statuses {
		if !kept[i] {
			p.SkipStatuses = append(p.SkipStatuses, name)
		}
	}
	if g, ok := w.Providers["github"]; ok {
		g.Repos = workfile.Names(repos)
		if len(repos) == 0 {
			g.Repos = workfile.Names{"all"}
		}
		w.Providers["github"] = g
	}
	w.Policy.States = setupStates(statuses, order)
	w.Policy.Gates = map[string][]workfile.Rule{}
	w.Policy.Transitions = map[string]workfile.Transition{}
	p.Statuses = map[string]workfile.Names{}
	for i, n := range order {
		state := w.Policy.States[i]
		p.Statuses[state] = workfile.Names{statuses[n]}
		if i == len(order)-1 {
			w.Policy.Transitions[state] = workfile.Transition{End: true}
		} else {
			w.Policy.Transitions[state] = workfile.Transition{To: []string{w.Policy.States[i+1]}}
		}
	}
	w.Providers["jira"] = p

	w.People = map[string]map[string]string{}
	names := make([]string, len(people))
	indices := make([]int, len(people))
	for i, person := range people {
		names[i] = clean(person.DisplayName)
		if strings.TrimSpace(names[i]) == "" {
			names[i] = fmt.Sprintf("person_%d", i+1)
		}
		indices[i] = i
	}
	aliases := setupStates(names, indices)
	for i, person := range people {
		w.People[aliases[i]] = map[string]string{"name": clean(person.DisplayName), "jira": person.AccountID}
	}
}
