package cli

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

type inboxTicket struct {
	row     workfile.InboxRow
	item    ticketItem
	read    *ticketRead
	actions workfile.NextActions
	body    viewport.Model
	heading []string
	footer  []string
	notice  string
}

type inboxTicketPolicyMsg struct {
	read       *ticketRead
	assessment workfile.Assessment
	actions    workfile.NextActions
	err        error
}

func (m *inboxModel) openTicket(row workfile.InboxRow) tea.Cmd {
	listing := workfile.Listing{Key: row.Ticket, Title: row.Title, State: row.Stage, URL: row.URL}
	var work workfile.InboxWork
	if m.data != nil {
		if i := slices.IndexFunc(m.data.work, func(w workfile.InboxWork) bool { return w.Listing.Key == row.Ticket }); i >= 0 {
			work = m.data.work[i]
			listing = work.Listing
		}
	}
	m.ticket = &inboxTicket{row: row, item: ticketItem{Listing: listing}, body: viewport.New(), actions: work.Next}
	return m.readTicket(work.Assessment)
}

func (m *inboxModel) readTicket(cached *workfile.Assessment) tea.Cmd {
	t := m.ticket
	if t.read != nil {
		t.read.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	r := &ticketRead{policy: cached, cancel: cancel, detailStarted: true, policyStarted: cached == nil}
	t.read = r
	m.workflow.clear()
	t.notice = ""
	m.fillTicket()
	e, ticket := m.e, t.row.Ticket
	detail := func() tea.Msg {
		d, err := e.client.Detail(ctx, ticket)
		return ticketDetailMsg{r, d, err}
	}
	if cached != nil {
		return tea.Batch(detail, m.loadTicketWorkflow())
	}
	var who map[string]string
	if m.data != nil {
		who = m.data.who
	}
	return tea.Batch(detail, func() tea.Msg {
		a, err := readPolicy(ctx, e, ticket)
		if err != nil {
			return inboxTicketPolicyMsg{read: r, err: err}
		}
		return inboxTicketPolicyMsg{read: r, assessment: a, actions: e.w.NextActions(a, who)}
	})
}

func (m *inboxModel) loadTicketWorkflow() tea.Cmd {
	t := m.ticket
	if t == nil || t.read.policy == nil {
		return nil
	}
	return m.workflow.request(m.e.w.Policy, *t.read.policy, previewTextWidth(m.renderWidth()))
}

func (m *inboxModel) ticketPane() workfilePane {
	t := m.ticket
	theme := m.theme()
	return workfilePane{
		v: m.e.view, t: theme, panelTheme: newTheme(theme.color, m.e.view.laneBackground()),
		policy: m.e.w.Policy, assessment: t.read.policy, actions: t.actions,
		pending: !t.read.policyDone(), err: t.read.policyErr, width: previewTextWidth(m.renderWidth()),
		focusPR: t.row.PRURL, now: m.now(), flow: m.workflow.picture,
	}
}

func (m *inboxModel) closeTicket() {
	if m.ticket.read != nil {
		m.ticket.read.cancel()
	}
	m.ticket = nil
	m.workflow.clear()
}

func (m *inboxModel) ticketKey(msg tea.KeyPressMsg) tea.Cmd {
	t := m.ticket
	switch msg.String() {
	case "esc":
		m.closeTicket()
		return nil
	case "q", "ctrl+c":
		t.read.cancel()
		return tea.Quit
	case "r":
		return m.readTicket(nil)
	case "o":
		target, _ := inboxOpenTarget(t.row)
		return m.openTicketLink(firstOf(target, t.item.URL))
	}
	if n, ok := fixKey(msg); ok {
		if fixes := m.ticketPane().numbered(); n <= len(fixes) {
			return m.openTicketLink(fixURL(fixes[n-1], nil))
		}
	}
	var cmd tea.Cmd
	t.body, cmd = t.body.Update(msg)
	return cmd
}

func (m *inboxModel) openTicketLink(url string) tea.Cmd {
	if !trackerWebURL(url) {
		return nil
	}
	open, ticket := m.open, m.ticket
	return func() tea.Msg { return inboxTicketOpenedMsg{ticket, open(url)} }
}

type inboxTicketOpenedMsg struct {
	ticket *inboxTicket
	err    error
}

func (m *inboxModel) ticketHeading() []string {
	t, theme := m.ticket, m.theme()
	line := theme.bold(m.e.view.link(t.item.Key, t.item.URL))
	if t.row.PR != "" {
		line += "  " + m.e.view.link(t.row.PR, t.row.PRURL)
	}
	return fit(line, ticketIndent, ticketIndent, m.renderWidth())
}

func (m *inboxModel) ticketFooter(p workfilePane) []string {
	t := m.ticket
	keys := "esc inbox · ↑↓ scroll · r refresh · [/] view"
	if _, label := inboxOpenTarget(t.row); label != "" {
		keys += " · o " + label
	} else if trackerWebURL(t.item.URL) {
		keys += " · o open ticket"
	}
	n := min(9, len(p.numbered()))
	switch {
	case n == 1:
		keys += " · 1 fix"
	case n > 1:
		keys += " · 1-" + strconv.Itoa(n) + " fixes"
	}
	lines := fit(keys, ticketIndent, ticketIndent, m.renderWidth())
	if t.notice != "" {
		lines = append(fit(m.theme().bad(t.notice), ticketIndent, ticketIndent, m.renderWidth()), lines...)
	}
	return lines
}

func (m *inboxModel) fillTicket() {
	t := m.ticket
	if t == nil {
		return
	}
	width := m.renderWidth()
	pane := m.ticketPane()
	preview := ticketPreview{
		v: pane.v, t: pane.t, panelTheme: pane.panelTheme, width: width, now: pane.now,
		showComments: true,
	}
	lines := fit(pane.t.bold(clean(t.item.Title)), "", "", preview.textWidth())
	lines = append(lines, fit(clean(t.row.Text), "", "", preview.textWidth())...)
	if !pane.v.hyperlinks() {
		for _, url := range []string{t.item.URL, t.row.PRURL} {
			if url != "" {
				lines = append(lines, fit(clean(url), "", "", preview.textWidth())...)
			}
		}
	}
	blocks := append([]ticketBlock{{lines: lines}}, panelBlocks(pane.sections())...)
	blocks = append(blocks, preview.ticketBody(t.read)...)
	t.heading, t.footer = m.ticketHeading(), m.ticketFooter(pane)
	t.body.SetWidth(width)
	t.body.SetHeight(max(1, m.bodyHeight(len(t.footer))-len(t.heading)))
	t.body.SetContent(strings.TrimPrefix(preview.sections(blocks, width), "\n"))
}
