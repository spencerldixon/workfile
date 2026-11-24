package cli

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// ticketModel is the screen for `wf status TICKET`. It answers two questions:
// where is this ticket, and what needs attention before it can move on.
//
// It is a Bubble Tea model so it follows the rest of the UI, but it is drawn
// once and printed. Nothing here reads the keyboard, so it never takes over the
// terminal and the result stays in scrollback and in pipes.
type ticketModel struct {
	v    view
	t    theme
	w    *workfile.Workspace
	a    workfile.Assessment
	flow *picture
	// to limits the fixes to one destination, for `wf check --to`.
	to string
}

func newTicketModel(v view, w *workfile.Workspace, a workfile.Assessment) *ticketModel {
	return &ticketModel{v: v, t: newTheme(v.color, v.laneBackground()), w: w, a: a}
}

func (m *ticketModel) Init() tea.Cmd                       { return nil }
func (m *ticketModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *ticketModel) View() tea.View                      { return tea.NewView(m.render()) }

const ticketIndent = "  "

// panelWidth is the width of each panel, after the screen indent. width is the
// text inside it, after the panel's own padding.
func (m *ticketModel) panelWidth() int { return max(14, m.v.width-len(ticketIndent)) }
func (m *ticketModel) width() int      { return m.panelWidth() - 4 }

// render lays the sections out top to bottom, each in its own panel, with a
// blank line between them. Without a background a panel is just its text.
func (m *ticketModel) render() string {
	sections := [][]string{m.header(), m.track()}
	if m.a.Ticket.Error == "" {
		sections = append(sections, m.attention(), m.pullRequests(), m.nextMoves())
	}
	return panels(m.t, m.panelWidth(), sections)
}

func (m *ticketModel) printed(g graphics, id int) string {
	if g.on && id <= lastImageID && m.a.Ticket.Error == "" && slices.Contains(m.w.Policy.States, m.a.Ticket.State) {
		var sequence string
		sequence, m.flow = ticketDiagram(m.w.Policy, m.a).printedPicture(g, m.width(), id)
		return sequence + m.render()
	}
	return m.render()
}

// wrap breaks text to the screen width, then styles each line, so colour codes
// and links never change the layout.
func (m *ticketModel) wrap(text string, style func(string) string) []string {
	lines := wrapped(text, m.width())
	for i, line := range lines {
		lines[i] = style(line)
	}
	return lines
}

// address writes out a web address when the terminal cannot show hyperlinks,
// such as piped output, breaking it to fit the width.
func (m *ticketModel) address(target string) []string {
	if m.v.hyperlinks() || target == "" {
		return nil
	}
	var lines []string
	for _, r := range textRows(target, "", muted, "", m.width()) {
		lines = append(lines, m.v.render(r))
	}
	return lines
}

// heading is a section title. Neutral sections have a dim title; a title with
// an icon says whether the section needs work.
func (m *ticketModel) heading(text string) string {
	return m.t.dim(strings.ToUpper(text))
}
func (m *ticketModel) verdictHeading(text string, ok bool) string {
	if ok {
		return m.t.good("✓ ") + m.t.bold(strings.ToUpper(text))
	}
	return m.t.bad("✗ ") + m.t.bold(strings.ToUpper(text))
}

func (m *ticketModel) header() []string {
	k := m.a.Ticket
	lines := []string{m.t.bold(m.v.link(k.Key, k.URL))}
	lines = append(lines, m.address(k.URL)...)
	for _, line := range wrapped(k.Title, m.width()) {
		lines = append(lines, line)
	}
	var facts []string
	if kind, _ := k.Facts["type"].(string); kind != "" {
		facts = append(facts, kind)
	}
	facts = append(facts, m.assigned())
	if k.OutOfScope {
		facts = append(facts, "not on the board; shown because you asked")
	}
	return append(lines, m.wrap(strings.Join(facts, " · "), m.t.dim)...)
}

func (m *ticketModel) assigned() string {
	name, _ := m.a.Ticket.Facts["assignee"].(string)
	if name == "" && m.a.Ticket.Assignee != "" {
		for _, person := range workfile.SortedKeys(m.w.People) {
			if m.w.People[person][m.w.Policy.Tracker] == m.a.Ticket.Assignee {
				name = m.w.DisplayName(person)
				break
			}
		}
		if name == "" {
			name = "name unavailable"
		}
	}
	if name == "" {
		return "Unassigned"
	}
	return "Assigned to " + name
}

func (m *ticketModel) track() []string {
	p := m.w.Policy
	if len(p.States) == 0 {
		return nil
	}
	if m.a.Ticket.Error != "" {
		// An error replaces the track: say what is wrong and what to do next.
		action := fmt.Sprintf("Map this ticket's %s status in providers.yml before checking transitions.", m.w.TrackerLabel())
		if strings.Contains(m.a.Ticket.Error, "evidence") {
			action = "Restore access to the missing evidence, then run wf again. Unknown checks are listed below."
		}
		lines := m.wrap(m.a.Ticket.Error, m.t.bad)
		return append(lines, m.wrap(action, m.t.bad)...)
	}
	lines := []string{m.heading("Where it is")}
	lines = append(lines, ticketWorkflowLines(p, m.a, m.v, m.width(), m.flow)...)
	if !slices.Contains(p.States, m.a.Ticket.State) {
		lines = append(lines, m.wrap("! Current stage is not mapped to this workflow.", m.t.dim)...)
	}
	return append(lines, m.verdict()...)
}

// verdict is one sentence that says what to do about this ticket.
func (m *ticketModel) verdict() []string {
	a := m.a
	switch {
	case a.Violation():
		return m.wrap("Out of policy: "+quantity(unmet(a.Earlier), "earlier requirement is", "earlier requirements are")+" not met.", m.t.bad)
	case len(a.Routes) == 0:
		return m.wrap("Complete. No next steps.", m.t.good)
	case a.Blocked():
		return m.wrap("Blocked: "+quantity(len(m.blockers()), "thing needs", "things need")+" attention before it can move on.", m.t.bad)
	}
	for _, route := range routes(a) {
		if route.Available() && !route.Backward {
			return m.wrap("Ready to move to "+route.To+".", m.t.good)
		}
	}
	return m.wrap("Ready to move.", m.t.good)
}

func unmet(checks []workfile.Check) int {
	n := 0
	for _, c := range checks {
		if c.Outcome == "fail" || c.Outcome == "unknown" {
			n++
		}
	}
	return n
}

// attentionItem is one fix, and where it applies.
type attentionItem struct {
	target, link, action, gate, fact string
	to                               []string
}

// blockers lists each unmet fix once, under the ticket or PR it applies to.
// It looks at the earlier gates when the ticket is out of policy, and at the
// forward moves that are blocked. Backward moves are left out: they are rarely
// what a person is trying to do.
func (m *ticketModel) blockers() []attentionItem {
	a := m.a
	type source struct {
		label  string
		checks []workfile.Check
	}
	var sources []source
	if m.to != "" {
		// One destination: its checks already include the earlier gates.
		for _, route := range a.Routes {
			if route.To == m.to {
				sources = append(sources, source{checks: route.Checks})
			}
		}
	} else {
		if a.Violation() {
			sources = append(sources, source{checks: a.Earlier})
		}
		for _, route := range routes(a) {
			if !route.Backward && !route.Available() {
				sources = append(sources, source{label: route.To, checks: route.Checks})
			}
		}
	}
	var items []attentionItem
	for _, s := range sources {
		for _, check := range s.checks {
			for _, reason := range check.Reasons {
				target, link := reasonTarget(a, reason)
				action := strings.TrimSuffix(reason.Action, ".")
				at := slices.IndexFunc(items, func(i attentionItem) bool {
					return i.target == target && i.link == link && i.action == action && i.gate == check.Gate
				})
				if at < 0 {
					items = append(items, attentionItem{target: target, link: link, action: action, gate: check.Gate, fact: check.Condition.Fact})
					at = len(items) - 1
				}
				if s.label != "" && !slices.Contains(items[at].to, s.label) {
					items[at].to = append(items[at].to, s.label)
				}
			}
		}
	}
	if m.to == "" {
		for _, provider := range workfile.SortedKeys(a.Ticket.Records) {
			for _, pr := range a.Ticket.Records[provider] {
				if pr.UnresolvedReviewThreads {
					items = append(items, attentionItem{target: pr.ID, link: pr.URL, action: workfile.ResolveReviewThreads})
				}
				if pr.Facts["state"] == "open" && pr.Facts["checks"] == "failure" && pr.Unavailable["checks"] == "" && !slices.ContainsFunc(items, func(item attentionItem) bool {
					return item.link == pr.URL && item.target == pr.ID && item.fact == "checks"
				}) {
					items = append(items, attentionItem{target: pr.ID, link: pr.URL, action: "Fix failing checks", fact: "checks"})
				}
			}
		}
	}
	// Group by target so each ticket or PR is named once. The ticket's own fixes
	// come first; each PR keeps the order it first appeared in.
	order := []string{a.Ticket.Key}
	for _, item := range items {
		if !slices.Contains(order, item.target+"\x00"+item.link) && item.target != a.Ticket.Key {
			order = append(order, item.target+"\x00"+item.link)
		}
	}
	rank := func(i attentionItem) int {
		if i.target == a.Ticket.Key {
			return 0
		}
		return slices.Index(order, i.target+"\x00"+i.link)
	}
	slices.SortStableFunc(items, func(x, y attentionItem) int { return rank(x) - rank(y) })
	return items
}

func (m *ticketModel) attention() []string {
	items := m.blockers()
	lines := []string{m.verdictHeading("Needs attention", len(items) == 0)}
	if len(items) == 0 {
		return append(lines, m.wrap("Nothing needs attention.", m.t.good)...)
	}
	return append(lines, m.fixes(items)...)
}

// fixes lists each fix once, numbered, under the ticket or PR it applies to.
func (m *ticketModel) fixes(items []attentionItem) []string {
	var lines []string
	var last string
	n := 0
	for _, item := range items {
		if item.target+item.link != last {
			if last != "" {
				lines = append(lines, "")
			}
			last = item.target + item.link
			lines = append(lines, m.t.bold(m.v.link(item.target, item.link)))
		}
		n++
		prefix := fmt.Sprintf("  %d. ", n)
		lead := strings.Repeat(" ", runewidth.StringWidth(prefix))
		text := item.action
		if item.gate != "" {
			text += " - " + human(item.gate)
		}
		text += "."
		for i, line := range wrapped(text, max(1, m.width()-runewidth.StringWidth(prefix))) {
			if i == 0 {
				lines = append(lines, m.t.bad(prefix)+line)
			} else {
				lines = append(lines, lead+line)
			}
		}
		if len(item.to) > 0 {
			for _, line := range wrapped("needed to move to "+strings.Join(item.to, " or "), max(1, m.width()-runewidth.StringWidth(lead))) {
				lines = append(lines, lead+m.t.dim(line))
			}
		}
	}
	return lines
}

func (m *ticketModel) pullRequests() []string {
	entries := prEntries(m.a)
	lines := []string{m.heading("Pull requests")}
	if len(entries) == 0 {
		message := "No linked PRs. Include the ticket key in the PR title, start its branch with the key, or link to the ticket in the description."
		if len(m.w.Providers) == 1 {
			message = "No GitHub provider configured."
		}
		return append(lines, m.wrap(message, m.t.dim)...)
	}
	slices.SortStableFunc(entries, func(a, b prEntry) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(a.repo+a.number, b.repo+b.number)
	})
	repoWidth := 0
	for _, e := range entries {
		repoWidth = max(repoWidth, runewidth.StringWidth(e.repo+" "+e.number))
	}
	for _, e := range entries {
		name := e.repo + " " + e.number
		pad := strings.Repeat(" ", max(0, repoWidth-runewidth.StringWidth(name)))
		status := e.status
		if e.next != "" {
			status += "  " + e.next
		}
		var coloured string
		switch e.color {
		case green:
			coloured = m.t.good(status)
		case red:
			coloured = m.t.bad(status)
		default:
			coloured = m.t.dim(status)
		}
		line := m.t.bold(m.v.link(name, e.record.URL)) + pad + "  " + coloured
		if runewidth.StringWidth(name)+2+runewidth.StringWidth(status) > m.width() {
			// Too narrow for one line: the PR on one line, its status under it.
			lines = append(lines, m.t.bold(m.v.link(name, e.record.URL)), "  "+coloured)
			lines = append(lines, m.address(e.record.URL)...)
			continue
		}
		lines = append(lines, line)
		lines = append(lines, m.address(e.record.URL)...)
	}
	return lines
}

func (m *ticketModel) nextMoves() []string {
	if len(m.a.Routes) == 0 {
		return nil
	}
	lines := []string{m.heading("Next moves")}
	moves := routes(m.a)
	labelWidth := 0
	for _, route := range moves {
		labelWidth = max(labelWidth, runewidth.StringWidth(route.To)+4)
	}
	for _, route := range moves {
		arrow := "→ "
		if route.Backward {
			arrow = "↩ "
		}
		label, note, colour := "✓ "+arrow+route.To, "ready now", m.t.good
		if !route.Available() {
			label, note, colour = "✗ "+arrow+route.To, quantity(unmet(route.Checks), "requirement", "requirements")+" not met", m.t.bad
		}
		if labelWidth+2+runewidth.StringWidth(note) > m.width() {
			// Too narrow for one line: the note goes under its destination.
			lines = append(lines, colour(label), "    "+m.t.dim(note))
			continue
		}
		pad := strings.Repeat(" ", labelWidth-runewidth.StringWidth(label)+2)
		lines = append(lines, colour(label)+pad+m.t.dim(note))
	}
	return lines
}
