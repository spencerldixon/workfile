package cli

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// tab draws one view's name. The current one stands out by more than colour.
// The Assigned to me count is left out until it is known who "me" is.
func (m *trackerModel) tab(view ticketView, counts bool) string {
	label := view.label()
	if view.inbox && m.inbox.data != nil {
		label += fmt.Sprintf(" (%d)", len(m.inbox.rows))
	} else if !view.inbox && counts && (!view.me || m.me != "") {
		pending := ""
		if view.attention() != "" {
			if checked, total := m.checkProgress(view); checked < total || m.whoLoading {
				pending = "+"
			}
		}
		label += fmt.Sprintf(" (%d%s)", m.counts[view], pending)
		// Reserve the pending marker's cell when the count settles.
		if view.attention() != "" && pending == "" {
			label += " "
		}
	}
	if view != m.shownView() {
		return ticketIndent + m.t.dim(label)
	}
	if !m.t.color {
		return "▸ " + label
	}
	return ticketIndent + m.t.accent(label)
}

// tabs keeps the active count when the other counts no longer fit. At the
// narrowest widths the active view's name takes priority.
func (m *trackerModel) tabs(room int) string {
	views := m.views()
	tabs := make([]string, len(views))
	for i, view := range views {
		tabs[i] = m.tab(view, m.loaded)
	}
	if line := strings.Join(tabs, " "); ansi.StringWidth(line) <= room {
		return line
	}
	current := tabs[m.view]
	for i, view := range views {
		if i != m.view {
			tabs[i] = m.tab(view, false)
		}
	}
	if line := strings.Join(tabs, " "); ansi.StringWidth(line) <= room {
		return line
	}
	tabs[m.view] = m.tab(views[m.view], false)
	if line := strings.Join(tabs, " "); ansi.StringWidth(line) <= room {
		return line
	}
	if ansi.StringWidth(current) <= room {
		return current
	}
	return ansi.Truncate(tabs[m.view], max(0, room), "…")
}

// navigation draws the same view strip above the board, list and reader.
func (m *trackerModel) navigation() string {
	query := ""
	switch m.list.FilterState() {
	case list.Filtering:
		query = "/ " + clean(m.list.FilterValue()) + "█"
	case list.FilterApplied:
		query = "/ " + clean(m.list.FilterValue())
	}
	margin := len(ticketIndent)
	inner := m.width - margin
	searchRoom := 0
	if query != "" {
		searchRoom = min(runewidth.StringWidth(query)+2, inner/3)
	}
	syncRoom := max(0, inner-ansi.StringWidth(m.tab(m.shownView(), false))-searchRoom-3)
	spinner := "  "
	if m.loaded && !m.refresh.syncing && m.busy() {
		spinner = m.spin.View() + " "
	}
	// Reserve the board clock's width on list views too, so switching views
	// keeps the same labels and counts visible.
	hasBoard := slices.ContainsFunc(m.views(), ticketView.board)
	clock := m.syncCaption(syncRoom, hasBoard)
	if m.shownView().inbox {
		switch {
		case m.inbox.loading:
			clock = "Syncing…"
		case m.inbox.data != nil:
			clock = "Synced " + m.inbox.data.at.Format("15:04:05")
		}
	}
	tabs := m.tabs(inner - ansi.StringWidth(spinner+clock) - searchRoom - 1)
	if hasBoard && !m.shownView().board() && !m.shownView().inbox {
		clock = m.syncCaption(syncRoom, false)
	}
	right := spinner + ansi.Truncate(clock, syncRoom, "…")
	line := tabs
	used := ansi.StringWidth(tabs)
	if query != "" {
		room := m.width - margin - used - ansi.StringWidth(right) - 3
		query = runewidth.Truncate(query, max(0, room), "…")
		line += "  " + query
		used += 2 + runewidth.StringWidth(query)
	}
	gap := m.width - margin - used - ansi.StringWidth(right)
	return m.t.onPanel(ansi.Truncate(line+strings.Repeat(" ", max(1, gap))+m.t.dim(right)+ticketIndent, m.width, ""), m.width)
}
