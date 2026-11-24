package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

const trackerInset = " "

const trackerHeaderRows = 2
const trackerFooterRows = 3 // blank line, divider and command hints

func (m *trackerModel) render() string {
	content := m.renderContent()
	if m.pickingProject || m.pickingPR {
		return m.renderProjectPicker(content)
	}
	return content
}

func (m *trackerModel) renderContent() string {
	divider := m.t.dim(strings.Repeat("─", m.width))
	lines := []string{m.navigation(), divider}
	var footer []string
	if m.shownView().inbox {
		footer = m.inbox.footer(m.t, m.width)
		lines = append(lines, m.inbox.content(m.t, m.width, len(footer))...)
		lines = fitTrackerRows(lines, m.height-(trackerFooterRows-1)-len(footer))
	} else if m.showingBoard() {
		height := max(0, m.height-trackerFooterRows)
		lines = append(lines, m.boardLines(height-len(lines))...)
		lines = fitTrackerRows(lines, height)
	} else {
		if !m.onTicket {
			lines = append(lines, "", m.columnHeadings())
			lines = append(lines, fitTrackerRows(m.listLines(), m.listRows())...)
			lines = append(lines, divider)
		}
		lines = append(lines, m.readerHeader...)
		lines = append(lines, m.previewLines()...)
	}
	if footer == nil {
		footer = []string{m.footer()}
	}
	lines = append(lines, "", divider)
	return strings.Join(append(lines, footer...), "\n")
}

// fitTrackerRows keeps the next section anchored when results fill fewer rows.
func fitTrackerRows(lines []string, height int) []string {
	if len(lines) >= height {
		return lines[:height]
	}
	return append(lines, make([]string, height-len(lines))...)
}

func (m *trackerModel) previewLines() []string {
	left, right := m.columns()
	if m.onTicket && m.readerWorkfile && right == 0 {
		return strings.Split(m.side.View(), "\n")
	}
	main := strings.Split(m.pane.View(), "\n")
	if right > 0 {
		divider := m.t.dim("│")
		side := strings.Split(m.side.View(), "\n")
		for i, line := range side {
			if i < len(main) {
				main[i] = m.t.onPanel(main[i], left) + divider + line
			}
		}
	}
	return main
}

// footer shows the keys for the current screen.
func (m *trackerModel) footer() string {
	room := max(0, m.width-2*len(ticketIndent))
	left := ansi.Truncate(m.help.ShortHelpView(m.helpKeys()), room, "")
	if m.notice != "" {
		left = ansi.Truncate(m.t.bad("! "+clean(m.notice)), room, "…")
	}
	return m.t.onPanel(ticketIndent+left, m.width)
}

func (m *trackerModel) listLines() []string {
	if message := m.resultMessage(); message != nil {
		return message
	}
	return strings.Split(m.list.View(), "\n")
}

func (m *trackerModel) resultMessage() []string {
	message := func(text string) []string { return fit(text, ticketIndent, ticketIndent, m.width-len(ticketIndent)) }
	switch {
	case !m.loaded:
		return message(m.t.dim("Reading your tickets…"))
	case len(m.all) == 0:
		return message(noTickets)
	case m.total == 0:
		return message(m.emptyView())
	case len(m.list.VisibleItems()) == 0:
		return message("No tickets match this search.")
	}
	return nil
}

// emptyView says why a view has nothing in it.
func (m *trackerModel) emptyView() string {
	view := m.shownView()
	switch {
	case view.me && m.findingMe:
		return "Finding your tickets…"
	case view.me && m.me == "":
		return cannotFindMe
	case view.attention() != "" && m.whoLoading:
		return "Identifying action owners…"
	case view.attention() != "":
		checked, total := m.checkProgress(view)
		unclear := ""
		if count := m.unclearChecks(view); count > 0 {
			unclear = fmt.Sprintf(" %d have unavailable evidence or unclear ownership.", count)
		}
		if checked < total {
			return fmt.Sprintf("No matching checked tickets. %d of %d checked; press n to check more.", checked, total) + unclear
		}
		return "No tickets match this attention view." + unclear
	case view.me && view.custom == nil:
		return "Nothing assigned to you."
	}
	return "Nothing in " + view.label() + "."
}

// trackerColumn is shared by headings and rows. The title gets spare room;
// narrow terminals drop assignee, then status, regardless of configured order.
type trackerColumn struct {
	name  string
	width int
}

func (m *trackerModel) rowColumns() []trackerColumn {
	names := m.e.tracker.Columns
	widths := map[string]int{
		"key":    min(max(3, m.keyWidth), max(3, m.width-12)),
		"status": max(8, m.statusWidth+2), "assignee": 16,
		"action": min(44, max(24, m.width/3)),
	}
	measure := func() int {
		room := m.width - 2*len(ticketIndent) - 2*(len(names)-1)
		for _, name := range names {
			room -= widths[name]
		}
		return room
	}
	for _, drop := range []struct {
		name    string
		minimum int
	}{{"assignee", 24}, {"status", 16}, {"action", 16}} {
		if slices.Contains(names, drop.name) && measure() < drop.minimum {
			names = slices.DeleteFunc(slices.Clone(names), func(name string) bool { return name == drop.name })
		}
	}
	widths["title"] = max(1, measure())
	var cols []trackerColumn
	for _, name := range names {
		cols = append(cols, trackerColumn{name, widths[name]})
	}
	return cols
}

func (m *trackerModel) columnHeadings() string {
	var labels []string
	for _, col := range m.rowColumns() {
		label := map[string]string{"key": "Key", "title": "Title", "status": "  Status", "assignee": "Assignee", "action": "Next action"}[col.name]
		labels = append(labels, pad(label, col.width))
	}
	return m.t.onPanel(m.t.bold(ticketIndent+strings.Join(labels, "  ")), m.width)
}

// row draws one ticket across the full width. The key leads, the title gets the
// room, and the status and assignee sit at the right edge. Narrow terminals drop
// the assignee, then the status, so the key and title stay readable. The current
// focused row is highlighted edge to edge; content sections remain unfilled.
func (m *trackerModel) row(item ticketItem, selected bool, matches []int) string {
	base := lipgloss.NewStyle()
	bar := selected && !m.onTicket
	// With a card background the bar is that grey fill, so colours stay
	// readable. Without one, fall back to reverse video.
	fill := bar && m.t.color && m.boardSelectedTheme.bg != ""
	if fill {
		base = base.Bold(true)
	} else if bar {
		base = base.Bold(true).Reverse(true)
	} else if selected {
		base = base.Faint(true)
	}
	soft := base
	if !bar || fill {
		soft = soft.Faint(true)
	}
	hits := map[int]bool{}
	for _, i := range matches {
		hits[i] = true
	}
	cursor := ticketIndent
	if selected {
		cursor = "▸ "
	}
	line := m.paint(cursor, 0, nil, base)
	for i, col := range m.rowColumns() {
		if i > 0 {
			line += m.paint("  ", 0, nil, base)
		}
		text, style, offset := "", base, 0
		highlights := hits
		room := col.width
		switch col.name {
		case "key":
			text, style = item.Key, base.Bold(true)
		case "title":
			text, offset = item.Title, len([]rune(item.Key))+1
		case "status":
			icon, signalStyle := m.statusSignal(item, soft)
			// Keep the focus bar solid. The icon preserves meaning without
			// reversing semantic colours into coloured background patches.
			if bar && !fill {
				signalStyle = base
			}
			line += m.paint(icon+" ", 0, nil, signalStyle)
			room -= 2
			text, style = item.Status, signalStyle
			offset = len([]rune(item.Key + " " + item.Title + " " + item.Assignee + " "))
		case "action":
			result := m.nextActions(item.Key)
			text, highlights = trackerActionText(item.Key, result, m.t.color), nil
			style = soft
			if !bar || fill {
				style = attentionStyle(result.State, soft)
			}
		case "assignee":
			text, style = assigneeName(item.Listing), soft
			if item.Assignee == "" {
				highlights = nil
			}
			offset = len([]rune(item.Key + " " + item.Title + " "))
		}
		text = runewidth.Truncate(clean(text), room, "…")
		line += m.paint(text, offset, highlights, style)
		line += m.paint(strings.Repeat(" ", max(0, room-runewidth.StringWidth(text))), 0, nil, base)
	}
	line += m.paint(strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line))), 0, nil, base)
	if !m.t.color {
		return strings.TrimRight(line, " ")
	}
	if fill {
		return m.boardSelectedTheme.onPanel(line, m.width)
	}
	return line
}

// statusSignal annotates the tracker stage, never inferring readiness from its
// name. Unread tickets stay neutral. Cached assessments need no extra reads.
func (m *trackerModel) statusSignal(item ticketItem, neutral lipgloss.Style) (string, lipgloss.Style) {
	bad := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(1))
	good := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(2))
	if r := m.ticketReads.byKey[item.Key]; r != nil {
		if m.overview && r.policyDone() {
			state := m.nextActions(item.Key).State
			return trackerAttentionIcon(state, m.t.color), attentionStyle(state, neutral)
		}
		switch {
		case r.policyErr != nil:
			return trackerAttentionIcon(workfile.AttentionUnknown, m.t.color), attentionStyle(workfile.AttentionUnknown, neutral)
		case r.policy != nil:
			switch r.policy.Standing() {
			case workfile.CannotCheck:
				return trackerAttentionIcon(workfile.AttentionUnknown, m.t.color), attentionStyle(workfile.AttentionUnknown, neutral)
			case workfile.OutOfPolicy:
				return setupIcon(m.t.color, "‼", "󰀦"), bad
			case workfile.Blocked:
				return trackerAttentionIcon(workfile.NeedsMe, m.t.color), bad
			case workfile.NeedsWork:
				return trackerAttentionIcon(workfile.ReadyToMove, m.t.color), good
			case workfile.Done:
				return "·", neutral
			}
		}
	}
	if m.e.w.Policy.Transitions[item.State].End {
		return "·", neutral
	}
	return "→", neutral
}

// paint styles text, underlining the characters a search matched. offset is
// where text starts in the words the search looked at.
func (m *trackerModel) paint(text string, offset int, hits map[int]bool, st lipgloss.Style) string {
	if !m.t.color {
		return text
	}
	var b strings.Builder
	var run []rune
	hit := false
	flush := func() {
		if len(run) == 0 {
			return
		}
		style := st
		if hit {
			style = style.Underline(true)
		}
		b.WriteString(style.Render(string(run)))
		run = run[:0]
	}
	for i, r := range []rune(text) {
		if now := hits[offset+i]; now != hit {
			flush()
			hit = now
		}
		run = append(run, r)
	}
	flush()
	return b.String()
}

// textWidth is the room for text inside the ticket's panels.
func (m *trackerModel) textWidth() int {
	left, _ := m.columns()
	return trackerPaneWidth(left)
}

// imageWidth leaves room for an image inside a comment or a reply.
func (m *trackerModel) imageWidth() int {
	_, width := m.previewRenderer().commentSize(1)
	return max(1, width)
}

// fit wraps styled text to width, breaking long words, and indents it.
func fit(text, first, rest string, width int) []string {
	lines := strings.Split(ansi.Wrap(text, max(4, width-runewidth.StringWidth(rest)), ""), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = rest + lines[i]
		}
	}
	return lines
}

// ago says when something happened in plain words, or nothing if it is not known.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	days := int(now.Sub(t).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 30:
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("2 Jan 2006")
}
