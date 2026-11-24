package cli

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func previewTextWidth(width int) int { return max(10, width-4) }

func trackerPaneWidth(width int) int { return max(10, width-2*len(trackerInset)) }

// ticketPreview owns rich ticket presentation for both tracker layouts. It
// draws from immutable input and cached pictures; it never fetches or transmits.
type ticketPreview struct {
	v              view
	t              theme
	panelTheme     theme
	tagPalette     ticketTagPalette
	width          int
	tracker        bool
	now            time.Time
	showComments   bool
	images, titles map[string]*ticketPicture
	headings       map[ticketHeading]*ticketPicture
}

func (m *trackerModel) ticketContent() (heading ticketBlock, body, policy []ticketBlock) {
	item, ok := m.selected()
	if !ok {
		return
	}
	r := m.ticketReads.byKey[item.Key]
	left, right := m.columns()
	preview := m.previewRenderer()
	if right == 0 {
		right = left
	}
	var statusIcon string
	var statusColour lipgloss.ANSIColor
	if preview.t.color {
		icon, style := m.statusSignal(item, lipgloss.NewStyle())
		statusIcon = icon
		if icon == "→" {
			statusIcon = "\uf06e"
		}
		statusColour, _ = style.GetForeground().(lipgloss.ANSIColor)
	}
	return ticketBlock{lines: preview.identity(item, statusIcon, statusColour)}, preview.ticketBody(r), panelBlocks(m.paneFor(r, right).sections())
}

func (m *trackerModel) previewRenderer() ticketPreview {
	width, _ := m.columns()
	return ticketPreview{
		v: m.v, t: m.t, panelTheme: m.panelTheme, tagPalette: m.tagPalette, width: width, tracker: true, now: m.now(),
		showComments: m.e.tracker.ShowComments(), images: m.images, titles: m.titles, headings: m.headings,
	}
}

func (p ticketPreview) textWidth() int {
	if p.tracker {
		return trackerPaneWidth(p.width)
	}
	return previewTextWidth(p.width)
}

type ticketBlock struct {
	lines []string
}

func panelBlocks(all [][]string) []ticketBlock {
	blocks := make([]ticketBlock, len(all))
	for i, lines := range all {
		blocks[i] = ticketBlock{lines: lines}
	}
	return blocks
}

func (p ticketPreview) sections(all []ticketBlock, width int) string {
	return strings.Join(ticketLines(all, width, ticketIndent), "\n")
}

func ticketLines(all []ticketBlock, width int, inset string) []string {
	var lines []string
	for _, b := range all {
		if len(b.lines) == 0 {
			continue
		}
		lines = append(lines, "")
		for _, line := range b.lines {
			line = inset + line
			lines = append(lines, line+strings.Repeat(" ", max(0, width-ansi.StringWidth(line))))
		}
	}
	return lines
}

func (p ticketPreview) ticketBody(r *ticketRead) []ticketBlock {
	switch {
	case r == nil || !r.detailDone():
		return []ticketBlock{{lines: []string{p.t.dim("Reading the ticket…")}}}
	case r.detailErr != nil:
		return []ticketBlock{{lines: fit(p.t.bad("! Cannot read this ticket: "+clean(r.detailErr.Error())), "", "", p.textWidth())}}
	}
	body := []string{p.t.bold("Description"), ""}
	if len(r.detail.Body) == 0 {
		body = append(body, p.t.dim("No description."))
	} else {
		body = append(body, p.blocks(r.detail.Body, p.textWidth())...)
	}
	blocks := []ticketBlock{{lines: body}}
	if p.showComments {
		comments := p.comments(*r.detail)
		if len(r.detail.Comments) > 0 {
			divider := p.t.dim(strings.Repeat("─", p.textWidth()))
			comments[0].lines = append([]string{divider, ""}, comments[0].lines...)
		}
		blocks = append(blocks, comments...)
	}
	return blocks
}

func (p ticketPreview) identity(item ticketItem, statusIcon string, statusColour lipgloss.ANSIColor) []string {
	width := p.textWidth()
	key := p.t.bold(p.v.link(clean(item.Key), item.URL))
	metadata := []ticketMetadata{
		{item.Type, "\uf02d", 4},
		{item.Status, statusIcon, statusColour},
		{assigneeName(item.Listing), "\uf007", 6},
	}
	lines := p.metadataLines(key, metadata)
	if !p.v.hyperlinks() {
		lines = append(lines, fit(p.t.dim(clean(item.URL)), "", "", width)...)
	}
	lines = append(lines, "")
	if picture := p.titles[item.Key]; picture != nil && picture.width == width {
		return append(lines, placeholderRows(picture.picture)...)
	}
	return append(lines, fit(p.t.bold(clean(item.Title)), "", "", width)...)
}

type ticketMetadata struct {
	value, icon string
	fill        lipgloss.ANSIColor
}

func (p ticketPreview) metadataLines(key string, metadata []ticketMetadata) []string {
	width := p.textWidth()
	if !p.t.color {
		var facts string
		for _, meta := range metadata {
			if value := strings.TrimSpace(clean(meta.value)); value != "" {
				facts += " · " + value
			}
		}
		return fit(key+facts, "", "", width)
	}
	line := ansi.Truncate(key, width, "…")
	var lines []string
	for _, meta := range metadata {
		if value := strings.TrimSpace(clean(meta.value)); value != "" {
			body := ansi.Truncate(meta.icon+" "+value, width-2, "…")
			fill := meta.fill
			if fill == 0 {
				fill = 8 // Neutral status
			}
			tag := p.metadataTag(body, fill)
			if ansi.StringWidth(line)+ansi.StringWidth(tag)+2 > width {
				lines = append(lines, line)
				line = tag
			} else {
				line += "  " + tag
			}
		}
	}
	return append(lines, line)
}

func (p ticketPreview) blocks(blocks []workfile.Block, width int) []string {
	var lines []string
	for i, b := range blocks {
		if i > 0 && !(b.Kind == workfile.BlockList && blocks[i-1].Kind == workfile.BlockList) {
			lines = append(lines, "")
		}
		lines = append(lines, p.block(b, width)...)
	}
	return lines
}

func (p ticketPreview) block(b workfile.Block, width int) []string {
	indent := ""
	if b.Quoted {
		indent = "  "
	}
	switch b.Kind {
	case workfile.BlockHeading:
		heading := p.headingSpec(b, width)
		if image := p.headings[heading]; image != nil && image.cols <= heading.width {
			lines := placeholderRows(image.picture)
			for i := range lines {
				lines[i] = indent + lines[i]
			}
			return lines
		}
		return fit(p.t.bold(p.plain(b.Spans)), indent, indent, width)
	case workfile.BlockList:
		lead := indent + strings.Repeat("  ", min(b.Level, 4))
		marker := clean(b.Marker) + " "
		return fit(p.spans(b.Spans), lead+marker, lead+strings.Repeat(" ", runewidth.StringWidth(marker)), width)
	case workfile.BlockCode:
		var lines []string
		code := ansi.Hardwrap(p.plainLines(b.Spans), max(4, width-len(indent)-2), true)
		for _, line := range strings.Split(code, "\n") {
			lines = append(lines, indent+"  "+p.t.accent(line))
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
				text = p.t.bold(text)
			}
			lines = append(lines, fit(text, indent, indent, width)...)
		}
		return lines
	case workfile.BlockRule:
		return []string{indent + p.t.dim(strings.Repeat("─", max(1, width-len(indent))))}
	case workfile.BlockImage:
		if image := p.images[b.Attachment]; image != nil && p.textWidth() >= minImageWidth && image.cols <= width {
			return placeholderRows(image.picture)
		}
		return fit(p.t.dim(strings.TrimSpace("[image] "+clean(b.Name))), indent, indent, width)
	}
	text := p.spans(b.Spans)
	if b.Quoted {
		text = p.t.dim(p.plain(b.Spans))
	}
	return fit(text, indent, indent, width)
}

func (p ticketPreview) plain(spans []workfile.Span) string {
	return strings.ReplaceAll(p.plainLines(spans), "\n", " ")
}
func (p ticketPreview) plainLines(spans []workfile.Span) string {
	var out strings.Builder
	for _, span := range spans {
		for i, part := range strings.Split(span.Text, "\n") {
			if i > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(clean(part))
		}
	}
	return out.String()
}
func (p ticketPreview) spans(spans []workfile.Span) string {
	var out strings.Builder
	for _, span := range spans {
		for i, part := range strings.Split(span.Text, "\n") {
			if i > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(p.span(span, part))
		}
	}
	return out.String()
}
func (p ticketPreview) span(s workfile.Span, text string) string {
	if text == "" {
		return ""
	}
	styled := clean(text)
	if s.Link != "" {
		styled = p.v.link(text, s.Link)
		if !p.v.hyperlinks() && clean(s.Link) != styled {
			styled += " (" + clean(s.Link) + ")"
		}
	}
	if s.Code {
		styled = p.t.accent(styled)
	}
	if s.Italic {
		styled = p.t.render(styled, func(st lipgloss.Style) lipgloss.Style { return st.Italic(true) })
	}
	if s.Bold {
		styled = p.t.bold(styled)
	}
	return styled
}
