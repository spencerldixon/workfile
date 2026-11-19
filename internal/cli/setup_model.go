package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	bhelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupStep int

const (
	stepWelcome setupStep = iota
	stepTrackerKind
	stepCodeHostKind
	stepFolder
	stepTrackerSettings
	stepTrackerTest
	stepHostSettings
	stepHostTest
	stepProjects
	stepTrackerRepos
	stepRepos
	stepStatuses
	stepPeople
	stepReview
	stepSaving
	stepDone
)

type setupDeps struct {
	dir         string
	background  string
	credentials string
	open        func(string) error
	connect     func(*workfile.Workspace, map[string]string) setupSource
}

type stageItem struct {
	index int // position in the statuses the tracker returned
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
	nerd    bool

	step         setupStep
	loading      bool
	loadID       int
	err          string // shown under the current screen
	opened       string // "", "ok" or "fail": did the browser open?
	retry        func() tea.Cmd
	preview      viewport.Model
	policyData   string
	reviewPolicy bool

	input                                                textinput.Model
	spin                                                 spinner.Model
	help                                                 bhelp.Model
	providerPick, projectPick, trackerRepoPick, repoPick picker
	trackerOptions, hostOptions                          []setupOption

	dir                                     string
	tracker, host                           setupConnection
	fieldIndex                              int
	projects, trackerRepos, repos, statuses []string
	items                                   []stageItem
	cursor, statusOffset, height            int
	people                                  []provider.SetupPerson
	peopleErr                               bool

	// Connection.
	key    string
	w      *workfile.Workspace
	values map[string]string
	client setupSource

	statusFor                   string
	haveProjects                bool
	haveTrackerRepos, haveRepos bool

	saved bool
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
		height:  24,
		preview: viewport.New(),
		animate: v.color,
		nerd:    v.color && os.Getenv("WORKFILE_ICONS") == "nerd",
		input:   ti, spin: sp, help: h,
		providerPick:    newPicker(t, inputStyles, faint),
		projectPick:     newPicker(t, inputStyles, faint),
		trackerRepoPick: newPicker(t, inputStyles, faint),
		repoPick:        newPicker(t, inputStyles, faint),
		trackerOptions:  setupOptions(true),
		hostOptions:     setupOptions(false),
		dir:             d.dir,
		key:             fmt.Sprintf("WF_%X", time.Now().UnixNano()),
	}
}

// ---- layout ----------------------------------------------------------------

func (m *setupModel) panelWidth() int { return max(20, m.width) }
func (m *setupModel) inner() int      { return m.panelWidth() - 4 }
func (m *setupModel) busy() bool      { return m.loading }

func (m *setupModel) resize() {
	m.input.SetWidth(max(8, m.inner()-4))
	m.help.SetWidth(max(10, m.width-2))
	for _, p := range []*picker{&m.providerPick, &m.projectPick, &m.trackerRepoPick, &m.repoPick} {
		p.nerd = m.nerd
		p.size(m.inner())
	}
	m.providerPick.setSingle(true, false)
	m.projectPick.setSingle(m.singleProject(), true)
	m.scrollStatuses()
	m.preview.SetWidth(m.inner())
	m.preview.SetHeight(max(4, m.height-10))
	m.preview.SetContent(m.t.para(m.policyData, m.inner()))
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
	return tea.Tick(animationFrameTime, func(time.Time) tea.Msg { return animTickMsg{} })
}

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(20, msg.Width), msg.Height
		m.resize()
		return m, nil
	case animTickMsg:
		if m.frame < animLast && m.step == stepWelcome {
			m.frame++
			if m.frame < animLast {
				return m, animTick()
			}
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
	} else if p := m.currentPicker(); p != nil && !m.loading && m.retry == nil {
		p.list, cmd = p.list.Update(msg)
	}
	return m, cmd
}

func (m *setupModel) currentPicker() *picker {
	switch m.step {
	case stepTrackerKind, stepCodeHostKind:
		return &m.providerPick
	case stepProjects:
		return &m.projectPick
	case stepTrackerRepos:
		return &m.trackerRepoPick
	case stepRepos:
		return &m.repoPick
	}
	return nil
}

func (m *setupModel) isInput() bool {
	return m.step == stepFolder || m.step == stepTrackerSettings || m.step == stepHostSettings
}

func (m *setupModel) isToken() bool {
	return m.step != stepFolder && m.isInput() && m.field().Key == "token"
}

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
	if m.retry != nil {
		switch k {
		case "enter":
			return m.retry()
		case "esc":
			m.retry = nil
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
			return m.showOptions(stepTrackerKind)
		}
	case stepFolder, stepTrackerSettings, stepHostSettings:
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
	case stepTrackerKind, stepCodeHostKind:
		return m.pickKey(&m.providerPick, msg, m.chooseProvider)
	case stepProjects:
		return m.pickKey(&m.projectPick, msg, m.chooseProjects)
	case stepTrackerRepos, stepRepos:
		return m.pickKey(m.currentPicker(), msg, m.chooseRepos)
	case stepStatuses:
		return m.statusKey(k)
	case stepReview:
		switch k {
		case "p":
			m.reviewPolicy = !m.reviewPolicy
		case "enter":
			return m.save()
		case "esc":
			if m.reviewPolicy {
				m.reviewPolicy = false
				return nil
			}
			return m.goBack()
		default:
			if m.reviewPolicy {
				var cmd tea.Cmd
				m.preview, cmd = m.preview.Update(msg)
				return cmd
			}
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
	defer m.scrollStatuses()
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
	case stepTrackerKind:
		m.step = stepWelcome
	case stepCodeHostKind:
		return m.showOptions(stepTrackerKind)
	case stepFolder:
		return m.showOptions(stepCodeHostKind)
	case stepTrackerSettings, stepHostSettings:
		if m.fieldIndex > 0 {
			return m.focus(m.step, m.fieldIndex-1)
		}
		if m.step == stepHostSettings {
			return m.lastField(stepTrackerSettings)
		}
		return m.focus(stepFolder, 0)
	case stepTrackerTest:
		return m.lastField(stepTrackerSettings)
	case stepHostTest:
		return m.lastField(stepHostSettings)
	case stepProjects:
		if m.host.kind != "" {
			return m.lastField(stepHostSettings)
		}
		return m.lastField(stepTrackerSettings)
	case stepTrackerRepos:
		return m.showProjects()
	case stepRepos:
		if m.trackerSetting("repos") {
			return m.showRepos(stepTrackerRepos)
		}
		return m.showProjects()
	case stepStatuses:
		if m.host.kind != "" {
			return m.showRepos(stepRepos)
		}
		if m.trackerSetting("repos") {
			return m.showRepos(stepTrackerRepos)
		}
		return m.showProjects()
	case stepPeople, stepReview:
		m.step = stepStatuses
	}
	return nil
}

func (m *setupModel) statusRows() int {
	return max(3, min(len(m.items), m.height-17))
}

func (m *setupModel) scrollStatuses() {
	m.statusOffset = max(0, min(m.statusOffset, m.cursor))
	m.statusOffset = max(m.statusOffset, m.cursor-m.statusRows()+1)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---- network ---------------------------------------------------------------

// rebuild creates the workspace and client from the answers so far.
func (m *setupModel) rebuild(includeHost bool) {
	m.values = map[string]string{}
	tracker := m.tracker.kind
	m.w = &workfile.Workspace{
		Policy:    workfile.Policy{Tracker: tracker},
		Providers: map[string]workfile.Provider{tracker: m.tracker.provider(tracker, m.key, m.values)},
		People:    map[string]map[string]string{},
	}
	if includeHost {
		host := m.hostName()
		p := m.host.provider(host, m.key, m.values)
		p.Repos = workfile.Names{"all"}
		m.w.Providers[host] = p
	}
	m.client = m.deps.connect(m.w, m.values)
}

func (m *setupModel) trackerSetting(name string) bool {
	return m.tracker.role != nil && slices.Contains(m.tracker.role.Settings, name)
}

func (m *setupModel) singleProject() bool { return m.trackerSetting("project") }

func (m *setupModel) begin(step setupStep) int {
	m.step, m.err, m.loading, m.retry = step, "", true, nil
	m.loadID++
	return m.loadID
}

func (m *setupModel) test(step setupStep) tea.Cmd {
	isHost := step == stepHostTest
	m.rebuild(isHost)
	name := m.tracker.kind
	if isHost {
		name = m.hostName()
	}
	id := m.begin(step)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		// Scope discovery verifies access to the selected projects and repositories.
		_, err := client.Identity(ctx, name)
		return testedMsg{id, err}
	})
}

func (m *setupModel) tested(msg testedMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	isTracker := m.step == stepTrackerTest
	if msg.err != nil {
		label := m.connection().label
		step := stepTrackerSettings
		if !isTracker {
			step = stepHostSettings
		}
		cmd := m.lastField(step)
		m.err = "Could not connect to " + label + ". " + clean(msg.err.Error()) + ". Check the details and try again."
		return cmd
	}
	if isTracker && m.host.kind != "" {
		return m.focus(stepHostSettings, 0)
	}
	return m.showProjects()
}

func (m *setupModel) showRepos(step setupStep) tea.Cmd {
	if step == stepRepos && m.haveRepos || step == stepTrackerRepos && m.haveTrackerRepos {
		m.step, m.err = step, ""
		return nil
	}
	name := m.hostName()
	if step == stepTrackerRepos {
		name = m.tracker.kind
	}
	id := m.begin(step)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		r, err := client.SetupRepos(ctx, name)
		return reposMsg{id, r, err}
	})
}

func (m *setupModel) gotRepos(msg reposMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		step := m.step
		m.discoveryError("Could not find repositories", msg.err, func() tea.Cmd { return m.showRepos(step) })
		return nil
	}
	items := []pickItem{{pickAll, "All repositories"}}
	for _, r := range msg.repos {
		items = append(items, pickItem{r.Name, clean(r.Name)})
	}
	m.currentPicker().setItems(items)
	if m.step == stepTrackerRepos {
		m.haveTrackerRepos = true
	} else {
		m.haveRepos = true
	}
	m.resize()
	return nil
}

func (m *setupModel) chooseRepos() tea.Cmd {
	p := m.currentPicker()
	if !anyPicked(p.picked) {
		m.err = "Pick at least one repository."
		return nil
	}
	if m.step == stepTrackerRepos {
		m.trackerRepos = p.chosen()
		if m.host.kind != "" {
			return m.showRepos(stepRepos)
		}
	} else {
		m.repos = p.chosen()
	}
	return m.showStatuses()
}

func (m *setupModel) showProjects() tea.Cmd {
	if m.haveProjects {
		m.step, m.err = stepProjects, ""
		return nil
	}
	id := m.begin(stepProjects)
	ctx, client := m.ctx, m.client
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		p, err := client.SetupProjects(ctx)
		return projectsMsg{id, p, err}
	})
}

func (m *setupModel) discoveryError(summary string, err error, retry func() tea.Cmd) {
	m.err, m.retry = summary+". "+clean(err.Error())+".", retry
}

func (m *setupModel) gotProjects(msg projectsMsg) tea.Cmd {
	if !m.loading || msg.id != m.loadID {
		return nil
	}
	m.loading = false
	if msg.err == nil && len(msg.projects) == 0 {
		msg.err = errors.New("no visible projects; check your account and token permissions")
	}
	if msg.err != nil {
		m.discoveryError("Could not find projects", msg.err, m.showProjects)
		return nil
	}
	items := make([]pickItem, len(msg.projects))
	for i, p := range msg.projects {
		label := clean(p.Key)
		if m.singleProject() {
			label = "#" + label
		}
		items[i] = pickItem{p.Key, label + "  " + clean(p.Name)}
	}
	m.projectPick.setItems(items)
	m.haveProjects = true
	m.resize()
	return nil
}

func (m *setupModel) chooseProjects() tea.Cmd {
	if m.singleProject() {
		item, ok := m.projectPick.list.SelectedItem().(pickItem)
		if !ok {
			m.err = "Choose a project."
			return nil
		}
		m.projects = []string{item.key}
	} else {
		if !anyPicked(m.projectPick.picked) {
			m.err = "Pick at least one project."
			return nil
		}
		m.projects = m.projectPick.chosen()
	}
	if m.trackerSetting("repos") {
		return m.showRepos(stepTrackerRepos)
	}
	if m.host.kind != "" {
		return m.showRepos(stepRepos)
	}
	return m.showStatuses()
}

func (m *setupModel) showStatuses() tea.Cmd {
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
	if msg.err == nil && len(msg.statuses) == 0 {
		msg.err = errors.New("no statuses available for the selected projects")
	}
	if msg.err != nil {
		m.discoveryError("Could not read workflow statuses", msg.err, m.showStatuses)
		return nil
	}
	m.statuses, m.statusFor, m.cursor, m.statusOffset = msg.statuses, strings.Join(m.projects, ","), 0, 0
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
	m.step, m.reviewPolicy = stepReview, false
	if data, err := setupPolicyYAML(m.draft().Policy); err == nil {
		m.policyData = string(data)
		m.preview.SetContent(m.t.para(m.policyData, m.inner()))
		m.preview.GotoTop()
	}
	return nil
}

func (m *setupModel) draft() *workfile.Workspace {
	w := *m.w
	w.Providers = maps.Clone(m.w.Providers)
	setupApply(&w, m.projects, m.statuses, m.chosen(), m.people)
	tracker := w.Policy.Tracker
	if m.trackerSetting("repos") {
		p := w.Providers[tracker]
		p.Repos = setupRepoNames(m.trackerRepos)
		w.Providers[tracker] = p
	}
	if m.host.kind != "" {
		name := m.hostName()
		p := w.Providers[name]
		p.Repos = setupRepoNames(m.repos)
		w.Providers[name] = p
	}
	return &w
}

func (m *setupModel) save() tea.Cmd {
	id := m.begin(stepSaving)
	dir, w, credentials, values := m.dir, m.draft(), m.deps.credentials, m.values
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
		m.err = "Could not apply policy. " + clean(msg.err.Error())
		return nil
	}
	m.saved, m.step = true, stepDone
	return nil
}

func setupRepoNames(repos []string) workfile.Names {
	if len(repos) == 0 {
		return workfile.Names{"all"}
	}
	return workfile.Names(repos)
}

func setupApply(w *workfile.Workspace, projects []string, statuses []string, order []int, people []provider.SetupPerson) {
	name := w.Policy.Tracker
	p := w.Providers[name]
	role := workfile.Kinds[p.Kind].Tracker
	if slices.Contains(role.Settings, "project") {
		p.Project, _ = strconv.Atoi(projects[0])
	} else {
		p.Projects = workfile.Names(projects)
	}
	p.KeepDoneFor = "14d"
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
	w.Providers[name] = p

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
		w.People[aliases[i]] = map[string]string{"name": clean(person.DisplayName), name: person.AccountID}
	}
}
