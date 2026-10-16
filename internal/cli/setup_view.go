package cli

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
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
	out := []string{"", m.header(), ""}
	if m.step == stepWelcome || m.step == stepDone {
		out = append(out, m.art(), "")
	}
	out = append(out, m.t.panel(m.body(), m.panelWidth()))
	if h := m.footer(); h != "" {
		out = append(out, "", " "+h)
	}
	screen := strings.Join(out, "\n") + "\n"
	if !m.t.color {
		screen = ansi.Strip(screen)
	}
	return screen
}

// art is the animated picture, or just the name on a very narrow terminal.
func (m *setupModel) art() string {
	if m.width < 24 {
		return m.t.wordmark()
	}
	return m.t.animFrame(m.frame)
}

func (m *setupModel) header() string {
	title := " " + m.t.bold("workfile") + m.t.dim(" setup")
	if m.step == stepWelcome || m.step == stepDone {
		return title
	}
	names := []string{"Connect", "Discover", "Save"}
	current := 0
	switch {
	case m.step >= stepReview:
		current = 2
	case m.step >= stepProjects:
		current = 1
	}
	if m.width < 52 {
		return title + m.t.dim(fmt.Sprintf("  ·  %d of 3 %s", current+1, names[current]))
	}
	parts := make([]string, len(names))
	for i, name := range names {
		switch {
		case i < current:
			parts[i] = m.t.good("✓") + " " + name
		case i == current:
			parts[i] = m.t.accent("●") + " " + m.t.bold(name)
		default:
			parts[i] = m.t.dim("○ " + name)
		}
	}
	return title + "   " + strings.Join(parts, m.t.dim("  ›  "))
}

// screen lays out a title, a short explanation, and then the parts.
func (m *setupModel) screen(title, text string, parts ...string) string {
	lines := []string{m.t.bold(title)}
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

// steps is a numbered list whose wrapped lines hang under the text.
func (m *setupModel) steps(items ...string) string {
	rows := make([]string, len(items))
	for i, item := range items {
		rows[i] = lipgloss.JoinHorizontal(lipgloss.Top, m.t.dim(fmt.Sprintf("%d. ", i+1)), m.t.para(item, m.inner()-3))
	}
	return strings.Join(rows, "\n")
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
		count = m.t.good("✓ ") + fmt.Sprintf("%d characters pasted", n)
	}
	return strings.Join(nonEmpty(m.input.View(), count, m.errView()), "\n\n")
}

func (m *setupModel) pageLink(label, target string) string {
	if m.v.color {
		return m.v.link(label+" ↗", target)
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
		return m.spin.View() + " " + m.loadingText()
	}
	in := m.inner()
	switch m.step {
	case stepWelcome:
		return m.screen("Welcome to workfile",
			"Let's connect your tickets and pull requests, then check them against a written workflow. Workfile only reads. It never changes a ticket, a label or a pull request.",
			m.t.dim("It takes about two minutes."))
	case stepFolder:
		return m.screen("Where should your workfile live?",
			"Workfile keeps its settings in a .workfile folder. Choose the folder that will hold it, usually your project root.",
			m.inputView())
	case stepSite:
		return m.screen("Your Jira site",
			"The address you use to open Jira, such as example-team.atlassian.net.",
			m.inputView())
	case stepEmail:
		return m.screen("Your Jira email", "The email address of your Atlassian account.", m.inputView())
	case stepJiraToken:
		return m.screen("Create a Jira API token",
			"",
			m.steps("Open the page below and choose Create API token.", "Name it workfile, then copy the token.", "Paste it here."),
			strings.Join(nonEmpty(m.pageLink("Open Atlassian API tokens", jiraTokenPage), m.openedNote()), "\n"),
			m.t.dim(m.t.para("Workfile only reads. If Jira offers scopes, choose read:jira-work and read:jira-user.", in)),
			m.tokenView())
	case stepAskGitHub:
		return m.screen("Connect GitHub too?",
			"Workfile can also read pull requests, so each ticket shows its reviews and checks. It is optional, and you can add it later.",
			m.choiceView())
	case stepOrg:
		return m.screen("Your GitHub organisation",
			"The name in your GitHub address, like example-team in github.com/example-team.",
			m.inputView())
	case stepGitHubToken:
		return m.screen("Create a GitHub token",
			"",
			m.steps("Open the page below. It selects the two permissions Workfile needs: repo and read:org.", "Choose Generate token, then copy it.", "Paste it here."),
			strings.Join(nonEmpty(m.pageLink("Open GitHub token page", githubTokenPage), m.openedNote()), "\n"),
			m.t.dim(m.t.para("Workfile only reads, but GitHub's classic tokens have no read-only choice for repositories, so repo is the smallest one. If your organisation uses SSO, choose Configure SSO next to the token and authorise it.", in)),
			m.tokenView())
	case stepProjects:
		return m.screen("Which Jira project?",
			"Workfile checks the tickets in one project. Type to filter the list.",
			m.list.View())
	case stepStatuses:
		return m.statusesView()
	case stepReview:
		return m.reviewView()
	case stepDone:
		return m.doneView()
	}
	return ""
}

func (m *setupModel) loadingText() string {
	switch m.step {
	case stepJiraTest:
		return "Checking your Jira connection…"
	case stepGitHubTest:
		return "Checking your GitHub connection…"
	case stepProjects:
		return "Finding your Jira projects…"
	case stepStatuses:
		return "Reading the statuses in " + clean(m.project) + "…"
	case stepPeople:
		return "Finding people in " + clean(m.project) + "…"
	case stepSaving:
		return "Saving your workfile…"
	}
	return "Working…"
}

func (m *setupModel) choiceView() string {
	yes, no := m.t.dim("○ Yes"), m.t.accent("● No")
	if m.github {
		yes, no = m.t.accent("● Yes"), m.t.dim("○ No")
	}
	return yes + "    " + no
}

func (m *setupModel) statusesView() string {
	rows := make([]string, len(m.items))
	names := []string{}
	for i, it := range m.items {
		name := runewidth.Truncate(clean(m.statuses[it.index]), max(8, m.inner()-6), "…")
		cursor, box := "  ", m.t.dim("[ ]")
		if it.on {
			box = m.t.good("[✓]")
			names = append(names, clean(m.statuses[it.index]))
		} else {
			name = m.t.dim(name)
		}
		if i == m.cursor {
			cursor = m.t.accent("› ")
			if it.on {
				name = m.t.bold(name)
			}
		}
		rows[i] = cursor + box + " " + name
	}
	pipeline := m.t.dim("Pipeline  ") + strings.Join(names, m.t.dim(" → "))
	if len(names) == 0 {
		pipeline = m.t.dim("Pipeline  none yet")
	}
	return m.screen("Which statuses should Workfile track?",
		"These are the statuses your project uses. Keep the ones tickets move through. Untick any your team rarely uses, and those tickets are left out of checks.",
		strings.Join(rows, "\n"),
		m.t.para(pipeline, m.inner()),
		m.errView())
}

func (m *setupModel) reviewView() string {
	labelW := 9
	row := func(label, value string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.t.dim(runewidth.FillRight(label, labelW)), m.t.para(value, m.inner()-labelW))
	}
	github := "Not connected"
	if m.github {
		github = clean(m.org)
	}
	stages := make([]string, 0, len(m.items))
	for _, i := range m.chosen() {
		stages = append(stages, clean(m.statuses[i]))
	}
	people := fmt.Sprintf("%d imported", len(m.people))
	if m.peopleErr {
		people = "None imported"
	}
	rows := strings.Join([]string{
		row("Folder", clean(m.dir)+"/.workfile"),
		row("Jira", clean(m.site)),
		row("GitHub", github),
		row("Project", clean(m.project)),
		row("Stages", strings.Join(stages, " → ")),
		row("People", people),
	}, "\n")
	parts := []string{rows}
	if m.peopleErr {
		parts = append(parts, m.t.para("! Could not import people. Add them later in people.yml.", m.inner()))
	}
	parts = append(parts,
		m.t.dim(m.t.para("Each stage links to the next, and no checks are added yet. Add your team's rules in .workfile/policy.yml. Tokens are saved privately in "+clean(m.deps.credentials)+".", m.inner())),
		m.errView())
	return m.screen("Ready to save", "", parts...)
}

func (m *setupModel) doneView() string {
	return m.screen(m.t.good("✓")+" Your workfile is ready", "Next, open your project folder and run:",
		m.t.bold(m.t.para("cd "+strconv.Quote(m.dir), m.inner()))+"\n"+m.t.bold("wf status"),
		m.t.dim(m.t.para("This starter has no checks yet, so everything reads as ready. Add your team's rules in .workfile/policy.yml. Guide: docs/getting-started.md", m.inner())))
}

func (m *setupModel) footer() string {
	hint := func(k, d string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, d)) }
	var b []key.Binding
	switch {
	case m.step == stepDone:
		b = []key.Binding{hint("enter", "close")}
	case m.step == stepSaving:
		return ""
	case m.loading:
		b = []key.Binding{hint("esc", "back"), hint("ctrl+c", "quit")}
	case m.step == stepWelcome && m.frame < animLast:
		b = []key.Binding{hint("any key", "skip")}
	case m.step == stepWelcome:
		b = []key.Binding{hint("enter", "begin"), hint("ctrl+c", "quit")}
	case m.isToken():
		b = []key.Binding{hint("enter", "continue"), hint("ctrl+o", "open page"), hint("esc", "back")}
	case m.isInput():
		b = []key.Binding{hint("enter", "continue"), hint("esc", "back"), hint("ctrl+c", "quit")}
	case m.step == stepAskGitHub:
		b = []key.Binding{hint("←/→", "choose"), hint("enter", "confirm"), hint("esc", "back")}
	case m.step == stepProjects && m.list.FilterState() == list.Filtering:
		b = []key.Binding{hint("enter", "apply filter"), hint("esc", "clear")}
	case m.step == stepProjects:
		b = []key.Binding{hint("↑/↓", "move"), hint("/", "filter"), hint("enter", "select"), hint("esc", "back")}
	case m.step == stepStatuses:
		b = []key.Binding{hint("space", "tick"), hint("shift+↑/↓", "reorder"), hint("enter", "continue"), hint("esc", "back")}
	case m.step == stepReview:
		b = []key.Binding{hint("enter", "save"), hint("esc", "back")}
	}
	return m.help.ShortHelpView(b)
}
