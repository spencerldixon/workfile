package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	bhelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
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

type projectItem struct{ key, name string }

func (p projectItem) FilterValue() string { return p.key + " " + p.name }

type projectDelegate struct{ t theme }

func (projectDelegate) Height() int                         { return 1 }
func (projectDelegate) Spacing() int                        { return 0 }
func (projectDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d projectDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	p, ok := item.(projectItem)
	if !ok {
		return
	}
	line := runewidth.Truncate(p.key+"  "+p.name, max(m.Width()-2, 4), "…")
	if index == m.Index() {
		fmt.Fprint(w, d.t.accent("› ")+d.t.bold(line))
		return
	}
	fmt.Fprint(w, "  "+line)
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

	input textinput.Model
	spin  spinner.Model
	help  bhelp.Model
	list  list.Model

	// Answers.
	dir, site, email, jiraToken string
	github                      bool
	org, githubToken            string
	project                     string
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

	statusProject string
	haveProjects  bool

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

	l := list.New(nil, projectDelegate{t}, 40, 6)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Styles.Filter = inputStyles
	l.Styles.NoItems = faint
	l.FilterInput.SetStyles(inputStyles)

	return &setupModel{
		ctx: ctx, v: v, t: t, deps: d,
		width:   v.width,
		animate: v.color,
		input:   ti, spin: sp, help: h, list: l,
		dir: d.dir,
		key: fmt.Sprintf("WF_%X", time.Now().UnixNano()),
	}
}

// ---- layout ----------------------------------------------------------------

func (m *setupModel) panelWidth() int { return max(24, min(m.width, 76)) }
func (m *setupModel) inner() int      { return m.panelWidth() - 4 }

func (m *setupModel) resize() {
	m.input.SetWidth(max(8, m.inner()-4))
	m.help.SetWidth(max(10, m.width-2))
	m.list.SetSize(m.inner(), min(8, max(3, len(m.list.Items()))))
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
		m.list, cmd = m.list.Update(msg)
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
		filtering := m.list.FilterState() == list.Filtering
		switch {
		case k == "enter" && !filtering:
			if p, ok := m.list.SelectedItem().(projectItem); ok {
				return m.chooseProject(p.key)
			}
			return nil
		case k == "esc" && m.list.FilterState() == list.Unfiltered:
			return m.goBack()
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return cmd
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
	case stepProjects:
		if m.github {
			return m.focus(stepGitHubToken, false)
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
		m.w.Providers["github"] = workfile.Provider{Kind: "github", Org: m.org, Auth: map[string]string{"token": ref("_GITHUB_TOKEN")}}
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
	m.haveProjects, m.statusProject = false, ""
	if m.step == stepJiraTest {
		m.step = stepAskGitHub
		return nil
	}
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
	items := make([]list.Item, len(msg.projects))
	for i, p := range msg.projects {
		items[i] = projectItem{clean(p.Key), clean(p.Name)}
	}
	m.list.SetItems(items)
	m.list.ResetFilter()
	m.list.Select(0)
	m.haveProjects = true
	m.resize()
	return nil
}

func (m *setupModel) chooseProject(key string) tea.Cmd {
	m.project = key
	if m.statusProject == key && len(m.items) > 0 {
		m.step = stepStatuses
		return nil
	}
	id := m.begin(stepStatuses)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		s, err := client.SetupStatuses(ctx, key)
		return statusesMsg{id, s, err}
	})
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
	m.statuses, m.statusProject, m.cursor = msg.statuses, m.project, 0
	m.items = make([]stageItem, len(msg.statuses))
	for i := range msg.statuses {
		m.items[i] = stageItem{index: i, on: true}
	}
	return nil
}

func (m *setupModel) loadPeople() tea.Cmd {
	id := m.begin(stepPeople)
	ctx, client, project := m.ctx, m.client, m.project
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		p, err := client.SetupPeople(ctx, project)
		return peopleMsg{id, p, err}
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
	setupApply(m.w, m.project, m.statuses, m.chosen(), m.people)
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

// setupApply writes the chosen project, stages and people into the workspace.
func setupApply(w *workfile.Workspace, project string, statuses []string, order []int, people []provider.SetupPerson) {
	p := w.Providers["jira"]
	p.Scope = "project = " + strconv.Quote(project)
	if len(order) < len(statuses) {
		selected := make([]string, len(order))
		for i, n := range order {
			selected[i] = strconv.Quote(statuses[n])
		}
		p.Scope += " AND status in (" + strings.Join(selected, ", ") + ")"
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
