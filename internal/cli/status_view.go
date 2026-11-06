package cli

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	bubblehelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"workfile/internal/workfile"
)

// statusScreen answers "what can move on now, and what does each ticket
// need?" It is drawn two ways from the same groups: printed, for pipes and
// plain output, and as an interactive screen with the tickets on the left and
// the chosen ticket's full view on the right.
//
// Both group tickets by what to do: ready to move, then one group per fix,
// then out of policy. Done tickets are counted but not listed.
type statusScreen struct {
	screen
	w   *workfile.Workspace
	o   options
	all []workfile.Assessment
}

func newStatusScreen(v view, w *workfile.Workspace, all []workfile.Assessment, o options) statusScreen {
	return statusScreen{screen: newScreen(v), w: w, o: o, all: all}
}

// interactiveStatus reports whether status can take over the terminal: the
// board, with colour, and a person at the keyboard.
func interactiveStatus(v view, o options, all []workfile.Assessment) bool {
	return len(o.keys) == 0 && !o.evidence && len(all) > 0 && v.color && isTerminal(os.Stdin) && isTerminal(v.out)
}

// exploreStatus runs the interactive screen until the person quits. who is
// the person's identity, for the m key; nil leaves the key out.
func exploreStatus(ctx context.Context, v view, w *workfile.Workspace, all []workfile.Assessment, o options, who map[string]string) error {
	m := newStatusModel(newStatusScreen(v, w, all, o), who)
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)).Run()
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// fix is one thing a ticket needs: an action, the gate it belongs to, and the
// PR it applies to, if any.
type fix struct {
	action, gate, target, link string
}

// ticketFixes lists what a ticket needs, each once: first the gates an out of
// policy ticket should already have passed, then the gates of the moves it
// cannot make yet, forward moves first.
func ticketFixes(a workfile.Assessment) []fix {
	var checks []workfile.Check
	if g := group(a); g == 1 || g == 2 {
		if g == 1 {
			checks = append(checks, a.Earlier...)
		}
		forward := slices.ContainsFunc(a.Routes, func(r workfile.RouteAssessment) bool { return !r.Backward })
		for _, r := range a.Routes {
			if r.Available() || forward && r.Backward {
				continue
			}
			checks = append(checks, r.Checks...)
		}
	}
	var fixes []fix
	for _, c := range checks {
		if c.Outcome != "fail" {
			continue
		}
		for _, reason := range c.Reasons {
			f := fix{action: strings.TrimSuffix(reason.Action, "."), gate: c.Gate}
			if reason.Record != "" && !reason.OnTicket {
				f.target, f.link = reasonTarget(a, reason)
			}
			if !slices.Contains(fixes, f) {
				fixes = append(fixes, f)
			}
		}
	}
	return fixes
}

// sharedAction words a fix so it reads the same for every ticket: the ticket's
// own key becomes "this ticket", and the hint on where to put the key goes.
func sharedAction(action, key string) string {
	if at := strings.Index(action, "; include "+key); at >= 0 {
		action = action[:at]
	}
	if strings.HasPrefix(action, key+" ") {
		action = "This ticket" + strings.TrimPrefix(action, key)
	}
	return strings.ReplaceAll(action, key, "this ticket")
}

// fixGroup is one shared fix and the tickets that need it.
type fixGroup struct {
	action, gate string
	items        []fixItem
}

type fixItem struct {
	a            workfile.Assessment
	target, link string // the PR the fix applies to, or "" for the ticket
}

// groupFixes gathers the tickets that need the same fix, in gate order. With
// first set, a ticket joins only the group of its first fix, so it is listed
// once.
func (s statusScreen) groupFixes(list []workfile.Assessment, first bool) []fixGroup {
	var groups []fixGroup
	for _, a := range list {
		for i, f := range ticketFixes(a) {
			if first && i > 0 {
				break
			}
			action := sharedAction(f.action, a.Ticket.Key)
			at := slices.IndexFunc(groups, func(g fixGroup) bool { return g.action == action && g.gate == f.gate })
			if at < 0 {
				groups = append(groups, fixGroup{action: action, gate: f.gate})
				at = len(groups) - 1
			}
			groups[at].items = append(groups[at].items, fixItem{a: a, target: f.target, link: f.link})
		}
	}
	order := s.w.Policy.GateOrder()
	slices.SortStableFunc(groups, func(x, y fixGroup) int { return slices.Index(order, x.gate) - slices.Index(order, y.gate) })
	return groups
}

// inGroup lists the assessments of one group in workflow order.
func (s statusScreen) inGroup(list []workfile.Assessment, g int) []workfile.Assessment {
	var result []workfile.Assessment
	for _, a := range list {
		if group(a) == g {
			result = append(result, a)
		}
	}
	slices.SortStableFunc(result, func(x, y workfile.Assessment) int {
		return slices.Index(s.w.Policy.States, x.Ticket.State) - slices.Index(s.w.Policy.States, y.Ticket.State)
	})
	return result
}

func (s statusScreen) header(width int, tag string) string {
	return edge(s.t.bold("workfile")+s.t.dim(" status"), s.t.dim(tag), width)
}

// summary is one sentence about the board, the groups with their icons, and
// what the selection leaves out.
func (s statusScreen) summary(list []workfile.Assessment, width int, notes []string) []string {
	t := count(list)
	lead := fmt.Sprintf("%d %s can move on, %d need work and %d %s out of policy.", t.ready, plural(t.ready, "ticket", "tickets"), t.needs, t.out, plural(t.out, "is", "are"))
	if width < 50 {
		lead = fmt.Sprintf("%d can move on, %d need work, %d out of policy.", t.ready, t.needs, t.out)
	}
	if len(list) == 0 {
		lead = "No tickets match this selection."
	}
	var lines []string
	for _, line := range wrapped(lead, width) {
		lines = append(lines, s.t.bold(line))
	}
	items := []struct {
		icon string
		mark func(string) string
		text string
		n    int
	}{
		{"!", s.t.bad, "cannot check", t.cannot},
		{"‼", s.t.bad, "out of policy", t.out},
		{"✗", s.t.bad, "need work", t.needs},
		{"✓", s.t.good, "ready", t.ready},
		{"·", s.t.dim, "done", t.done},
	}
	legend, used := "", 0
	for _, item := range items {
		if item.n == 0 && item.icon == "!" {
			continue
		}
		text := fmt.Sprintf("%d %s", item.n, item.text)
		w := 2 + len([]rune(text))
		if used > 0 && used+3+w > width {
			lines = append(lines, legend)
			legend, used = "", 0
		}
		if used > 0 {
			legend += "   "
			used += 3
		}
		legend += item.mark(item.icon) + " " + s.t.dim(text)
		used += w
	}
	lines = append(lines, legend)
	for _, note := range notes {
		for _, line := range wrapped(note, width) {
			lines = append(lines, s.t.dim(line))
		}
	}
	return lines
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// notes says what the selection leaves out: a sample, or other people's work.
func (s statusScreen) notes() []string {
	var notes []string
	if s.o.me {
		notes = append(notes, "Only tickets involving you.")
	}
	if s.o.user != "" {
		notes = append(notes, "Only tickets involving "+s.w.DisplayName(s.o.user)+".")
	}
	if s.o.limited() && s.o.limit > 0 && len(s.all) >= s.o.limit {
		notes = append(notes, fmt.Sprintf("The first %d tickets in your %s search; wf status --all checks every one.", s.o.limit, s.w.TrackerLabel()))
	}
	return notes
}

// moveTag says where a ready ticket can go, such as "doing → review".
func moveTag(a workfile.Assessment) string {
	if len(a.Next) == 0 {
		return ""
	}
	return "→ " + strings.Join(a.Next, " or ")
}

// statusIcon is the icon for a ticket's group, and how to colour it.
func (s statusScreen) statusIcon(a workfile.Assessment) string {
	switch group(a) {
	case 0:
		return s.t.bad("!")
	case 1:
		return s.t.bad("‼")
	case 2:
		return s.t.bad("✗")
	case 3:
		return s.t.good("✓")
	}
	return s.t.dim("·")
}

// ticketRow is an icon, the linked key and the title, with a tag at the right
// edge. The title is cut to fit; the tag goes first when there is no room.
func (s screen) ticketRow(lead, icon string, a workfile.Assessment, tag string, tagStyle func(string) string, width int) string {
	k := a.Ticket
	head := lead + icon + " " + s.t.bold(s.v.link(k.Key, k.URL))
	headW := lipgloss.Width(head)
	tag = clean(tag)
	room := width - headW - 2 - lipgloss.Width(tag) - 2
	if tag == "" || room < 8 {
		tag = ""
		room = width - headW - 2
	}
	if room >= 4 {
		head += "  " + s.v.link(truncate(clean(k.Title), room), k.URL)
	}
	return edge(head, tagStyle(tag), width)
}

// printed is the plain report: what can move, each fix with the tickets that
// need it, then tickets out of policy and tickets that cannot be checked.
func (s statusScreen) printed() string {
	width := s.width()
	top := []string{"", s.header(width, quantity(len(s.all), "ticket", "tickets")), ""}
	top = append(top, s.summary(s.all, width, s.notes())...)
	for i, line := range top {
		top[i] = strings.TrimRight(ticketIndent+line, " ")
	}
	out := strings.Join(top, "\n") + "\n"

	// Without hyperlinks, each web address is written out once, under the
	// first row that links it.
	seen := map[string]bool{}
	addresses := func(indent string, urls ...string) []string {
		if s.v.hyperlinks() {
			return nil
		}
		var lines []string
		for _, u := range urls {
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			for _, part := range chunks(u, max(1, width-len(indent))) {
				lines = append(lines, indent+s.t.dim(part))
			}
		}
		return lines
	}

	var sections [][]string
	if ready := s.inGroup(s.all, 3); len(ready) > 0 && !s.o.failing {
		lines := append(s.tagged("Ready to move", quantity(len(ready), "ticket", "tickets"), s.t.bold, s.t.dim), "")
		for _, a := range ready {
			lines = append(lines, s.ticketRow("", s.statusIcon(a), a, a.Ticket.State+" "+moveTag(a), s.t.dim, width))
			lines = append(lines, addresses("    ", a.Ticket.URL)...)
		}
		sections = append(sections, lines)
	}
	if needs := s.inGroup(s.all, 2); len(needs) > 0 {
		lines := append(s.tagged("Needs work", quantity(len(needs), "ticket", "tickets")+" · grouped by fix", s.t.bold, s.t.dim), "")
		for i, g := range s.groupFixes(needs, false) {
			if i > 0 {
				lines = append(lines, "")
			}
			title := wrapped(g.action, max(1, width-6))
			lines = append(lines, edge(s.t.bold(title[0]), s.t.bad(fmt.Sprint(len(g.items))), width))
			for _, more := range title[1:] {
				lines = append(lines, s.t.bold(more))
			}
			lines = append(lines, s.t.dim(human(g.gate)))
			for _, item := range g.items {
				tag := item.a.Ticket.State
				style := s.t.dim
				if item.target != "" {
					tag = item.target + " · " + tag
					target, link := item.target, item.link
					style = func(text string) string {
						return s.t.dim(strings.Replace(text, target, s.v.link(target, link), 1))
					}
				}
				lines = append(lines, s.ticketRow("  ", s.statusIcon(item.a), item.a, tag, style, width))
				lines = append(lines, addresses("      ", item.a.Ticket.URL, item.link)...)
			}
		}
		sections = append(sections, lines)
	}
	if broken := s.inGroup(s.all, 1); len(broken) > 0 {
		lines := append(s.tagged("Out of policy", "past a gate they fail", s.t.bold, s.t.dim), "")
		for i, a := range broken {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, s.ticketRow("", s.statusIcon(a), a, a.Ticket.State, s.t.dim, width))
			lines = append(lines, addresses("    ", a.Ticket.URL)...)
			for _, f := range ticketFixes(a) {
				lines = append(lines, s.fixLines(f, width, "    ")...)
				lines = append(lines, addresses("    ", f.link)...)
			}
		}
		sections = append(sections, lines)
	}
	if cannot := s.inGroup(s.all, 0); len(cannot) > 0 {
		lines := append(s.tagged("Cannot check", quantity(len(cannot), "ticket", "tickets"), s.t.bold, s.t.dim), "")
		for _, a := range cannot {
			lines = append(lines, s.ticketRow("", s.statusIcon(a), a, "", s.t.dim, width))
			lines = append(lines, addresses("    ", a.Ticket.URL)...)
			lines = append(lines, s.indented(a.Ticket.Error, "    ", s.t.dim)...)
		}
		sections = append(sections, lines)
	}
	if len(sections) > 0 {
		out += panels(s.t, s.panelWidth(), sections)
	} else {
		out += "\n"
	}
	if done := count(s.all).done; done > 0 {
		out += ticketIndent + s.t.dim(quantity(done, "done ticket is", "done tickets are")+" not shown.") + "\n\n"
	}
	return out
}

// fixLines is one fix under a ticket: the action, then the PR and gate it
// belongs to, on the same line when they fit.
func (s screen) fixLines(f fix, width int, indent string) []string {
	where := human(f.gate)
	if f.target != "" {
		where = f.target + " · " + where
	}
	lines := wrapped(f.action, max(1, width-len(indent)))
	last := len(lines) - 1
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = indent + line
	}
	shown := where
	if f.target != "" {
		shown = strings.Replace(where, f.target, s.v.link(f.target, f.link), 1)
	}
	if lipgloss.Width(result[last])+3+len([]rune(where)) <= width {
		result[last] += s.t.dim(" · " + shown)
	} else {
		result = append(result, indent+s.t.dim(shown))
	}
	return result
}

// chunks cuts text into pieces no wider than width, for web addresses that
// have no spaces to wrap at.
func chunks(text string, width int) []string {
	var parts []string
	runes := []rune(text)
	for len(runes) > width {
		parts = append(parts, string(runes[:width]))
		runes = runes[width:]
	}
	return append(parts, string(runes))
}

// statusEntry is one row of the interactive list: a group heading, or a
// ticket in it.
type statusEntry struct {
	heading string
	a       workfile.Assessment
	tag     string
	ready   bool
}

type statusKeys struct {
	Choose, Open, Back, Mine, Page, Quit key.Binding
}

// statusModel is the interactive status screen. Tickets are listed on the
// left, grouped by what to do; the panel on the right is the full view of the
// chosen ticket, the same as wf status TICKET. Narrow terminals show the list
// and the panel one at a time.
type statusModel struct {
	statusScreen
	who     map[string]string
	mine    bool
	entries []statusEntry
	sel     int // index into the ticket entries
	open    bool
	height  int
	pane    viewport.Model
	keys    statusKeys
	help    bubblehelp.Model
}

func newStatusModel(s statusScreen, who map[string]string) *statusModel {
	m := &statusModel{statusScreen: s, who: who, height: 24, pane: viewport.New()}
	m.keys = statusKeys{
		Choose: key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("↑↓", "choose")),
		Open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Mine:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "only mine")),
		Page:   key.NewBinding(key.WithKeys("pgdown", "pgup"), key.WithHelp("pgdn", "more")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
	m.help = s.t.help()
	m.build()
	return m
}

// shown is the work the list covers: everyone's, or the person's own.
func (m *statusModel) shown() []workfile.Assessment {
	if !m.mine || m.who == nil {
		return m.all
	}
	return slices.DeleteFunc(slices.Clone(m.all), func(a workfile.Assessment) bool { return !a.Ticket.Involves(m.w.Policy.Tracker, m.who) })
}

// build groups the list: ready to move, one group per fix with each ticket
// under its first fix, out of policy, then tickets that cannot be checked.
func (m *statusModel) build() {
	current := ""
	if t := m.tickets(); m.sel < len(t) {
		current = t[m.sel].a.Ticket.Key
	}
	list := m.shown()
	m.entries = nil
	add := func(heading string, items []statusEntry) {
		if len(items) == 0 {
			return
		}
		m.entries = append(m.entries, statusEntry{heading: heading})
		m.entries = append(m.entries, items...)
	}
	var items []statusEntry
	if !m.o.failing {
		for _, a := range m.inGroup(list, 3) {
			items = append(items, statusEntry{a: a, tag: moveTag(a), ready: true})
		}
		add("Ready to move", items)
	}
	for _, g := range m.groupFixes(m.inGroup(list, 2), true) {
		items = nil
		for _, item := range g.items {
			items = append(items, statusEntry{a: item.a, tag: item.a.Ticket.State})
		}
		add(capitalise(g.action), items)
	}
	for _, g := range []struct {
		heading string
		group   int
	}{{"Out of policy", 1}, {"Cannot check", 0}} {
		items = nil
		for _, a := range m.inGroup(list, g.group) {
			items = append(items, statusEntry{a: a, tag: a.Ticket.State})
		}
		add(g.heading, items)
	}
	m.sel = 0
	for i, e := range m.tickets() {
		if e.a.Ticket.Key == current {
			m.sel = i
		}
	}
	m.layout()
}

func (m *statusModel) tickets() []statusEntry {
	var t []statusEntry
	for _, e := range m.entries {
		if e.heading == "" {
			t = append(t, e)
		}
	}
	return t
}

func (m *statusModel) Init() tea.Cmd { return nil }

func (m *statusModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *statusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.v.width = max(20, min(msg.Width, 140))
		m.height = max(10, msg.Height)
	case tea.KeyPressMsg:
		if cmd := m.key(msg); cmd != nil {
			return m, cmd
		}
	}
	m.layout()
	return m, nil
}

func (m *statusModel) wide() bool { return m.v.width >= 70 }

func (m *statusModel) key(msg tea.KeyPressMsg) tea.Cmd {
	up := msg.String() == "up" || msg.String() == "k"
	down := msg.String() == "down" || msg.String() == "j"
	n := len(m.tickets())
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Mine) && m.who != nil:
		m.mine = !m.mine
		m.build()
	case key.Matches(msg, m.keys.Page):
		m.pane, _ = m.pane.Update(msg)
	case m.open && !m.wide():
		switch {
		case up:
			m.pane.ScrollUp(1)
		case down:
			m.pane.ScrollDown(1)
		case key.Matches(msg, m.keys.Back):
			m.open = false
		}
	case n == 0:
	case up:
		m.sel = (m.sel + n - 1) % n
		m.pane.GotoTop()
	case down:
		m.sel = (m.sel + 1) % n
		m.pane.GotoTop()
	case key.Matches(msg, m.keys.Open) && !m.wide():
		m.open = true
		m.pane.GotoTop()
	}
	return nil
}

func (m *statusModel) inner() int { return m.v.width - 2*len(ticketIndent) }

func (m *statusModel) listWidth() int {
	switch {
	case !m.wide():
		return m.inner()
	case m.v.width >= 100:
		return 44
	}
	return 34
}

func (m *statusModel) paneWidth() int {
	if m.wide() {
		return m.inner() - m.listWidth() - 2
	}
	return m.inner()
}

func (m *statusModel) top() []string {
	width := m.inner()
	tag := quantity(len(m.all), "ticket", "tickets")
	notes := m.notes()
	if m.mine {
		tag = "only yours"
		notes = append([]string{"Only tickets involving you."}, notes...)
	}
	lines := []string{m.header(width, tag), ""}
	return append(append(lines, m.summary(m.shown(), width, notes)...), "")
}

func (m *statusModel) bodyHeight() int { return max(4, m.height-len(m.top())-2) }

// layout fits the panel to the screen and fills it with the chosen ticket.
func (m *statusModel) layout() {
	m.help.SetWidth(m.inner())
	width := max(1, m.paneWidth()-4)
	m.pane.SetWidth(width)
	m.pane.SetHeight(max(1, m.bodyHeight()-2))
	t := m.tickets()
	if len(t) == 0 {
		m.pane.SetContentLines([]string{m.t.dim("Nothing to show.")})
		return
	}
	m.pane.SetContentLines(m.ticketView(t[m.sel].a, width))
}

// ticketView is the single-ticket screen, drawn to fit the panel.
func (m *statusModel) ticketView(a workfile.Assessment, width int) []string {
	v := m.v
	v.width = width + 2*len(ticketIndent) + 2
	tm := &ticketModel{v: v, t: newTheme(m.t.color, ""), w: m.w, a: a}
	lines := strings.Split(strings.Trim(tm.render(), "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, ticketIndent)
	}
	return lines
}

func (m *statusModel) render() string {
	lines := m.top()
	var body []string
	switch {
	case m.wide():
		list := m.list()
		pane := m.panel()
		for i := range m.bodyHeight() {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			left += strings.Repeat(" ", max(0, m.listWidth()+2-lipgloss.Width(left)))
			body = append(body, left+pane[i])
		}
	case m.open:
		body = m.panel()
	default:
		body = m.list()
	}
	for len(body) < m.bodyHeight() {
		body = append(body, "")
	}
	lines = append(lines, body[:m.bodyHeight()]...)
	lines = append(lines, "", m.help.ShortHelpView(m.helpKeys()))
	for i, line := range lines {
		lines[i] = ticketIndent + line
	}
	return strings.Join(lines, "\n")
}

func (m *statusModel) helpKeys() []key.Binding {
	var keys []key.Binding
	if m.open && !m.wide() {
		choose := m.keys.Choose
		choose.SetHelp("↑↓", "scroll")
		keys = append(keys, choose, m.keys.Back)
	} else {
		keys = append(keys, m.keys.Choose)
		if !m.wide() {
			keys = append(keys, m.keys.Open)
		}
	}
	if m.wide() && m.pane.TotalLineCount() > m.pane.Height() {
		keys = append(keys, m.keys.Page)
	}
	if m.who != nil {
		mine := m.keys.Mine
		if m.mine {
			mine.SetHelp("m", "everyone")
		}
		keys = append(keys, mine)
	}
	return append(keys, m.keys.Quit)
}

// list is the column of tickets under their group headings, scrolled so the
// chosen ticket stays in view. The chosen row shares the panel's background.
func (m *statusModel) list() []string {
	width := m.listWidth()
	var lines []string
	at, n := 0, 0
	for i, e := range m.entries {
		if e.heading != "" {
			if i > 0 {
				lines = append(lines, "")
			}
			count := 0
			for _, x := range m.entries[i+1:] {
				if x.heading != "" {
					break
				}
				count++
			}
			lines = append(lines, edge(m.t.dim(truncate(e.heading, max(1, width-6))), m.t.dim(fmt.Sprint(count)), width-1))
			continue
		}
		on := n == m.sel
		if on {
			at = len(lines)
		}
		mark := "  "
		if on && m.t.bg == "" {
			mark = "› "
		}
		tagStyle := m.t.dim
		if e.ready {
			tagStyle = m.t.good
		}
		row := m.ticketRow(mark, m.statusIcon(e.a), e.a, e.tag, tagStyle, width-1)
		if on {
			if m.wide() {
				row = m.t.onPanel(row, width+2)
			} else {
				row = m.t.onPanel(row, width)
			}
		}
		lines = append(lines, row)
		n++
	}
	if len(lines) == 0 {
		lines = append(lines, m.t.dim("No tickets match."))
	}
	height := m.bodyHeight()
	start := max(0, min(at-height/3, len(lines)-height))
	return lines[start:min(len(lines), start+height)]
}

func (m *statusModel) panel() []string {
	width := m.paneWidth()
	lines := []string{m.t.onPanel("", width)}
	for _, line := range strings.Split(m.pane.View(), "\n") {
		lines = append(lines, m.t.onPanel("  "+line, width))
	}
	return append(lines, m.t.onPanel("", width))
}
