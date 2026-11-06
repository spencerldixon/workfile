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
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// healthScreen answers "is the board following our workflow, and where does it
// break?" It is drawn two ways from the same facts: printed, for pipes and
// plain output, and as an interactive screen where you pick a gate or a stage
// and see its rules and tickets.
type healthScreen struct {
	screen
	w   *workfile.Workspace
	o   options
	r   workfile.Rollup
	all []workfile.Assessment
}

func newHealthScreen(v view, w *workfile.Workspace, all []workfile.Assessment, o options) healthScreen {
	return healthScreen{screen: newScreen(v), w: w, o: o, r: w.Rollup(all), all: all}
}

// interactiveHealth reports whether health can take over the terminal: a
// summary, with colour, and a person at the keyboard.
func interactiveHealth(v view, o options, all []workfile.Assessment) bool {
	return !o.listing() && !o.evidence && len(all) > 0 && v.color && isTerminal(os.Stdin) && isTerminal(v.out)
}

// exploreHealth runs the interactive screen until the person quits.
func exploreHealth(ctx context.Context, v view, w *workfile.Workspace, all []workfile.Assessment, o options) error {
	m := newHealthModel(newHealthScreen(v, w, all, o))
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)).Run()
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// header is the top line: the command on the left, how much was checked on
// the right.
func (h healthScreen) header(width int) string {
	left := h.t.bold("workfile") + h.t.dim(" health")
	tag := quantity(h.r.Checked+h.r.Unchecked, "ticket", "tickets")
	gap := width - lipgloss.Width(left) - runewidth.StringWidth(tag)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + h.t.dim(tag)
}

// summary is one sentence about the board, notes on what was left out, and
// one bar of tickets out of policy.
func (h healthScreen) summary(width int) []string {
	r := h.r
	lead := fmt.Sprintf("%d of %d tickets are out of policy (%s).", r.OutOfPolicy, r.Checked, percent(r.OutOfPolicy, r.Checked))
	if width < 44 {
		lead = fmt.Sprintf("%d of %d out of policy (%s).", r.OutOfPolicy, r.Checked, percent(r.OutOfPolicy, r.Checked))
	}
	switch {
	case r.Checked == 0:
		lead = "No tickets could be checked."
	case r.OutOfPolicy == 0:
		lead = fmt.Sprintf("All %s are in policy.", quantity(r.Checked, "ticket", "tickets"))
	}
	var lines []string
	for _, line := range wrapped(lead, width) {
		lines = append(lines, h.t.bold(line))
	}
	var notes []string
	if r.Unchecked > 0 {
		notes = append(notes, fmt.Sprintf("%d more could not be checked; wf status --failing shows why.", r.Unchecked))
	}
	if h.o.limitSet {
		notes = append(notes, fmt.Sprintf("This is a sample of up to %d tickets.", h.o.limit))
	}
	if h.o.me {
		notes = append(notes, "Only tickets involving you.")
	}
	if h.o.user != "" {
		notes = append(notes, "Only tickets involving "+h.w.DisplayName(h.o.user)+".")
	}
	for _, note := range notes {
		for _, line := range wrapped(note, width) {
			lines = append(lines, h.t.dim(line))
		}
	}
	if r.Checked > 0 && r.OutOfPolicy > 0 {
		filled, rest := bar(r.OutOfPolicy, r.Checked, min(48, width))
		lines = append(lines, h.t.bad(filled)+h.t.dim(rest))
	}
	return lines
}

// rate is one row of a rate list: a name, how many fail, and of how many.
type rate struct {
	label string
	n, d  int
	bold  bool
}

// rates lays out name, bar, "n / d" and a percentage. Narrow screens drop the
// bar first, then the counts, so the percentage always survives.
func (h healthScreen) rates(items []rate, width int) []string {
	labelW, countW := 0, 0
	for _, r := range items {
		labelW = max(labelW, runewidth.StringWidth(r.label))
		countW = max(countW, len(fmt.Sprintf("%d / %d", r.n, r.d)))
	}
	showBar, showCount := true, true
	fits := func() bool {
		w := labelW + 2 + 4
		if showBar {
			w += barWidth + 2
		}
		if showCount {
			w += countW + 2
		}
		return w <= width
	}
	if !fits() {
		showBar = false
	}
	if !fits() {
		showCount = false
	}
	var lines []string
	for _, r := range items {
		label := pad(r.label, labelW)
		if r.bold {
			label = h.t.bold(label)
		}
		line := label + "  "
		if showBar {
			filled, rest := bar(r.n, r.d, barWidth)
			line += h.t.bad(filled) + h.t.dim(rest) + "  "
		}
		if showCount {
			line += lpad(fmt.Sprintf("%d / %d", r.n, r.d), countW) + "  "
		}
		value := h.t.good
		if r.n > 0 {
			value = h.t.bad
		}
		lines = append(lines, line+value(lpad(percent(r.n, r.d), 4)))
	}
	return lines
}

// edge puts a tag at the right edge of a line, or leaves it out when there is
// no room. Both sides may already be styled.
func edge(left, tag string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(tag)
	if tag == "" || gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + tag
}

// failingRules lists a gate's rules that some ticket fails, in policy order.
func failingRules(g workfile.GateStat) []workfile.RuleStat {
	return slices.DeleteFunc(slices.Clone(g.Rules), func(r workfile.RuleStat) bool { return r.Failing == 0 })
}

// ruleCountWidth is wide enough for every rule's count, so counts line up
// across gates.
func (h healthScreen) ruleCountWidth() int {
	width := 1
	for _, g := range h.r.Gates {
		for _, r := range g.Rules {
			width = max(width, len(fmt.Sprint(r.Failing)))
		}
	}
	return width
}

// printed is the plain report: the summary, then each gate with the rules its
// tickets fail, then the stages.
func (h healthScreen) printed() string {
	width := h.width()
	top := []string{"", h.header(width), ""}
	top = append(top, h.summary(width)...)
	for i, line := range top {
		top[i] = strings.TrimRight(ticketIndent+line, " ")
	}
	out := strings.Join(top, "\n") + "\n"
	if h.r.OutOfPolicy == 0 {
		return out + "\n"
	}

	var gateRates []rate
	for _, g := range h.r.Gates {
		gateRates = append(gateRates, rate{label: human(g.Name), n: g.Failing, d: g.Required, bold: true})
	}
	rows := h.rates(gateRates, width)
	countW := h.ruleCountWidth()
	gates := h.tagged("Gates", "failing of tickets that must pass", h.t.bold, h.t.dim)
	gates = append(gates, "")
	for i, g := range h.r.Gates {
		if i > 0 {
			gates = append(gates, "")
		}
		gates = append(gates, rows[i])
		for _, rule := range failingRules(g) {
			lead := "  " + lpad(fmt.Sprint(rule.Failing), countW) + "  "
			for j, line := range wrapped(rule.Text, max(1, width-len(lead))) {
				if j == 0 {
					gates = append(gates, h.t.bad(lead)+line)
				} else {
					gates = append(gates, strings.Repeat(" ", len(lead))+line)
				}
			}
		}
	}
	gates = append(gates, "")
	gates = append(gates, h.wrap("Under each gate: tickets failing each rule. A ticket can fail more than one.", h.t.dim)...)

	var stageRates []rate
	for _, s := range h.r.Stages {
		stageRates = append(stageRates, rate{label: s.Name, n: s.Failing, d: s.Total})
	}
	stages := h.tagged("Stages", "out of policy of tickets in stage", h.t.bold, h.t.dim)
	stages = append(append(stages, ""), h.rates(stageRates, width)...)

	out += panels(h.t, h.panelWidth(), [][]string{gates, stages})
	if g := h.worstGate(); g != "" {
		out += ticketIndent + h.t.dim("Next  ") + "wf health --gate " + g + "\n\n"
	}
	return out
}

// worstGate is the gate with the highest failure rate, for the next step.
func (h healthScreen) worstGate() string {
	best, rate := "", -1
	for _, g := range h.r.Gates {
		if g.Failing == 0 || g.Required == 0 {
			continue
		}
		if r := g.Failing * 1000 / g.Required; r > rate {
			best, rate = g.Name, r
		}
	}
	return best
}

// gateMove is the first move that requires the gate, such as "todo → doing".
func gateMove(p workfile.Policy, gate string) string {
	for _, state := range p.States {
		for _, route := range p.Routes(state) {
			if !route.Backward && slices.Contains(route.Requires, gate) {
				return state + " → " + route.To
			}
		}
	}
	return ""
}

// healthItem is one row of the list: a gate, or a stage.
type healthItem struct {
	gate  int // index into Rollup.Gates, or -1 for a stage
	stage int // index into Rollup.Stages, or -1 for a gate
}

// healthKeys are the screen's keys, and the words the help line shows.
type healthKeys struct {
	Choose, Open, Back, Quit key.Binding
}

// healthModel is the interactive health screen. Gates and stages are listed on
// the left; the panel on the right explains the one you pick. Enter lists its
// tickets. Narrow terminals show the list and the panel one at a time.
type healthModel struct {
	healthScreen
	items   []healthItem
	sel     int
	tickets bool // the panel lists tickets
	open    bool // narrow terminals: the panel is showing
	height  int
	pane    viewport.Model
	keys    healthKeys
	help    bubblehelp.Model
}

const healthListWidth = 22

func newHealthModel(h healthScreen) *healthModel {
	m := &healthModel{healthScreen: h, height: 24, pane: viewport.New()}
	for i := range h.r.Gates {
		m.items = append(m.items, healthItem{gate: i, stage: -1})
	}
	for i := range h.r.Stages {
		m.items = append(m.items, healthItem{gate: -1, stage: i})
	}
	m.keys = healthKeys{
		Choose: key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("↑↓", "choose")),
		Open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "tickets")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
	m.help = bubblehelp.New()
	m.help.ShortSeparator = "  ·  "
	m.help.Styles.ShortKey = h.t.style(func(s lipgloss.Style) lipgloss.Style { return s })
	m.help.Styles.ShortDesc = h.t.style(func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	m.help.Styles.ShortSeparator = m.help.Styles.ShortDesc
	m.help.Styles.Ellipsis = m.help.Styles.ShortDesc
	m.layout()
	return m
}

func (m *healthModel) Init() tea.Cmd { return nil }

func (m *healthModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *healthModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *healthModel) wide() bool { return m.v.width >= 60 }

func (m *healthModel) key(msg tea.KeyPressMsg) tea.Cmd {
	up := msg.String() == "up" || msg.String() == "k"
	down := msg.String() == "down" || msg.String() == "j"
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case m.tickets:
		switch {
		case up:
			m.pane.ScrollUp(1)
		case down:
			m.pane.ScrollDown(1)
		case key.Matches(msg, m.keys.Back):
			m.tickets = false
		default:
			m.pane, _ = m.pane.Update(msg) // page up and down
		}
	case !m.wide() && !m.open:
		switch {
		case up:
			m.move(-1)
		case down:
			m.move(1)
		case key.Matches(msg, m.keys.Open):
			m.open = true
		}
	default:
		switch {
		case up && m.wide():
			m.move(-1)
		case down && m.wide():
			m.move(1)
		case key.Matches(msg, m.keys.Open) && m.failing(m.items[m.sel]) > 0:
			m.tickets = true
			m.pane.GotoTop()
		case key.Matches(msg, m.keys.Back) && !m.wide():
			m.open = false
		}
	}
	return nil
}

func (m *healthModel) move(by int) {
	m.sel = (m.sel + by + len(m.items)) % len(m.items)
	m.tickets = false
	m.pane.GotoTop()
}

func (m *healthModel) failing(it healthItem) int {
	if it.gate >= 0 {
		return m.r.Gates[it.gate].Failing
	}
	return m.r.Stages[it.stage].Failing
}

// inner is the screen width inside the indent and the same margin on the right.
func (m *healthModel) inner() int { return m.v.width - 2*len(ticketIndent) }

// top is everything above the list: the header and the summary.
func (m *healthModel) top() []string {
	width := m.inner()
	lines := []string{m.header(width), ""}
	return append(append(lines, m.summary(width)...), "")
}

// paneWidth is the width of the right panel, including its padding.
func (m *healthModel) paneWidth() int {
	if m.wide() {
		return m.inner() - healthListWidth - 2
	}
	return m.inner()
}

func (m *healthModel) bodyHeight() int { return max(4, m.height-len(m.top())-2) }

// layout fits the panel to the screen and fills it for the current choice.
func (m *healthModel) layout() {
	m.help.SetWidth(m.inner())
	m.pane.SetWidth(max(1, m.paneWidth()-4))
	m.pane.SetHeight(max(1, m.bodyHeight()-2))
	m.pane.SetContentLines(m.detail(max(1, m.paneWidth()-4)))
}

func (m *healthModel) render() string {
	lines := m.top()
	var body []string
	switch {
	case m.wide():
		list := m.list(healthListWidth)
		pane := m.panel()
		for i := range m.bodyHeight() {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			left += strings.Repeat(" ", max(0, healthListWidth+2-lipgloss.Width(left)))
			body = append(body, left+pane[i])
		}
	case m.open:
		body = m.panel()
	default:
		body = m.list(m.inner())
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

// helpKeys are the keys that do something right now.
func (m *healthModel) helpKeys() []key.Binding {
	choose, open, back := m.keys.Choose, m.keys.Open, m.keys.Back
	if m.tickets {
		choose.SetHelp("↑↓", "scroll")
		return []key.Binding{choose, back, m.keys.Quit}
	}
	if !m.wide() && !m.open {
		open.SetHelp("enter", "open")
		return []key.Binding{choose, open, m.keys.Quit}
	}
	keys := []key.Binding{}
	if m.wide() {
		keys = append(keys, choose)
	}
	if m.failing(m.items[m.sel]) > 0 {
		keys = append(keys, open)
	}
	if !m.wide() {
		keys = append(keys, back)
	}
	return append(keys, m.keys.Quit)
}

// list is the column of gates and stages. The chosen row shares the panel's
// background, so it reads as attached to the panel beside it. Without a
// background it is marked with an arrow instead.
func (m *healthModel) list(width int) []string {
	var lines []string
	section := ""
	for i, it := range m.items {
		name, n, d := "", 0, 0
		if it.gate >= 0 {
			g := m.r.Gates[it.gate]
			name, n, d = human(g.Name), g.Failing, g.Required
			if section != "gates" {
				lines = append(lines, m.t.dim("Gates"))
				section = "gates"
			}
		} else {
			s := m.r.Stages[it.stage]
			name, n, d = s.Name, s.Failing, s.Total
			if section != "stages" {
				lines = append(lines, "", m.t.dim("Stages"))
				section = "stages"
			}
		}
		on := i == m.sel
		mark := " "
		if on && m.t.bg == "" {
			mark = "›"
		}
		markW := 1
		value := m.t.good
		if n > 0 {
			value = m.t.bad
		}
		rateText := lpad(percent(n, d), 4) + " "
		label := truncate(name, max(1, width-markW-1-len(rateText)))
		if on {
			label = m.t.bold(label)
		}
		row := mark + " " + label
		row += strings.Repeat(" ", max(0, width-lipgloss.Width(row)-len(rateText))) + value(rateText)
		if m.wide() {
			if on {
				row = m.t.onPanel(row, width+2)
			} else {
				row += "  "
			}
		} else if on {
			row = m.t.onPanel(row, width)
		}
		lines = append(lines, row)
	}
	return lines
}

// panel draws the viewport on the panel background, with a line of room
// above and below.
func (m *healthModel) panel() []string {
	width := m.paneWidth()
	lines := []string{m.t.onPanel("", width)}
	for _, line := range strings.Split(m.pane.View(), "\n") {
		lines = append(lines, m.t.onPanel("  "+line, width))
	}
	return append(lines, m.t.onPanel("", width))
}

// detail is the panel's content for the chosen gate or stage.
func (m *healthModel) detail(width int) []string {
	it := m.items[m.sel]
	if it.gate >= 0 {
		return m.gateDetail(m.r.Gates[it.gate], width)
	}
	return m.stageDetail(it.stage, width)
}

func (m *healthModel) gateDetail(g workfile.GateStat, width int) []string {
	name := capitalise(human(g.Name))
	if m.tickets {
		lines := []string{m.t.bold(name) + m.t.dim("  "+quantity(g.Failing, "ticket", "tickets")), ""}
		for i, rule := range failingRules(g) {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, edge(truncate(rule.Text, max(1, width-6)), m.t.bad(lpad(fmt.Sprint(rule.Failing), 3)), width))
			for _, a := range m.all {
				if failsRule(a, g.Name, rule.Text) {
					lines = append(lines, m.ticketRow(" ", "", a, a.Ticket.State, m.t.dim, width))
				}
			}
		}
		return lines
	}
	lines := []string{
		edge(m.t.bold(name), m.t.dim(gateMove(m.w.Policy, g.Name)), width),
		fmt.Sprintf("%d of %d tickets fail  ", g.Failing, g.Required) + m.rateText(g.Failing, g.Required),
		"",
		edge(m.t.dim("Rules"), m.t.dim("failing"), width),
	}
	for _, rule := range g.Rules {
		count := m.t.dim(lpad("0", 3))
		if rule.Failing > 0 {
			count = m.t.bad(lpad(fmt.Sprint(rule.Failing), 3))
		}
		text := wrapped(rule.Text, max(1, width-5))
		lines = append(lines, edge(text[0], count, width))
		lines = append(lines, text[1:]...)
	}
	var where []string
	for _, s := range g.Stages {
		if s.Failing > 0 {
			where = append(where, fmt.Sprintf("%s %d", s.Name, s.Failing))
		}
	}
	if len(where) > 0 {
		lines = append(lines, "")
		for i, line := range wrapped("In "+strings.Join(where, " · "), width) {
			if i == 0 {
				line = m.t.dim("In") + strings.TrimPrefix(line, "In")
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (m *healthModel) stageDetail(index, width int) []string {
	s := m.r.Stages[index]
	name := capitalise(s.Name)
	if m.tickets {
		lines := []string{m.t.bold(name) + m.t.dim(fmt.Sprintf("  %d out of policy", s.Failing)), ""}
		for _, a := range m.all {
			if a.Ticket.Error != "" || a.Ticket.State != s.Name || !a.Violation() {
				continue
			}
			var gates []string
			for _, g := range m.r.Gates {
				if a.FailsGate(g.Name) {
					gates = append(gates, human(g.Name))
				}
			}
			lines = append(lines, m.ticketRow(" ", "", a, strings.Join(gates, ", "), m.t.dim, width))
		}
		return lines
	}
	lines := []string{edge(m.t.bold(name), m.t.dim(quantity(s.Total, "ticket", "tickets")), width)}
	if s.Failing == 0 {
		return append(lines, m.t.good("All in policy"))
	}
	lines = append(lines, fmt.Sprintf("%d out of policy  ", s.Failing)+m.rateText(s.Failing, s.Total), "")
	lines = append(lines, edge(m.t.dim("Failing gates"), m.t.dim("tickets"), width))
	for _, g := range m.r.Gates {
		if n := g.Stages[index].Failing; n > 0 {
			lines = append(lines, edge(human(g.Name), m.t.bad(lpad(fmt.Sprint(n), 3)), width))
		}
	}
	return lines
}

func (m *healthModel) rateText(n, d int) string {
	if n > 0 {
		return m.t.bad(percent(n, d))
	}
	return m.t.good(percent(n, d))
}

// failsRule reports whether the ticket fails one rule of a gate on its path.
func failsRule(a workfile.Assessment, gate, rule string) bool {
	return slices.ContainsFunc(a.Earlier, func(c workfile.Check) bool {
		return c.Gate == gate && c.Requirement == rule && c.Outcome == "fail"
	})
}

// gateBoxes explains one gate: how often it fails, which rule fails, and where
// the failing tickets are.
func (v view) gateBoxes(w *workfile.Workspace, all []workfile.Assessment, gate string) {
	r := w.Rollup(all)
	width := v.innerWidth()
	var g *workfile.GateStat
	for i := range r.Gates {
		if r.Gates[i].Name == gate {
			g = &r.Gates[i]
		}
	}
	title := styled(upper(gate), "1")
	switch {
	case g == nil:
		v.block(muted, title, []row{styledRow("No ticket has to pass this gate yet.", muted)})
	case g.Failing == 0:
		v.block(green, title, []row{styledRow(fmt.Sprintf("✓ All %d tickets that must pass this gate do.", g.Required), green)})
	default:
		filled, rest := bar(g.Failing, g.Required, 24)
		rows := []row{
			leftRight(row{styled(fmt.Sprintf("%d of %d", g.Failing, g.Required), red), plain(" tickets that must pass this gate fail it")}, styled(percent(g.Failing, g.Required), red), width),
			{styled(filled, red), styled(rest, muted)},
			{},
		}
		textW, countW := 0, len(fmt.Sprint(g.Failing))
		for _, rule := range g.Rules {
			textW = max(textW, runewidth.StringWidth(rule.Text))
		}
		textW = min(textW, max(10, width-barWidth-countW-4))
		for _, rule := range g.Rules {
			f, rs := bar(rule.Failing, g.Required, barWidth)
			color := red
			if rule.Failing == 0 {
				color = muted
			}
			rows = append(rows, row{plain(pad(runewidth.Truncate(rule.Text, textW, "…"), textW) + "  "), styled(f, color), styled(rs, muted), styled("  "+lpad(fmt.Sprint(rule.Failing), countW), color)})
		}
		if len(g.Rules) > 1 {
			rows = append(rows, row{}, styledRow("A ticket can fail more than one rule.", muted))
		}
		v.block(red, title, rows)
		var stages []stat
		for i, s := range g.Stages {
			if s.Total > 0 {
				stages = append(stages, stat{fmt.Sprintf("%d. %s", i+1, s.Name), s.Failing, s.Total})
			}
		}
		v.block(red, styled("WHERE THEY ARE", "1"), v.rateRows(stages))
	}
}

func (v view) healthList(w *workfile.Workspace, all []workfile.Assessment, o options) {
	name := "health"
	if o.gate != "" {
		name += " · gate " + human(o.gate)
	}
	if o.stage != "" {
		name += " · stage " + o.stage
	}
	v.heading(name)
	if len(all) == 0 {
		v.wrap("No tickets match this selection.", "  ")
		v.line("")
		return
	}
	shown := slices.DeleteFunc(slices.Clone(all), func(a workfile.Assessment) bool { return !selected(a, o) })
	if o.gate != "" {
		v.gateBoxes(w, all, o.gate)
	} else {
		meta := quantity(len(all), "ticket", "tickets") + " checked"
		if o.limited() {
			meta += fmt.Sprintf(" · limit %d", o.limit)
		}
		v.wrap(meta, "  ")
		v.line("")
		if len(shown) == 0 {
			v.wrap("✓ No policy violations in the tickets checked.", "  ")
			v.line("")
			return
		}
	}
	v.stageBoxes(w, shown, func(a workfile.Assessment) []row {
		checks := a.Earlier
		if o.gate != "" {
			checks = slices.DeleteFunc(slices.Clone(checks), func(c workfile.Check) bool { return c.Gate != o.gate })
		}
		rows := []row{v.ticketHead("", "✗", red, a, cell{})}
		rows = append(rows, v.reasonRows(checks, "    ", o.gate == "")...)
		if a.Fallback != "" && o.gate == "" {
			rows = append(rows, textRows("First unmet gate on this policy path: "+a.Fallback+".", "    ", muted, "", v.innerWidth())...)
		}
		return rows
	})
}
