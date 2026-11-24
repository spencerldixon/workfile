package cli

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func inboxSymbol(k workfile.RowKind) string {
	switch k {
	case workfile.RowChangesRequested, workfile.RowChecksFailed, workfile.RowNeedsWork:
		return "✗"
	case workfile.RowOutOfPolicy:
		return "‼"
	case workfile.RowReviewRequested, workfile.RowComments:
		return "●"
	case workfile.RowReady, workfile.RowChecksPassed:
		return "✓"
	case workfile.RowCannotCheck:
		return "!"
	}
	return "○"
}

// paintSymbol uses the same red, yellow and green signals as ticket detail.
func paintSymbol(t theme, k workfile.RowKind) string {
	symbol := inboxSymbol(k)
	switch k {
	case workfile.RowOutOfPolicy:
		return t.bold(t.bad(setupIcon(t.color, symbol, "󰀦")))
	case workfile.RowChangesRequested:
		return t.bad(setupIcon(t.color, symbol, "󰅖"))
	case workfile.RowChecksFailed:
		return t.bad(setupIcon(t.color, symbol, "󰙨"))
	case workfile.RowNeedsWork:
		return t.bad(setupIcon(t.color, symbol, "󰏫"))
	case workfile.RowReviewRequested:
		return t.warn(setupIcon(t.color, symbol, ""))
	case workfile.RowComments:
		return t.warn(setupIcon(t.color, symbol, "󰆉"))
	case workfile.RowReady, workfile.RowChecksPassed:
		return t.good(setupIcon(t.color, symbol, "󰄬"))
	case workfile.RowCannotCheck:
		return t.warn(setupIcon(t.color, symbol, "󰋼"))
	default:
		return t.dim(setupIcon(t.color, symbol, "󰋽"))
	}
}

func (m *inboxModel) theme() theme { return newTheme(m.e.view.color, "") }

func (m *inboxModel) renderWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

func (m *inboxModel) bodyHeight(footerLines int) int {
	return max(1, m.height-trackerHeaderRows-(trackerFooterRows-1)-footerLines)
}

func (m *inboxModel) content(t theme, width, footerLines int) []string {
	if m.ticket != nil {
		return append(append([]string{}, m.ticket.heading...), strings.Split(m.ticket.body.View(), "\n")...)
	}
	room := m.bodyHeight(footerLines)
	toast := ansi.Truncate(m.toastLine(t, width), width, "…")
	if toast != "" {
		room = max(0, room-1)
	}
	var body []string
	switch {
	case m.data == nil:
		body = []string{ticketIndent + t.dim("Reading your work…")}
	case len(m.rows) == 0:
		body = []string{ticketIndent + t.dim("No alerts.")}
	case m.detail:
		body = strings.Split(m.activityViewport(t, width, room).View(), "\n")
	default:
		body = m.list(t, width, room)
	}
	if toast != "" {
		body = append(fitTrackerRows(body, room), toast)
	}
	return body
}

func (m *inboxModel) activityViewport(t theme, width, room int) viewport.Model {
	lines := m.preview(t, width)
	pane := viewport.New(viewport.WithWidth(width), viewport.WithHeight(min(room, len(lines))))
	pane.SetContentLines(lines)
	pane.SetYOffset(m.detailOffset)
	return pane
}

func (m *inboxModel) list(t theme, width, height int) []string {
	var lines []string
	selected := 0
	selectedHeight := 1
	keyWidth := 0
	for _, r := range m.rows {
		keyWidth = max(keyWidth, lipgloss.Width(inboxKey(r)))
	}
	keyWidth = min(keyWidth, width/3)
	for i, r := range m.rows {
		row := m.row(t, r, i == m.sel, width, keyWidth)
		if i == m.sel {
			selected, selectedHeight = len(lines), len(row)
		}
		lines = append(lines, row...)
	}
	start := max(0, selected+min(height, selectedHeight)-height)
	return lines[start:min(len(lines), start+height)]
}

func inboxKey(r workfile.InboxRow) string {
	if strings.HasPrefix(r.ID, "!") {
		return ""
	}
	return clean(firstOf(r.Ticket, r.PR))
}

func (m *inboxModel) row(t theme, r workfile.InboxRow, selected bool, width, keyWidth int) []string {
	id := ansi.Truncate(inboxKey(r), keyWidth, "…")
	lead := "  "
	if selected {
		lead = t.bold("▸") + " "
	}
	mark := paintSymbol(t, r.Kind)
	key := t.bold(m.e.view.link(id, firstOf(r.URL, r.PRURL)))
	left := lead + mark + " " + key
	age := shortAge(r.Latest(), m.now())
	text := clean(r.Text)
	paint := t.dim
	if r.Action() {
		paint = func(s string) string { return s }
	}
	var lines []string
	// On narrow screens keep the reason readable on its own line.
	if width < 55 && id != "" && lipgloss.Width(left)+4+lipgloss.Width(text)+len(age) > width {
		if age != "" {
			left += strings.Repeat(" ", max(2, width-lipgloss.Width(left)-len(age))) + t.dim(age)
		}
		lines = append([]string{left}, fit(paint(text), "    ", "    ", width)...)
	} else {
		if keyWidth > 0 {
			left += strings.Repeat(" ", keyWidth-lipgloss.Width(id)+2)
		}
		room := width - lipgloss.Width(left)
		if age != "" && room > len(age)+4 {
			room -= len(age) + 2
		} else {
			age = ""
		}
		left += paint(ansi.Truncate(text, max(0, room), "…"))
		if age != "" {
			left += strings.Repeat(" ", max(2, width-lipgloss.Width(left)-len(age))) + t.dim(age)
		}
		lines = []string{left}
	}
	if selected && t.color {
		panel := newTheme(true, m.e.view.selectedLaneBackground())
		for i, line := range lines {
			lines[i] = panel.onPanel(line, width)
		}
	}
	return lines
}

// preview puts the next action above the evidence for it.
func (m *inboxModel) preview(t theme, width int) []string {
	r, ok := m.current()
	if !ok {
		return nil
	}
	panel := newTheme(t.color, m.e.view.laneBackground())
	inner := max(10, width-4)
	id := inboxKey(r)
	head := t.bold(m.e.view.link(id, r.URL))
	if pr := clean(r.PR); pr != "" && pr != id {
		head += "  " + m.e.view.link(pr, r.PRURL)
	} else if pr == id && id != "" && r.Ticket == "" {
		head = t.bold(m.e.view.link(id, r.PRURL))
	}
	if r.Stage != "" {
		head += "  " + t.dim(clean(r.Stage))
	}
	var lines []string
	if head != "" {
		lines = append(lines, ticketIndent+head)
	}
	if title := clean(r.Title); title != "" {
		lines = append(lines, fit(title, ticketIndent, ticketIndent, max(1, width))...)
	}
	lines = append(lines, fit(clean(r.Text), ticketIndent+paintSymbol(t, r.Kind)+" ", "    ", width)...)
	lines = append(lines, "")
	section := func(title, tag string, body []string) {
		if len(body) == 0 {
			return
		}
		heading := t.bold(title)
		if tag != "" {
			heading += strings.Repeat(" ", max(2, inner-lipgloss.Width(title)-lipgloss.Width(tag))) + t.dim(tag)
		}
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		content := strings.Join(append([]string{heading}, body...), "\n")
		for _, l := range strings.Split(panel.panel(content, width), "\n") {
			lines = append(lines, ansi.Truncate(l, width, ""))
		}
	}
	var next []string
	for i, step := range m.nextSteps(r) {
		next = append(next, fit(fmt.Sprintf("%d. %s", i+1, clean(step)), "", "   ", inner)...)
	}
	section("Next step", "", next)
	var news []string
	shown := r.Events
	if len(shown) > 4 {
		shown = shown[len(shown)-4:]
	}
	for i := len(shown) - 1; i >= 0; i-- {
		e := shown[i]
		who := clean(e.Who)
		if who == "" {
			who = clean(m.data.names[e.WhoID])
		}
		if who == "" {
			who = clean(e.WhoID)
		}
		where := clean(firstOf(e.Where, prLabelOf(e.PR), e.Ticket))
		meta := t.bold(who) + "  " + t.dim(where)
		if age := shortAge(e.At, m.now()); age != "" {
			meta += t.dim(" · " + age)
		}
		news = append(news, fit(meta, "", "", inner)...)
		if text := strings.Join(strings.Fields(clean(e.Text)), " "); text != "" {
			news = append(news, fit(text, "", "", inner)...)
		}
	}
	tag := ""
	if r.New > 0 {
		tag = fmt.Sprintf("%d new", r.New)
	}
	section("Latest activity", tag, news)

	if note := m.agentNote(r.ID); note != nil {
		lines = append(lines, "", ticketIndent+t.render("⚙ agent on it · "+shortAge(note.At, m.now()), func(st lipgloss.Style) lipgloss.Style { return st.Foreground(lipgloss.ANSIColor(6)) }))
	}
	return lines
}

// nextSteps are the person's own steps for the row's ticket, or the row
// itself worded as an action.
func (m *inboxModel) nextSteps(r workfile.InboxRow) []string {
	switch r.Kind {
	case workfile.RowInfo:
		return nil
	case workfile.RowComments:
		return []string{fmt.Sprintf("Answer %d %s on %s", r.Count, plural(r.Count, "comment", "comments"), r.PR)}
	case workfile.RowReviewRequested:
		return []string{"Review " + r.PR}
	case workfile.RowChangesRequested:
		return []string{fmt.Sprintf("Address %s's review on %s", r.Who, r.PR)}
	case workfile.RowChecksFailed:
		return []string{"Fix the failing checks on " + r.PR}
	case workfile.RowCannotCheck:
		return []string{"Check your connection, then press r"}
	}

	var steps []string
	for _, w := range m.data.work {
		if w.Listing.Key != r.Ticket || !w.Mine {
			continue
		}
		for _, a := range w.Next.Actions {
			if a.State == workfile.NeedsMe || a.State == workfile.ReadyToMove {
				steps = append(steps, a.Text)
			}
		}
	}
	if len(steps) == 0 {
		steps = append(steps, r.Text)
	}
	return steps
}

func (m *inboxModel) agentNote(id string) *workfile.AgentNote {
	for i := len(m.st.Agents) - 1; i >= 0; i-- {
		if m.st.Agents[i].Row == id {
			return &m.st.Agents[i]
		}
	}
	return nil
}

func (m *inboxModel) toastLine(t theme, width int) string {
	if m.toast == "" {
		return ""
	}
	text := ansi.Truncate(clean(m.toast), max(0, width-len(ticketIndent)), "…")
	if m.toastBad {
		return ticketIndent + t.bad(text)
	}
	return ticketIndent + t.dim(text)
}

func (m *inboxModel) footer(t theme, width int) []string {
	if m.ticket != nil {
		return m.ticket.footer
	}
	keys := m.footerKeys()
	if _, ok := m.current(); ok {
		if m.detail {
			keys = append([][2]string{{"esc", "list"}, {"pgup/dn", "scroll"}}, keys...)
		} else {
			keys = append([][2]string{{"enter", "details"}}, keys...)
		}
	}
	var lines []string
	line := ticketIndent
	for _, k := range keys {
		part := ansi.Truncate(t.bold(k[0])+" "+t.dim(k[1]), max(0, width-len(ticketIndent)), "…")
		if line == ticketIndent {
			line += part
		} else if lipgloss.Width(line)+3+lipgloss.Width(part) <= width {
			line += t.dim(" · ") + part
		} else {
			lines = append(lines, line)
			line = ticketIndent + part
		}
	}
	return append(lines, line)
}

func prLabelOf(id string) string {
	if i := strings.LastIndex(id, "#"); i >= 0 {
		return id[i:]
	}
	return id
}

// shortAge is "2m", "3h" or "4d".
func shortAge(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
