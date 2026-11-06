package cli

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func (m *ticketsModel) render() string {
	lines := []string{m.header()}
	rows := m.listRows()
	list := m.listLines()
	lines = append(lines, list[:min(rows, len(list))]...)
	for len(lines) < rows+1 {
		lines = append(lines, "")
	}
	main := strings.Split(m.pane.View(), "\n")
	if _, right := m.columns(); right > 0 {
		for i, line := range strings.Split(m.side.View(), "\n") {
			if i < len(main) {
				main[i] += " " + line
			}
		}
	}
	return strings.Join(append(append(lines, main...), m.footer()), "\n")
}

// tab draws one view's name. The current one stands out by more than colour.
// The Assigned to me count is left out until it is known who "me" is.
func (m *ticketsModel) tab(view ticketView, counts bool) string {
	label := view.label()
	if counts && (!view.me || m.me != "") {
		label += fmt.Sprintf(" %d", m.counts[view])
	}
	switch {
	case view != m.shownView():
		return " " + m.t.dim(label) + " "
	case m.t.color:
		return m.t.render(" "+label+" ", func(st lipgloss.Style) lipgloss.Style { return st.Reverse(true).Bold(true) })
	}
	return "[" + label + "]"
}

// tabs fits the views into room. Narrow terminals drop the counts first, then
// the other views, so the current one is always named.
func (m *ticketsModel) tabs(room int) string {
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
	return m.tab(m.shownView(), false)
}

// header says what the list holds: the views, the search, and how many tickets
// of the view the search leaves. It runs the full width.
func (m *ticketsModel) header() string {
	right := ""
	if m.loaded {
		right = fmt.Sprintf("%d of %d", len(m.list.VisibleItems()), m.total)
	}
	if m.busy() {
		right = strings.TrimSpace(m.spin.View() + " " + right)
	}
	query := ""
	switch m.list.FilterState() {
	case list.Filtering:
		query = "/ " + clean(m.list.FilterValue()) + "█"
	case list.FilterApplied:
		query = "/ " + clean(m.list.FilterValue())
	}
	margin := len(ticketIndent)
	inner := m.width - 2*margin
	searchRoom := min(runewidth.StringWidth(query)+2, inner/2)
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

// footer is the keys, and how far through the ticket the reader is when it does
// not all fit.
func (m *ticketsModel) footer() string {
	right := ""
	if m.pane.TotalLineCount() > m.pane.VisibleLineCount() {
		right = fmt.Sprintf("%d%%", int(m.pane.ScrollPercent()*100))
	}
	margin := len(ticketIndent)
	room := m.width - 2*margin - len(right) - 1
	// The help line can overflow when even its "…" does not fit, so cut it.
	left := ansi.Truncate(m.help.ShortHelpView(m.helpKeys()), max(0, room), "")
	if m.notice != "" {
		left = m.t.bad("! " + m.notice)
	}
	gap := m.width - margin - ansi.StringWidth(left) - len(right) - margin
	return ticketIndent + left + strings.Repeat(" ", max(1, gap)) + m.t.dim(right)
}

func (m *ticketsModel) listLines() []string {
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
	return strings.Split(m.list.View(), "\n")
}

// emptyView says why a view has nothing in it.
func (m *ticketsModel) emptyView() string {
	view := m.shownView()
	switch {
	case view.me && m.findingMe:
		return "Finding your tickets…"
	case view.me && m.me == "":
		return cannotFindMe
	case view.me:
		return "Nothing assigned to you."
	}
	return "Nothing in " + view.label() + "."
}

// row draws one ticket across the full width. The key leads, the title gets the
// room, and the status and assignee sit at the right edge. Narrow terminals drop
// the assignee, then the status, so the key and title stay readable. The current
// row is a bright bar while the list has focus and a soft one otherwise.
func (m *ticketsModel) row(item ticketItem, selected bool, matches []int) string {
	const whoWidth = 16
	margin := len(ticketIndent)
	width := m.width - margin
	status := runewidth.Truncate(clean(item.Status), m.statusWidth, "…")
	who := runewidth.Truncate(clean(assigneeName(item.Listing)), whoWidth, "…")
	titleWidth := width - margin - m.keyWidth - 2
	showWho := titleWidth-m.statusWidth-2-whoWidth-2 >= 24
	showStatus := titleWidth-m.statusWidth-2 >= 16
	switch {
	case showWho:
		titleWidth -= m.statusWidth + whoWidth + 4
	case showStatus:
		titleWidth -= m.statusWidth + 2
	}
	titleWidth = max(1, titleWidth)
	key := clean(item.Key)
	title := runewidth.Truncate(clean(item.Title), titleWidth, "…")

	base := lipgloss.NewStyle()
	bar := selected && !m.onTicket
	if bar {
		base = base.Reverse(true)
	}
	soft := base
	if !bar {
		soft = soft.Faint(true)
	}
	if selected {
		base = base.Bold(true)
	}
	hits := map[int]bool{}
	for _, i := range matches {
		hits[i] = true
	}
	cursor := ticketIndent
	if selected {
		cursor = "▸ "
	}
	line := m.paint(cursor, 0, nil, base) +
		m.paint(key, 0, hits, base.Bold(true)) +
		m.paint(strings.Repeat(" ", m.keyWidth-runewidth.StringWidth(key)+2), 0, nil, base) +
		m.paint(title, len([]rune(key))+1, hits, base) +
		m.paint(strings.Repeat(" ", titleWidth-runewidth.StringWidth(title)), 0, nil, base)
	if showStatus || showWho {
		line += m.paint("  "+pad(status, m.statusWidth), 0, nil, soft)
	}
	if showWho {
		line += m.paint("  "+pad(who, whoWidth), 0, nil, soft)
	}
	line += m.paint(strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line))), 0, nil, base)
	if !m.t.color {
		return strings.TrimRight(line, " ")
	}
	if selected && !bar {
		return m.t.onPanel(line, m.width)
	}
	return line
}

// paint styles text, underlining the characters a search matched. offset is
// where text starts in the words the search looked at.
func (m *ticketsModel) paint(text string, offset int, hits map[int]bool, st lipgloss.Style) string {
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

// minCommentWidth is the narrowest a reply's block gets.
const minCommentWidth = 24

// textWidth is the room for text inside the ticket's panels.
func (m *ticketsModel) textWidth() int {
	left, _ := m.columns()
	return max(10, left-4)
}

// imageWidth leaves room for an image inside a comment or a reply.
func (m *ticketsModel) imageWidth() int { return max(1, m.textWidth()-6) }

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

// ticketBlock is one block of the ticket: a panel, or with flat a plain heading.
// indent moves it in from the left, which is how a reply sits inside the
// comment it answers.
type ticketBlock struct {
	lines  []string
	indent int
	flat   bool
}

func panelBlocks(all [][]string) []ticketBlock {
	blocks := make([]ticketBlock, len(all))
	for i, lines := range all {
		blocks[i] = ticketBlock{lines: lines}
	}
	return blocks
}

// sections stacks blocks with a gap between them. Each panel runs to the right
// edge of its column.
func (m *ticketsModel) sections(all []ticketBlock, width int) string {
	var lines []string
	for _, b := range all {
		if len(b.lines) == 0 {
			continue
		}
		if b.flat {
			for _, line := range b.lines {
				lines = append(lines, ticketIndent+line)
			}
		} else {
			lead := strings.Repeat(" ", b.indent)
			for _, line := range strings.Split(m.t.panel(strings.Join(b.lines, "\n"), width-b.indent), "\n") {
				lines = append(lines, lead+line)
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// ticketText is the bottom half, as the ticket's column and Workfile's. On a
// narrow terminal both share one column, with blockers before the description so
// the next action is on the first screen.
func (m *ticketsModel) ticketText() (main, side string) {
	item, ok := m.selected()
	if !ok {
		return "", ""
	}
	r := m.reads[item.Key]
	left, right := m.columns()
	wide := right > 0
	ticket := m.ticketBody(r)
	policyWidth := left
	if wide {
		policyWidth = right
	}
	lead, tail := m.policy(r, policyWidth)
	head := ticketBlock{lines: m.identity(item)}
	if wide {
		return m.sections(append([]ticketBlock{head}, ticket...), left), m.sections(panelBlocks(append(lead, tail...)), right)
	}
	all := append([]ticketBlock{head}, panelBlocks(lead)...)
	return m.sections(append(append(all, ticket...), panelBlocks(tail)...), left), ""
}

// ticketBody is the ticket's own text, in the tracker's words.
func (m *ticketsModel) ticketBody(r *ticketRead) []ticketBlock {
	width := m.textWidth()
	switch {
	case r == nil || !r.detailDone():
		return []ticketBlock{{lines: []string{m.t.dim("Reading the ticket…")}}}
	case r.detailErr != nil:
		return []ticketBlock{{lines: fit(m.t.bad("! Cannot read this ticket: "+clean(r.detailErr.Error())), "", "", width)}}
	}
	return append([]ticketBlock{{lines: m.body(*r.detail)}}, m.comments(*r.detail)...)
}

// policy is Workfile's view of the ticket: where it is and what it needs, then
// its PRs and next moves.
func (m *ticketsModel) policy(r *ticketRead, panel int) (lead, tail [][]string) {
	width := max(10, panel-4)
	switch {
	case r == nil || !r.policyDone():
		return [][]string{{m.t.dim("Checking the policy…")}}, nil
	case r.policyErr != nil:
		return [][]string{fit(m.t.bad("! Cannot check the policy: "+clean(r.policyErr.Error())), "", "", width)}, nil
	case r.policy.Ticket.Error != "":
		return [][]string{fit(m.t.bad("! Cannot check the policy: "+clean(r.policy.Ticket.Error)), "", "", width)}, nil
	}
	v := m.v
	v.width = panel + len(ticketIndent) // the ticket screen's panels sit one indent in
	t := newTicketModel(v, m.e.w, *r.policy)
	return [][]string{t.track(), t.attention()}, [][]string{t.pullRequests(), t.nextMoves()}
}

func (m *ticketsModel) identity(item ticketItem) []string {
	width := m.textWidth()
	var facts string
	for _, fact := range []string{item.Type, item.Status, assigneeName(item.Listing)} {
		if fact != "" {
			facts += " · " + clean(fact)
		}
	}
	lines := fit(m.t.bold(m.v.link(item.Key, item.URL))+m.t.dim(facts), "", "", width)
	if !m.v.hyperlinks() {
		lines = append(lines, fit(m.t.dim(clean(item.URL)), "", "", width)...)
	}
	lines = append(lines, "")
	if p := m.titles[item.Key]; p != nil && p.width == width {
		return append(lines, placeholderRows(p.picture)...)
	}
	return append(lines, fit(m.t.bold(clean(item.Title)), "", "", width)...)
}

func (m *ticketsModel) body(d workfile.Detail) []string {
	if len(d.Body) == 0 {
		return []string{m.t.dim("No description.")}
	}
	return m.blocks(d.Body, m.textWidth())
}

// comments is a heading, then a block for each comment.
func (m *ticketsModel) comments(d workfile.Detail) []ticketBlock {
	if len(d.Comments) == 0 {
		return []ticketBlock{{lines: []string{m.t.dim("No comments.")}}}
	}
	heading := []string{m.t.bold(fmt.Sprintf("Comments (%d)", d.CommentTotal))}
	if shown := countComments(d.Comments); shown < d.CommentTotal {
		heading = append(heading, fit(m.t.dim(fmt.Sprintf("Showing %d of %d. Press o to read them all.", shown, d.CommentTotal)), "", "", m.textWidth())...)
	}
	blocks := []ticketBlock{{lines: heading, flat: true}}
	for _, c := range d.Comments {
		blocks = append(blocks, m.comment(c, 0)...)
	}
	return blocks
}

func countComments(comments []workfile.Comment) int {
	n := len(comments)
	for _, c := range comments {
		n += countComments(c.Replies)
	}
	return n
}

// comment is one comment's block, then a block for each reply, indented so a
// thread reads at a glance. Deep threads stop indenting, so the text keeps room.
func (m *ticketsModel) comment(c workfile.Comment, depth int) []ticketBlock {
	left, _ := m.columns()
	indent := max(0, min(min(depth, 4)*3, left-minCommentWidth))
	width := max(10, left-indent-4)
	who := m.t.bold(clean(c.Author))
	if depth > 0 {
		who = m.t.dim("↳ ") + who
	}
	if when := ago(c.Created, m.now()); when != "" {
		who += m.t.dim(" · " + when)
	}
	blocks := []ticketBlock{{lines: append(fit(who, "", "", width), m.blocks(c.Body, width)...), indent: indent}}
	for _, reply := range c.Replies {
		blocks = append(blocks, m.comment(reply, depth+1)...)
	}
	return blocks
}

// blocks draws a ticket's rich text, with a blank line between blocks. Items of
// one list stay together.
func (m *ticketsModel) blocks(blocks []workfile.Block, width int) []string {
	var lines []string
	for i, b := range blocks {
		if i > 0 && !(b.Kind == workfile.BlockList && blocks[i-1].Kind == workfile.BlockList) {
			lines = append(lines, "")
		}
		lines = append(lines, m.block(b, width)...)
	}
	return lines
}

func (m *ticketsModel) block(b workfile.Block, width int) []string {
	indent := ""
	if b.Quoted {
		indent = "  "
	}
	switch b.Kind {
	case workfile.BlockHeading:
		return fit(m.t.bold(m.plain(b.Spans)), indent, indent, width)
	case workfile.BlockList:
		lead := indent + strings.Repeat("  ", min(b.Level, 4))
		marker := clean(b.Marker) + " "
		return fit(m.spans(b.Spans), lead+marker, lead+strings.Repeat(" ", runewidth.StringWidth(marker)), width)
	case workfile.BlockCode:
		var lines []string
		wrappedCode := ansi.Hardwrap(m.plainLines(b.Spans), max(4, width-len(indent)-2), true)
		for _, line := range strings.Split(wrappedCode, "\n") {
			lines = append(lines, indent+"  "+m.t.accent(line))
		}
		return lines
	case workfile.BlockTable:
		var lines []string
		for i, row := range b.Rows {
			cells := make([]string, len(row))
			for j, cell := range row {
				cells[j] = clean(cell)
			}
			text := strings.Join(cells, " · ")
			if i == 0 && b.Header {
				text = m.t.bold(text)
			}
			lines = append(lines, fit(text, indent, indent, width)...)
		}
		return lines
	case workfile.BlockRule:
		return []string{indent + m.t.dim(strings.Repeat("─", max(1, width-len(indent))))}
	case workfile.BlockImage:
		if p := m.images[b.Attachment]; p != nil && m.textWidth() >= minImageWidth && p.cols <= width {
			return placeholderRows(p.picture)
		}
		return fit(m.t.dim(strings.TrimSpace("[image] "+clean(b.Name))), indent, indent, width)
	}
	text := m.spans(b.Spans)
	if b.Quoted {
		text = m.t.dim(m.plain(b.Spans))
	}
	return fit(text, indent, indent, width)
}

func (m *ticketsModel) plain(spans []workfile.Span) string {
	return strings.ReplaceAll(m.plainLines(spans), "\n", " ")
}

// plainLines is the text of spans, safe to draw, keeping its line breaks.
func (m *ticketsModel) plainLines(spans []workfile.Span) string {
	var b strings.Builder
	for _, s := range spans {
		parts := strings.Split(s.Text, "\n")
		for i, part := range parts {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(clean(part))
		}
	}
	return b.String()
}

// spans styles rich text. Every piece goes through clean, and view.link allows
// only safe web addresses, so ticket text cannot reach the terminal raw.
func (m *ticketsModel) spans(spans []workfile.Span) string {
	var b strings.Builder
	for _, s := range spans {
		for i, part := range strings.Split(s.Text, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(m.span(s, part))
		}
	}
	return b.String()
}

func (m *ticketsModel) span(s workfile.Span, text string) string {
	if text == "" {
		return ""
	}
	styled := clean(text)
	if s.Link != "" {
		styled = m.v.link(text, s.Link)
		if !m.v.hyperlinks() && clean(s.Link) != styled {
			styled += " (" + clean(s.Link) + ")"
		}
	}
	if s.Code {
		styled = m.t.accent(styled)
	}
	if s.Italic {
		styled = m.t.render(styled, func(st lipgloss.Style) lipgloss.Style { return st.Italic(true) })
	}
	if s.Bold {
		styled = m.t.bold(styled)
	}
	return styled
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
