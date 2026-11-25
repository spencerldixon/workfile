package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"workfile/internal/workfile"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A project is an origin supplied by the tracker, not a provider-specific query.
// Discover choices before applying the temporary project filter, so All always
// returns to the configured scope rather than broadening it.
type trackerProject struct {
	origin, label string
	count         int
}

func (p trackerProject) FilterValue() string { return p.label }

type inboxScopeChoice struct {
	scope workfile.InboxScope
	label string
}

func (choice inboxScopeChoice) FilterValue() string { return choice.label }

type trackerScopeRow struct{ t theme }

func (trackerScopeRow) Height() int                         { return 1 }
func (trackerScopeRow) Spacing() int                        { return 0 }
func (trackerScopeRow) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d trackerScopeRow) Render(w io.Writer, l list.Model, index int, item list.Item) {
	label := clean(item.FilterValue())
	count := ""
	if project, ok := item.(trackerProject); ok {
		count = fmt.Sprint(project.count)
	}
	if index == l.Index() {
		line := alignPair("▌ "+label, count+" ", l.Width())
		fmt.Fprint(w, d.t.render(line, func(st lipgloss.Style) lipgloss.Style {
			return st.Reverse(true).Bold(true)
		}))
		return
	}
	fmt.Fprint(w, alignPair("  "+label, d.t.dim(count)+" ", l.Width()))
}

func (m *trackerModel) projects() []trackerProject {
	tickets := arrangeTickets(m.all, m.e.w.Policy, m.shownView(), m.me)
	projects := []trackerProject{{label: "All projects", count: len(tickets)}}
	for _, ticket := range tickets {
		if ticket.Origin == "" {
			continue
		}
		at := slices.IndexFunc(projects, func(p trackerProject) bool { return strings.EqualFold(p.origin, ticket.Origin) })
		if at < 0 {
			projects = append(projects, trackerProject{origin: ticket.Origin, label: ticket.Origin, count: 1})
		} else {
			projects[at].count++
		}
	}
	return projects
}

func (m *trackerModel) openProjectPicker() tea.Cmd {
	if !m.loaded {
		m.notice = "Projects are still loading."
		return nil
	}
	choices := m.projects()
	items := make([]list.Item, len(choices))
	at := 0
	for i, p := range choices {
		items[i] = p
		if strings.EqualFold(p.origin, m.projectOrigins[m.shownView()]) {
			at = i
		}
	}
	m.projectPicker = list.New(items, trackerScopeRow{m.t}, 0, 0)
	configureTrackerPicker(&m.projectPicker)
	m.projectPicker.Select(at)
	m.projectGeneration++
	m.pickingProject, m.pickingPR = true, false
	m.resizeProjectPicker()
	return nil
}

func (m *trackerModel) pickingPicker() bool {
	return m.pickingProject || m.pickingPR || m.pickingInboxScope
}

func (m *trackerModel) openInboxScope() {
	items := []list.Item{
		inboxScopeChoice{scope: workfile.InboxScopeAll, label: "All signals"},
		inboxScopeChoice{scope: workfile.InboxScopeAttention, label: "Needs attention"},
	}
	m.projectPicker = list.New(items, trackerScopeRow{m.t}, 0, 0)
	configureTrackerPicker(&m.projectPicker)
	m.projectPicker.KeyMap.Filter.SetEnabled(false)
	if m.inbox.scope == workfile.InboxScopeAttention {
		m.projectPicker.Select(1)
	}
	m.projectGeneration++
	m.pickingInboxScope = true
	m.resizeProjectPicker()
}

func (m *trackerModel) projectKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "esc" || msg.String() == "q" && !m.projectPicker.SettingFilter() {
		m.pickingProject, m.pickingPR, m.pickingInboxScope = false, false, false
		return nil
	}
	if msg.String() == "enter" {
		switch p := m.projectPicker.SelectedItem().(type) {
		case inboxScopeChoice:
			m.pickingInboxScope = false
			m.inbox.scope = p.scope
			m.inbox.rebuild()
			return nil
		case trackerProject:
			m.projectOrigins[m.shownView()] = p.origin
			m.pickingProject = false
			return m.showTickets()
		case trackerPR:
			return m.choosePR(p)
		}
		return nil
	}
	return m.updateProjectPicker(msg)
}

type trackerProjectMsg struct {
	generation int
	msg        tea.Msg
}

// Keep asynchronous picker search results separate from the ticket search and
// ignore results from a picker that was closed or reopened while filtering.
func projectCommand(generation int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for i, child := range batch {
				batch[i] = projectCommand(generation, child)
			}
			return batch
		}
		return trackerProjectMsg{generation, msg}
	}
}

func (m *trackerModel) updateProjectPicker(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.projectPicker, cmd = m.projectPicker.Update(msg)
	return projectCommand(m.projectGeneration, cmd)
}

func (m *trackerModel) projectModalSize() (width, height int) {
	width = min(64, max(1, m.width-8))
	rows := len(m.projectPicker.Items())
	if m.pickingPR {
		rows *= 3
	}
	height = min(max(8, rows+7), min(18, max(1, m.height-2)))
	return width, height
}

func (m *trackerModel) resizeProjectPicker() {
	width, height := m.projectModalSize()
	m.projectPicker.SetSize(max(1, width-4), max(1, height-7))
}

func (m *trackerModel) renderProjectPicker(screen string) string {
	width, height := m.projectModalSize()
	inner := max(1, width-4)
	title, empty := "Choose a project", "No matching projects."
	caption := m.t.dim(alignPair("  Project", "Tickets ", inner))
	if m.pickingPR {
		title, empty = "Choose a pull request", "No matching PRs."
		caption = m.t.dim(m.prPickerCaption())
	}
	if m.pickingInboxScope {
		title, caption = "Choose Inbox scope", ""
	}
	if m.projectPicker.SettingFilter() || m.projectPicker.FilterValue() != "" {
		caption = m.t.dim("/ ") + clean(m.projectPicker.FilterValue())
		if m.projectPicker.SettingFilter() {
			caption += "█"
		}
	}
	lines := []string{"", "  " + m.t.bold(title), "  " + ansi.Truncate(caption, inner, "…"), ""}
	if len(m.projectPicker.VisibleItems()) == 0 {
		lines = append(lines, "  "+m.t.dim(empty))
	} else {
		for _, line := range strings.Split(m.projectPicker.View(), "\n") {
			lines = append(lines, "  "+ansi.Truncate(line, inner, "…"))
		}
	}
	lines = fitTrackerRows(lines, height-3)
	move, choose, cancel := m.keys.Move, m.keys.Read, m.keys.Clear
	move.SetHelp("↑↓", "move")
	choose.SetHelp("enter", "select")
	if m.pickingPR {
		choose.SetHelp("enter", "open")
	}
	cancel.SetHelp("esc", "cancel")
	modalHelp := m.help
	modalHelp.SetWidth(inner)
	bindings := []key.Binding{choose, cancel}
	if !m.pickingInboxScope {
		bindings = append(bindings, m.keys.Search)
	}
	help := modalHelp.ShortHelpView(append(bindings, move))
	lines = append(lines, "", "  "+ansi.Truncate(help, inner, ""), "")
	for i, line := range lines {
		lines[i] = m.panelTheme.onPanel(ansi.Truncate(line, width, "…"), width)
	}
	modal := lipgloss.NewLayer(strings.Join(lines, "\n")).
		X(max(0, (m.width-width)/2)).Y(max(0, (m.height-height)/2)).Z(1)
	return lipgloss.NewCompositor(lipgloss.NewLayer(screen), modal).Render()
}
