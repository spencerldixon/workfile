package cli

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
	height := max(1, m.height-m.topRows()-m.listRows()-3)
	m.readerHeader = nil

	var lines []string
	switch {
	case m.onTicket:
		height = max(1, m.height-trackerHeaderRows-3)
		header := ticketLines([]ticketBlock{heading}, left)
		// On very short terminals, leave room to scroll through the rest of
		// the heading as part of the ticket.
		pinned := min(len(header), max(0, height-2))
		m.readerHeader = header[:pinned]
		height -= pinned
		lines = append(header[pinned:], ticketLines(body, left)...)
		if right == 0 {
			right = left
		}
	case right == 0:
		lines = ticketLines(append(append([]ticketBlock{heading}, policy...), body...), left)
		policy = nil
	default:
		lines = ticketLines(append([]ticketBlock{heading}, body...), left)
	}

	m.pane.SetWidth(left)
	m.pane.SetHeight(height)
	m.pane.SetContentLines(lines)
	m.side.SetWidth(right)
	m.side.SetHeight(height)
	m.side.SetContentLines(ticketLines(policy, right))
	if top {
		m.pane.GotoTop()
		m.side.GotoTop()
	}
}
