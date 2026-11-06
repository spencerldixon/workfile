package cli

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	v view
	t theme
	w *workfile.Workspace
	a workfile.Assessment
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
func (m *ticketModel) heading(text string) string { return m.t.dim(strings.ToUpper(text)) }
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

// sideStates are stages a ticket parks in or leaves the workflow through, such
// as blocked and cancelled. They are not on the forward path, so the track
// leaves them out and notes them only when current.
//
// A stage is a side stage when it is an end stage other than the first one (done
// is the first), or a detour: its only exit goes back to an earlier stage that
// moves into it (blocked, which returns to doing).
func sideStates(p workfile.Policy) []string {
	exits := func(state string) []string {
		var to []string
		for _, route := range p.Routes(state) {
			to = append(to, route.To)
		}
		return to
	}
	var side []string
	firstEnd := true
	for i, state := range p.States {
		if p.Transitions[state].End {
			if !firstEnd {
				side = append(side, state)
			}
			firstEnd = false
			continue
		}
		out := exits(state)
		if len(out) == 1 && slices.Index(p.States, out[0]) < i && slices.Contains(exits(out[0]), state) {
			side = append(side, state)
		}
	}
	return side
}

// forwardStages is one path through the workflow, start to finish, through
// the current stage when it is on the main track. From each stage it takes the
// nearest stage it can move forward to, and before the current stage the
// nearest stage that moves into it. Alternatives, such as security beside qa,
// appear only when the ticket is in them.
func forwardStages(p workfile.Policy, current string) []string {
	side := sideStates(p)
	index := func(state string) int { return slices.Index(p.States, state) }
	moves := func(from, to string) bool {
		return slices.ContainsFunc(p.Routes(from), func(r workfile.Route) bool { return r.To == to })
	}
	main := slices.DeleteFunc(slices.Clone(p.States), func(s string) bool { return slices.Contains(side, s) })
	if len(main) == 0 {
		return nil
	}
	next := func(from string) string {
		for _, s := range main {
			if index(s) > index(from) && moves(from, s) {
				return s
			}
		}
		return ""
	}
	prev := func(to string) string {
		for i := len(main) - 1; i >= 0; i-- {
			if index(main[i]) < index(to) && moves(main[i], to) {
				return main[i]
			}
		}
		return ""
	}
	start := main[0]
	var path []string
	if slices.Contains(main, current) {
		for s := prev(current); s != ""; s = prev(s) {
			path = append([]string{s}, path...)
		}
		start = current
	}
	for s := start; s != ""; s = next(s) {
		path = append(path, s)
	}
	return path
}

// forwardPath draws the path as a stepper: a line of nodes joined by links,
// with the stage names under them. Stages behind the ticket are solid (●), the
// current stage is bold (◉), and stages ahead are dim (○). The link out of the
// current stage says whether that move is open: a solid green line when it is
// ready, a dashed red one when it is blocked. Earlier stages are not ticked,
// because Workfile cannot prove the path a ticket took.
//
// Gaps shrink to fit the width, then the stepper wraps, each new line picking
// up the link where the last one stopped.
func (m *ticketModel) forwardPath(path []string) []string {
	if len(path) == 0 {
		return nil
	}
	at := slices.Index(path, m.a.Ticket.State)
	ready := at >= 0 && at+1 < len(path) && slices.ContainsFunc(m.a.Routes, func(r workfile.RouteAssessment) bool {
		return r.To == path[at+1] && r.Available()
	})
	here := lipgloss.ANSIColor(2)
	if m.a.Violation() || m.a.Blocked() {
		here = lipgloss.ANSIColor(1)
	}
	strong := func(s string, underline bool) string {
		return m.t.render(s, func(st lipgloss.Style) lipgloss.Style {
			return st.Bold(true).Underline(underline).Foreground(here)
		})
	}
	node := func(i int) string {
		switch {
		case i == at:
			return strong("◉", false)
		case i < at:
			return "●"
		default:
			return m.t.dim("○")
		}
	}
	name := func(i int) string {
		switch {
		case i == at:
			return strong(path[i], true)
		case i < at:
			return path[i]
		default:
			return m.t.dim(path[i])
		}
	}
	// link is the line from stage i to stage i+1, n columns long.
	link := func(i, n int) string {
		n = max(n, 1)
		switch {
		case i < at:
			return strings.Repeat("━", n)
		case i == at && ready:
			return m.t.good(strings.Repeat("━", n))
		case i == at:
			return m.t.bad(strings.Repeat("╌", n))
		default:
			return m.t.dim(strings.Repeat("─", n))
		}
	}
	widths := make([]int, len(path))
	total := 0
	for i, state := range path {
		widths[i] = runewidth.StringWidth(state)
		total += widths[i]
	}
	gap := 2
	for _, g := range []int{4, 3} {
		if total+g*(len(path)-1) <= m.width() {
			gap = g
			break
		}
	}
	// Pack stages into rows. A row after the first starts with a short link,
	// and a row that continues leaves one column for its trailing link.
	const lead = 2
	var rows [][]int
	used := 0
	for i := range path {
		tail := 1
		if i+1 == len(path) {
			tail = 0
		}
		if len(rows) > 0 && used+widths[i]+tail <= m.width() {
			rows[len(rows)-1] = append(rows[len(rows)-1], i)
			used += widths[i] + gap
			continue
		}
		rows = append(rows, []int{i})
		used = widths[i] + gap
		if i > 0 {
			used += lead
		}
	}
	var lines []string
	for _, row := range rows {
		nodes, names := "", ""
		if row[0] > 0 {
			nodes, names = link(row[0]-1, lead), strings.Repeat(" ", lead)
		}
		for k, i := range row {
			nodes += node(i)
			names += name(i)
			switch {
			case k+1 < len(row):
				nodes += link(i, widths[i]+gap-1)
				names += strings.Repeat(" ", gap)
			case i+1 < len(path):
				nodes += link(i, widths[i])
			}
		}
		lines = append(lines, nodes, names)
	}
	return lines
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
	// The workflow graph is shared with the old details view. It lays out to the
	// view's inner width, so give it a view as wide as this screen's text.
	path := forwardStages(p, m.a.Ticket.State)
	lines := []string{m.heading("Where it is")}
	lines = append(lines, m.forwardPath(path)...)
	if !slices.Contains(path, m.a.Ticket.State) {
		lines = append(lines, m.wrap("Off the main path: "+m.a.Ticket.State, m.t.bad)...)
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
	target, link, action, gate string
	to                         []string
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
					items = append(items, attentionItem{target: target, link: link, action: action, gate: check.Gate})
					at = len(items) - 1
				}
				if s.label != "" && !slices.Contains(items[at].to, s.label) {
					items[at].to = append(items[at].to, s.label)
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
		text := item.action + " — " + human(item.gate) + "."
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
		message := "No linked PRs. A PR links when its title names the ticket, or its description links to it."
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
