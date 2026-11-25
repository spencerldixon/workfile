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
	icon, _ := inboxSignal(k, false)
	return icon
}

func inboxSignal(k workfile.RowKind, colour bool) (string, workfile.AttentionState) {
	var state workfile.AttentionState
	switch k {
	case workfile.RowChangesRequested, workfile.RowChecksFailed, workfile.RowNeedsWork:
		state = workfile.NeedsMe
	case workfile.RowOutOfPolicy:
		return outOfPolicyIcon(colour), workfile.NeedsMe
	case workfile.RowReviewRequested:
		return reviewIcon(colour), workfile.Waiting
	case workfile.RowComments:
		return terminalIcon(colour, "!", "󰍩"), workfile.Waiting
	case workfile.RowReady, workfile.RowChecksPassed:
		state = workfile.ReadyToMove
	case workfile.RowCannotCheck:
		state = workfile.AttentionUnknown
	default:
		state = workfile.AttentionDone
	}
	return attentionIcon(state, colour), state
}

func paintInboxSignal(t theme, k workfile.RowKind) string {
	icon, state := inboxSignal(k, t.color)
	return t.render(icon, func(st lipgloss.Style) lipgloss.Style {
		return attentionStyle(state, st.Faint(true)).Bold(k == workfile.RowOutOfPolicy)
	})
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
	case m.agentPreview != nil:
		lines := []string{ticketIndent + t.bold("Agent prompt preview"), ""}
		for _, line := range strings.Split(m.agentPreview.prompt, "\n") {
			lines = append(lines, fit(clean(line), ticketIndent, ticketIndent, width)...)
		}
		pane := viewport.New(viewport.WithWidth(width), viewport.WithHeight(min(room, len(lines))))
		pane.SetContentLines(lines)
		pane.SetYOffset(m.detailOffset)
		body = strings.Split(pane.View(), "\n")
	default:
		body = m.list(t, width, room)
	}
	if toast != "" {
		body = append(fitTrackerRows(body, room), toast)
	}
	return body
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
	if r.Ticket != "" && r.PR != "" {
		return clean(r.Ticket + " · " + r.PR)
	}
	return clean(firstOf(r.Ticket, r.PR))
}

func (m *inboxModel) row(t theme, r workfile.InboxRow, selected bool, width, keyWidth int) []string {
	id := ansi.Truncate(inboxKey(r), keyWidth, "…")
	lead := "  "
	if selected {
		lead = t.bold("▸") + " "
	}
	mark := paintInboxSignal(t, r.Kind)
	key := t.bold(m.e.view.link(id, firstOf(r.URL, r.PRURL)))
	left := lead + mark + " " + key
	age := shortAge(r.Latest(), m.now())
	title := clean(r.Title)
	reason := clean(r.Text)
	if r.ReadyReason != "" {
		reason += " · " + clean(r.ReadyReason)
	}
	if title == "" {
		title, reason = reason, ""
	}
	if keyWidth > 0 {
		left += strings.Repeat(" ", max(0, keyWidth-lipgloss.Width(id))+2)
	}
	lines := []string{alignPair(left+title, t.dim(age), width)}
	if reason != "" {
		indent := strings.Repeat(" ", min(keyWidth+6, max(0, width/2)))
		lines = append(lines, fit(t.dim(reason), indent, indent, width)...)
	}
	if selected && t.color {
		panel := newTheme(true, m.e.view.selectedLaneBackground())
		for i, line := range lines {
			lines[i] = panel.onPanel(line, width)
		}
	}
	return lines
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
	if m.agentPreview != nil {
		keys = [][2]string{{"enter", "launch"}, {"esc", "cancel"}, {"↑↓", "scroll"}}
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
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
