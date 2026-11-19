package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

const trackerHeaderRows = 3

func (m *trackerModel) render() string {
	content := m.renderContent()
	if m.pickingProject || m.pickingPR {
		return m.renderProjectPicker(content)
	}
	return content
}

func (m *trackerModel) renderContent() string {
	lines := []string{m.masthead(), "", m.header()}
	if m.showingBoard() {
		height := max(0, m.height-1)
		lines = append(lines, m.boardLines(height-len(lines))...)
		lines = fitTrackerRows(lines, height)
	} else {
		if !m.onTicket && !m.shownView().board() {
			lines = append(lines, "", m.columnHeadings())
			lines = append(lines, fitTrackerRows(m.listLines(), m.listRows())...)
		}
		lines = append(lines, "", m.previewHeading())
		lines = append(lines, m.readerHeader...)
		lines = append(lines, m.previewLines()...)
	}
	return strings.Join(append(lines, m.footer()), "\n")
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
		side := strings.Split(m.side.View(), "\n")
		for i, line := range side {
			if i < len(main) {
				main[i] = m.t.onPanel(main[i], left) + " " + line
			}
		}
	}
	return main
}

// masthead keeps the app and scope on one quiet line, above the views.
func (m *trackerModel) masthead() string {
	label := m.t.bold("workfile") + m.t.dim(" / tracker")
	room := m.width - 2*len(ticketIndent) - ansi.StringWidth(label) - 3
	scope := scopeText(m.e.w, max(0, room))
	if project := m.projectOrigins[m.shownView()]; project != "" {
		scope = ansi.Truncate(clean(project), max(0, room), "…")
	}
	if room < 4 {
		scope = ""
	}
	return m.bar(label, m.t.dim(scope), m.width)
}

// bar aligns a label and secondary text with the ticket panels' inset.
func (m *trackerModel) bar(left, right string, width int) string {
	inner := max(0, width-2*len(ticketIndent))
	return ticketIndent + alignPair(left, right, inner) + ticketIndent
}

// previewHeading stays outside the scrolling content, so focus is always clear.
func (m *trackerModel) previewHeading() string {
	label, hint := "Ticket preview", "enter to read"
	if m.onTicket {
		label, hint = "Ticket · reading", "tab to list"
		if m.shownView().board() {
			hint = "esc to board"
		}
	}
	if _, ok := m.selected(); !ok {
		hint = ""
	}
	left, right := m.columns()
	if m.onTicket && m.readerWorkfile && right == 0 {
		label, hint = "Workfile · reading", "w ticket"
	}
	if runewidth.StringWidth(label)+runewidth.StringWidth(hint)+4 > left-2*len(ticketIndent) {
		hint = ""
	}
	if right == 0 {
		return m.bar(m.highlight(label), m.t.dim(hint), left)
	}
	workfile, workfileHint := "Workfile", ""
	if m.onTicket && m.readerWorkfile {
		workfile, workfileHint = "Workfile · reading", "w ticket"
		label, hint = "Ticket", ""
	}
	return m.bar(m.highlight(label), m.t.dim(hint), left) + " " +
		m.bar(m.highlight(workfile), m.t.dim(workfileHint), right)
}

// highlight is a small title chip, not a background for the section's content.
func (m *trackerModel) highlight(label string) string {
	if !m.t.color {
		return label
	}
	return m.t.render(" "+label+" ", func(st lipgloss.Style) lipgloss.Style {
		return st.Bold(true).Reverse(true)
	})
}

// tab draws one view's name. The current one stands out by more than colour.
// The Assigned to me count is left out until it is known who "me" is.
func (m *trackerModel) tab(view ticketView, counts bool) string {
	label := view.label()
	if counts && (!view.me || m.me != "") {
		label += fmt.Sprintf(" %d", m.counts[view])
		if view.attention() != "" {
			// The "+" slot is kept as a space once checks finish, so the
			// tabs after it do not move as tickets are read.
			mark := " "
			if checked, total := m.checkProgress(view); checked < total || m.whoLoading {
				mark = "+"
			}
			label += mark
		}
	}
	switch {
	case view != m.shownView():
		return " " + m.t.dim(label) + " "
	case m.t.color && m.tabFocus:
		// Lip Gloss styles underlined spaces separately and drops reverse video.
		return ansi.Style{}.Bold().Underline(true).Reverse(true).Styled(" " + label + " ")
	case m.t.color:
		// highlight pads one space each side, the same room as the other tabs,
		// so the strip does not shift as the current view changes.
		return m.highlight(label)
	}
	return "[" + label + "]"
}

// tabs fits the views into room. Narrow terminals drop the counts first, then
// the other views, so the current one is always named.
func (m *trackerModel) tabs(room int) string {
	row := func(counts bool) string {
		var tabs []string
		for _, view := range m.views() {
			tabs = append(tabs, m.tab(view, counts))
		}
		return strings.Join(tabs, " ")
	}
	if line := row(m.loaded); ansi.StringWidth(line) <= room {
		return line
	}
	if line := row(false); ansi.StringWidth(line) <= room {
		return line
	}
	return ansi.Truncate(m.tab(m.shownView(), false), max(0, room), "…")
}

// header shows the view tabs, ticket search and sync time.
func (m *trackerModel) header() string {
	query := ""
	switch m.list.FilterState() {
	case list.Filtering:
		query = "/ " + clean(m.list.FilterValue()) + "█"
	case list.FilterApplied:
		query = "/ " + clean(m.list.FilterValue())
	}
	margin := len(ticketIndent)
	inner := m.width - 2*margin
	searchRoom := 0
	if query != "" {
		searchRoom = min(runewidth.StringWidth(query)+2, inner/3)
	}
	syncRoom := max(0, inner-ansi.StringWidth(m.tab(m.shownView(), false))-searchRoom-3)
	spinner := "  "
	if m.loaded && !m.refresh.syncing && m.busy() {
		spinner = m.spin.View() + " "
	}
	right := spinner + ansi.Truncate(m.syncCaption(syncRoom), syncRoom, "…")
	tabs := m.tabs(inner - ansi.StringWidth(right) - searchRoom - 1)
	line := ticketIndent + tabs
	used := margin + ansi.StringWidth(tabs)
	if query != "" {
		room := m.width - margin - used - ansi.StringWidth(right) - 3
		query = runewidth.Truncate(query, max(0, room), "…")
		line += "  " + query
		used += 2 + runewidth.StringWidth(query)
	}
	gap := m.width - margin - used - ansi.StringWidth(right)
	return m.t.onPanel(ansi.Truncate(line+strings.Repeat(" ", max(1, gap))+m.t.dim(right)+ticketIndent, m.width, ""), m.width)
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
			text, highlights = trackerActionText(item.Key, result), nil
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
			return attentionIcon(state), attentionStyle(state, neutral)
		}
		switch {
		case r.policyErr != nil:
			return "!", bad
		case r.policy != nil:
			switch r.policy.Standing() {
			case workfile.CannotCheck:
				return "!", bad
			case workfile.OutOfPolicy:
				return "‼", bad
			case workfile.Blocked:
				return "✗", bad
			case workfile.NeedsWork:
				return "✓", good
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
	return previewTextWidth(left)
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
