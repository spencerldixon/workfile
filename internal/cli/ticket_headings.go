package cli

import (
	"image"
	"slices"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

const maxPreviewHeadings = 24

type ticketHeading struct {
	text         string
	level, width int
}

type ticketHeadingMsg struct {
	heading    ticketHeading
	img        image.Image
	cols, rows int
	ok         bool
}

func (p ticketPreview) headingSpec(b workfile.Block, width int) ticketHeading {
	if b.Quoted {
		width -= 2
	}
	return ticketHeading{text: p.plain(b.Spans), level: max(1, min(6, b.Level)), width: width}
}

// Collect the same widths used by rendering, including quoted headings and
// nested replies. Duplicate headings share one terminal image.
func (p ticketPreview) headingRequests(d workfile.Detail) []ticketHeading {
	var requests []ticketHeading
	add := func(blocks []workfile.Block, width int) {
		for _, b := range blocks {
			if b.Kind != workfile.BlockHeading {
				continue
			}
			heading := p.headingSpec(b, width)
			if heading.text == "" || heading.width < minImageWidth || slices.Contains(requests, heading) {
				continue
			}
			if len(requests) >= maxPreviewHeadings {
				return
			}
			requests = append(requests, heading)
		}
	}
	add(d.Body, p.textWidth())
	var comments func([]workfile.Comment, int)
	comments = func(all []workfile.Comment, depth int) {
		_, width := p.commentSize(depth)
		for _, c := range all {
			add(c.Body, width)
			comments(c.Replies, depth+1)
		}
	}
	if p.showComments {
		comments(d.Comments, 0)
	}
	return requests
}

func (m *trackerModel) loadPreviewHeadings() tea.Cmd {
	r := m.ticketReads.byKey[m.current]
	if !m.gfx.on || !m.gfx.hasInk || r == nil || r.detail == nil || r.detailErr != nil || m.shownView().board() && !m.onTicket {
		return nil
	}
	var cmds []tea.Cmd
	for _, heading := range m.previewRenderer().headingRequests(*r.detail) {
		if m.headingTried[heading] {
			continue
		}
		m.headingTried[heading] = true
		g := m.gfx
		cmds = append(cmds, func() tea.Msg {
			img, cols, rows, ok := headingImage(heading.text, heading.level, g, heading.width)
			return ticketHeadingMsg{heading, img, cols, rows, ok}
		})
	}
	return tea.Batch(cmds...)
}

func (m *trackerModel) showHeading(msg ticketHeadingMsg) tea.Cmd {
	if !msg.ok || m.headings[msg.heading] != nil {
		return nil
	}
	p := &ticketPicture{picture: picture{id: m.allocate(), cols: msg.cols, rows: msg.rows}, width: msg.heading.width}
	m.headings[msg.heading] = p
	m.fill(false)
	return sendPicture(msg.img, p.picture)
}
