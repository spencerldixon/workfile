package cli

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func (m *setupModel) View() tea.View {
	return tea.NewView(m.render())
}

// render is the screen as a string. Plain output has every escape removed, so
// whatever a component draws by default cannot leak colour.
func (m *setupModel) render() string {
	var out []string
	switch m.step {
	case stepWelcome:
		out = append(out, m.t.animFrame(m.frame, m.panelWidth()), "")
	case stepDone:
		// The result stands on its own when the wizard ends.
	default:
		out = append(out, m.header(), "")
	}
	out = append(out, m.t.panel(m.body(), m.panelWidth()))
	if h := m.footer(); h != "" {
		out = append(out, "", " "+m.t.para(h, m.panelWidth()-1))
	}
	screen := strings.Join(out, "\n") + "\n"
	if !m.t.color {
		screen = ansi.Strip(screen)
	}
	return screen
}

func setupIcon(nerd bool, fallback, glyph string) string {
	if nerd {
		return glyph
	}
	return fallback
}

func (m *setupModel) icon(fallback, glyph string) string {
	return setupIcon(m.nerd, fallback, glyph)
}

func (m *setupModel) header() string {
	names := []string{"Providers", "Connections", "Scope", "Policy"}
	current := 0
	switch {
	case m.step >= stepStatuses:
		current = 3
	case m.step >= stepProjects:
		current = 2
	case m.step >= stepTrackerSettings:
		current = 1
	}
	gap := "   "
	if m.width < 60 {
		names[1], gap = "Connect", " "
	}
	if m.width < 38 {
		return m.t.accent(names[current]) + m.t.dim(fmt.Sprintf("  %d/%d", current+1, len(names)))
	}
	icons := []string{m.icon("+", "\uf1e6"), m.icon("↔", "\uf0c1"), m.icon("•", "\uf07c"), m.icon("≡", "\uf15c")}
	parts := make([]string, len(names))
	for i, name := range names {
		label := icons[i] + " " + name
		switch {
		case i < current:
			parts[i] = m.t.good(m.icon("✓", "\uf00c") + " " + name)
		case i == current:
			if !m.t.color {
				label = "› " + name
			}
			parts[i] = m.t.accent(label)
		default:
			parts[i] = m.t.dim(label)
		}
	}
	return strings.Join(parts, gap)
}

// screen lays out a title, a short explanation, and then the parts.
func (m *setupModel) screen(title, text string, parts ...string) string {
	lines := []string{m.t.bold(m.t.para(title, m.inner()))}
	if text != "" {
		lines = append(lines, "", m.t.para(text, m.inner()))
	}
	for _, p := range parts {
		if p != "" {
			lines = append(lines, "", p)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *setupModel) errView() string {
	if m.err == "" {
		return ""
	}
	return m.t.bad(m.t.para("✗ "+m.err, m.inner()))
}

func (m *setupModel) inputView() string {
	return strings.Join(nonEmpty(m.input.View(), m.errView()), "\n\n")
}

func nonEmpty(s ...string) []string {
	var r []string
	for _, v := range s {
		if v != "" {
			r = append(r, v)
		}
	}
	return r
}

// tokenView is the masked input plus a count, so a paste can be seen to land.
func (m *setupModel) tokenView() string {
	count := m.t.dim("Nothing pasted yet")
	if n := len([]rune(m.input.Value())); n > 0 {
		count = m.t.good(m.icon("✓", "\uf00c")+" ") + fmt.Sprintf("%d characters pasted", n)
	}
	return strings.Join(nonEmpty(m.input.View(), count, m.errView()), "\n\n")
}

func (m *setupModel) pageLink(label, target string) string {
	if m.v.color {
		var lines []string
		for _, line := range strings.Split(m.t.para(label+" ↗", m.inner()), "\n") {
			lines = append(lines, m.v.link(line, target))
		}
		return strings.Join(lines, "\n")
	}
	return m.t.para(target, m.inner())
}

func (m *setupModel) openedNote() string {
	switch m.opened {
	case "ok":
		return m.t.dim(m.t.para("Opened in your browser. Press ctrl+o to open it again.", m.inner()))
	case "fail":
		return m.t.dim(m.t.para("Could not open a browser. Open the link above yourself.", m.inner()))
	}
	return ""
}

func (m *setupModel) body() string {
	if m.loading {
		icon := m.spin.View() + " "
		return lipgloss.JoinHorizontal(lipgloss.Top, icon, m.t.para(m.loadingText(), m.inner()-lipgloss.Width(icon)))
	}
	if m.retry != nil {
		return m.screen("Try again", "", m.errView())
	}
	switch m.step {
	case stepWelcome:
		return m.screen("Set up your workflow",
			"Choose where you track work and review code. Then preview and apply a starter policy for this workspace.")
	case stepTrackerKind:
		return m.screen("Where does your team track work?",
			"Choose the tracker Workfile reads for tickets and their statuses.", m.providerPick.list.View(), m.errView())
	case stepCodeHostKind:
		return m.screen("Where does your team review code?",
			"Choose a code host to include pull requests, reviews and checks.", m.providerPick.list.View(), m.errView())
	case stepFolder:
		return m.screen("Where should .workfile/ go?",
			"Use your project root to share the workflow with your team.", m.inputView())
	case stepTrackerSettings, stepHostSettings:
		return m.connectionView()
	case stepProjects:
		if m.singleProject() {
			return m.screen("Choose a project", "Workfile reads work and statuses from this project.",
				m.projectPick.list.View(), m.errView())
		}
		return m.pickView("Which projects does your team work in?",
			"Workfile checks tickets in the projects you pick.", &m.projectPick, "project", "projects")
	case stepTrackerRepos:
		return m.pickView("Choose repositories for tickets",
			"Select the repositories Workfile should read for the tracker.", &m.trackerRepoPick, "repository", "repositories")
	case stepRepos:
		return m.pickView("Choose repositories for pull requests",
			"Select the repositories Workfile should read for the code host.", &m.repoPick, "repository", "repositories")
	case stepStatuses:
		return m.statusesView()
	case stepReview:
		return m.reviewView()
	case stepDone:
		return m.doneView()
	}
	return ""
}

func (m *setupModel) connectionView() string {
	field := m.field()
	if !m.isToken() {
		return m.screen(field.Title, field.Hint, m.inputView())
	}
	form := m.connection().form
	return m.screen(field.Title, form.TokenHint,
		strings.Join(nonEmpty(m.pageLink(form.TokenLink, form.TokenPage), m.openedNote()), "\n"),
		m.tokenView())
}

func (m *setupModel) pickView(title, text string, p *picker, one, many string) string {
	count := m.t.dim("Nothing picked yet")
	switch n := len(p.chosen()); {
	case p.picked[pickAll]:
		count = m.t.good(m.icon("✓", "\uf00c")+" ") + "All picked"
	case n == 1:
		count = m.t.good(m.icon("✓", "\uf00c")+" ") + "1 " + one + " picked"
	case n > 1:
		count = m.t.good(m.icon("✓", "\uf00c")+" ") + fmt.Sprintf("%d %s picked", n, many)
	}
	return m.screen(title, text, p.list.View(), count, m.errView())
}

func (m *setupModel) loadingText() string {
	switch m.step {
	case stepTrackerTest, stepHostTest:
		return "Connecting to " + m.connection().label + "…"
	case stepTrackerRepos, stepRepos:
		return "Finding repositories…"
	case stepProjects:
		return "Finding projects…"
	case stepStatuses:
		return "Reading workflow statuses…"
	case stepPeople:
		return "Finding people…"
	case stepSaving:
		return "Applying the policy…"
	}
	return "Working…"
}

func (m *setupModel) statusesView() string {
	end := min(m.statusOffset+m.statusRows(), len(m.items))
	rows := make([]string, 0, m.statusRows()+1)
	for i := m.statusOffset; i < end; i++ {
		it := m.items[i]
		name := runewidth.Truncate(clean(m.statuses[it.index]), m.inner()-4, "…")
		cursor, mark := "  ", m.t.dim(m.icon("○", "\uf10c"))
		if it.on {
			mark = m.t.good(m.icon("✓", "\uf00c"))
		} else {
			name = m.t.dim(name)
		}
		if i == m.cursor {
			cursor = m.t.accent(m.icon("›", "\uf054") + " ")
			name = m.t.accent(name)
		}
		rows = append(rows, cursor+mark+" "+name)
	}
	if end < len(m.items) || m.statusOffset > 0 {
		rows = append(rows, m.t.dim(fmt.Sprintf("Showing %d–%d of %d", m.statusOffset+1, end, len(m.items))))
	}
	var names []string
	for _, i := range m.chosen() {
		names = append(names, clean(m.statuses[i]))
	}
	pipeline := "None selected"
	if len(names) > 0 {
		pipeline = strings.Join(names, " → ")
	}
	return m.screen("Put the workflow stages in order",
		"Keep the statuses tickets move through. The last stage marks completed work.",
		strings.Join(rows, "\n"), m.t.para("Workflow  "+pipeline, m.inner()), m.errView())
}

func (m *setupModel) reviewView() string {
	if m.reviewPolicy {
		return m.screen("policy.yml", "", m.preview.View(), m.errView())
	}
	row := func(label, value string) string {
		const labelWidth = 11
		if m.inner()-labelWidth < 10 {
			return m.t.dim(label) + "\n" + m.t.para(value, m.inner())
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, m.t.dim(runewidth.FillRight(label, labelWidth)),
			m.t.para(value, m.inner()-labelWidth))
	}
	repos := func(names []string) string {
		if len(names) == 0 {
			return "all repositories"
		}
		return clean(strings.Join(names, ", "))
	}
	tracker := clean(strings.Join(m.projects, ", "))
	if m.singleProject() {
		tracker = "Project #" + tracker + " · " + repos(m.trackerRepos)
	}
	host := "Skipped"
	if m.host.kind != "" {
		host = m.host.label + " · " + repos(m.repos)
	}
	var stages []string
	for _, i := range m.chosen() {
		stages = append(stages, clean(m.statuses[i]))
	}
	folder := m.t.para("Write to "+clean(m.dir)+"/.workfile/", m.inner())
	rows := strings.Join([]string{
		row("Tracker", m.tracker.label+" · "+tracker),
		row("Code host", host),
		row("Workflow", strings.Join(stages, " → ")),
	}, "\n")
	note := ""
	if m.peopleErr {
		note = m.t.warn(m.t.para("! Could not import people. You can add them in people.yml.", m.inner()))
	} else if len(m.people) > 0 {
		note = m.t.dim(fmt.Sprintf("%d people imported", len(m.people)))
	}
	return m.screen("Review and apply policy", folder, rows, note,
		m.t.dim(m.t.para("Your starter policy links each stage to the next. Add checks in policy.yml. Tokens go in your private credentials file.", m.inner())),
		m.errView())
}

func (m *setupModel) doneView() string {
	return m.screen(m.t.good(m.icon("✓", "\uf00c"))+" Policy applied", "See the tickets in your new workflow.",
		m.t.bold(m.t.para("cd "+strconv.Quote(m.dir), m.inner()))+"\n"+m.t.bold("wf status"),
		m.t.dim(m.t.para("Add your team’s checks in .workfile/policy.yml before using the policy to decide what is ready.", m.inner())))
}

func (m *setupModel) footer() string {
	hint := func(k, d string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, d)) }
	var b []key.Binding
	switch {
	case m.step == stepDone:
		b = []key.Binding{hint("enter", "close")}
	case m.step == stepSaving:
		return ""
	case m.retry != nil:
		b = []key.Binding{hint("enter", "retry"), hint("esc", "back")}
	case m.loading:
		b = []key.Binding{hint("esc", "back"), hint("ctrl+c", "quit")}
	case m.step == stepWelcome && m.frame < animLast:
		b = []key.Binding{hint("any key", "skip")}
	case m.step == stepWelcome:
		b = []key.Binding{hint("enter", "begin"), hint("ctrl+c", "quit")}
	case m.isToken():
		b = []key.Binding{hint("enter", "test"), hint("ctrl+o", "open page"), hint("esc", "back")}
	case m.isInput():
		b = []key.Binding{hint("enter", "continue"), hint("esc", "back"), hint("ctrl+c", "quit")}
	case m.step == stepTrackerKind || m.step == stepCodeHostKind:
		b = []key.Binding{hint("↑/↓", "choose"), hint("enter", "confirm"), hint("esc", "back")}
	case m.currentPicker() != nil:
		p := m.currentPicker()
		switch {
		case p.filtering():
			b = []key.Binding{hint("enter", "apply filter"), hint("esc", "clear")}
		case p.single:
			return m.help.ShortHelpView([]key.Binding{hint("↑/↓", "choose"), hint("/", "filter")}) +
				"\n " + m.help.ShortHelpView([]key.Binding{hint("enter", "select"), hint("esc", "back")})
		default:
			return m.help.ShortHelpView([]key.Binding{hint("↑/↓", "move"), hint("space", "tick"), hint("/", "filter")}) +
				"\n " + m.help.ShortHelpView([]key.Binding{hint("enter", "continue"), hint("esc", "back")})
		}
	case m.step == stepStatuses:
		return m.help.ShortHelpView([]key.Binding{hint("space", "tick"), hint("shift+↑/↓", "reorder")}) +
			"\n " + m.help.ShortHelpView([]key.Binding{hint("enter", "continue"), hint("esc", "back")})
	case m.step == stepReview:
		if m.reviewPolicy {
			b = []key.Binding{hint("↑/↓", "scroll"), hint("p", "summary"), hint("enter", "apply")}
		} else {
			b = []key.Binding{hint("enter", "apply"), hint("p", "policy.yml"), hint("esc", "back")}
		}
	}
	return m.help.ShortHelpView(b)
}
