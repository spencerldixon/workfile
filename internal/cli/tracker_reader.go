package cli

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *trackerModel) setReading(reading bool) {
	if m.onTicket == reading {
		return
	}
	m.onTicket, m.readerWorkfile = reading, false
	m.fill(true)
}

func (m *trackerModel) fill(top bool) {
	heading, body, policy := m.ticketContent()
	left, right := m.columns()
	height := max(1, m.height-m.topRows()-m.listRows()-1-trackerFooterRows)
	m.readerHeader = nil

	var lines []string
	switch {
	case m.onTicket:
		height = max(1, m.height-trackerHeaderRows-trackerFooterRows)
		header := ticketLines([]ticketBlock{heading}, left, ticketIndent)
		if right == 0 && len(header) > 0 {
			header = append(header, m.t.dim(strings.Repeat("─", left)))
		}
		// On very short terminals, leave room to scroll through the rest of
		// the heading as part of the ticket.
		pinned := min(len(header), max(0, height-2))
		m.readerHeader = header[:pinned]
		height -= pinned
		lines = append(header[pinned:], ticketLines(body, left, trackerInset)...)
		if right == 0 {
			right = left
		}
	case right == 0:
		lines = ticketLines(append([]ticketBlock{heading}, policy...), left, trackerInset)
		if len(policy) > 0 && len(body) > 0 {
			lines = append(lines, m.t.dim(strings.Repeat("─", left)))
		}
		lines = append(lines, ticketLines(body, left, trackerInset)...)
		policy = nil
	default:
		lines = ticketLines(append([]ticketBlock{heading}, body...), left, trackerInset)
	}

	m.pane.SetWidth(left)
	m.pane.SetHeight(height)
	m.pane.SetContentLines(lines)
	m.side.SetWidth(right)
	m.side.SetHeight(height)
	m.side.SetContentLines(ticketLines(policy, right, trackerInset))
	if top {
		m.pane.GotoTop()
		m.side.GotoTop()
	}
}

// Downward reading continues into Workfile once the ticket ends. Scrolling
// back past the start of Workfile returns to the ticket in a split view.
func (m *trackerModel) readerKey(msg tea.KeyPressMsg) tea.Cmd {
	if _, right := m.columns(); right > 0 {
		keys := m.pane.KeyMap
		switch {
		case !m.readerWorkfile && m.pane.AtBottom() && !m.side.AtBottom() && key.Matches(msg, keys.Down, keys.PageDown, keys.HalfPageDown):
			m.readerWorkfile = true
		case m.readerWorkfile && m.side.AtTop() && !m.pane.AtTop() && key.Matches(msg, keys.Up, keys.PageUp, keys.HalfPageUp):
			m.readerWorkfile = false
		}
	}
	return m.scrollPane(msg, m.readerWorkfile)
}

func (m *trackerModel) scrollPane(msg tea.Msg, workfile bool) tea.Cmd {
	pane := &m.pane
	if workfile {
		pane = &m.side
	}
	var cmd tea.Cmd
	*pane, cmd = pane.Update(msg)
	return cmd
}

func (m *trackerModel) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	var move tea.KeyPressMsg
	switch msg.Button {
	case tea.MouseWheelUp:
		move.Code = tea.KeyUp
	case tea.MouseWheelDown:
		move.Code = tea.KeyDown
	default:
		return nil
	}
	if m.pickingProject || m.pickingPR {
		w, h := m.projectModalSize()
		x, y := (m.width-w)/2, (m.height-h)/2
		if msg.X >= x && msg.X < x+w && msg.Y >= y && msg.Y < y+h {
			return m.updateProjectPicker(move)
		}
		return nil
	}
	if m.showingBoard() || msg.Y >= m.height-trackerFooterRows {
		return nil
	}
	top := trackerHeaderRows + len(m.readerHeader)
	if !m.onTicket {
		start := m.topRows()
		end := start + m.listRows()
		if msg.Y >= start && msg.Y < end {
			return m.updateList(move)
		}
		top = end + 1
	}
	if msg.Y < top {
		return nil
	}
	left, right := m.columns()
	if right > 0 && msg.X == left {
		return nil
	}
	workfile := msg.X > left && right > 0 || right == 0 && m.onTicket && m.readerWorkfile
	if m.onTicket {
		m.readerWorkfile = workfile
	}
	return m.scrollPane(msg, workfile)
}
