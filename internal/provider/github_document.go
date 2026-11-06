package provider

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"workfile/internal/workfile"
)

// markdownBlocks turns GitHub Markdown into blocks, for reading. It handles what
// issues use: headings, paragraphs, emphasis, code, links, lists, quotes, rules,
// tables and images. Anything else stays as plain text. Text is returned as it
// came: the caller cleans it before drawing.
func markdownBlocks(text string) []workfile.Block {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return mdParse(strings.Split(text, "\n"), false, 0)
}

// markdownText is the plain text of a body and its headings, for policy checks.
// Headings is never nil.
func markdownText(body string) (string, []string) {
	blocks := markdownBlocks(body)
	headings := []string{}
	var lines []string
	for _, b := range blocks {
		var line string
		switch b.Kind {
		case workfile.BlockRule:
			continue
		case workfile.BlockTable:
			for _, row := range b.Rows {
				lines = append(lines, strings.Join(row, " | "))
			}
			continue
		case workfile.BlockImage:
			line = b.Name
		case workfile.BlockList:
			line = strings.Repeat("  ", b.Level) + b.Marker + " " + spansText(b.Spans)
		default:
			line = spansText(b.Spans)
		}
		if b.Kind == workfile.BlockHeading && !b.Quoted {
			headings = append(headings, line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), headings
}

func spansText(spans []workfile.Span) string {
	var text strings.Builder
	for _, s := range spans {
		text.WriteString(s.Text)
	}
	return text.String()
}

var (
	mdListItem  = regexp.MustCompile(`^( *)([-*+]|[0-9]{1,9}[.)])[ \t]+(.*)$`)
	mdTableRule = regexp.MustCompile(`^:?-+:?$`)
	mdImage     = regexp.MustCompile(`!\[([^\]]*)\]\(([^()\s]+)(?:[ \t]+"[^"]*")?\)`)
)

// mdIndent splits a line into its leading width (a tab counts four) and the rest.
func mdIndent(line string) (int, string) {
	width := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width, line[i:]
		}
	}
	return width, ""
}

// mdMaxDepth caps how deep quotes and emphasis nest. Anything deeper is text, so
// hostile input cannot overflow the stack.
const mdMaxDepth = 32

// mdWindow caps how far a bracket looks for its partner, so a run of unmatched
// brackets costs a little each, not the whole text each.
const mdWindow = 2000

func mdParse(lines []string, quoted bool, depth int) []workfile.Block {
	var blocks []workfile.Block
	var para []string
	add := func(b workfile.Block) {
		b.Quoted = quoted
		blocks = append(blocks, b)
	}
	flush := func() {
		if len(para) > 0 {
			for _, b := range mdParagraph(strings.Join(para, "\n")) {
				add(b)
			}
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		indent, text := mdIndent(strings.TrimRight(lines[i], " \t"))
		switch {
		case text == "":
			flush()
		case indent <= 3 && fenceMarker(text) != "":
			flush()
			fence := fenceMarker(text)
			var code []string
			for i++; i < len(lines); i++ {
				_, inner := mdIndent(strings.TrimRight(lines[i], " \t"))
				if closes := fenceMarker(inner); closes != "" && closes == inner && closes[0] == fence[0] && len(closes) >= len(fence) {
					break
				}
				code = append(code, lines[i])
			}
			add(workfile.Block{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: strings.Join(code, "\n"), Code: true}}})
		case indent <= 3 && mdHeading(text) != nil:
			flush()
			if h := mdHeading(text); h.Spans != nil {
				add(*h)
			}
		case indent <= 3 && mdRule(text):
			flush()
			add(workfile.Block{Kind: workfile.BlockRule})
		case indent <= 3 && text[0] == '>' && depth < mdMaxDepth:
			flush()
			var inner []string
			for ; i < len(lines); i++ {
				_, line := mdIndent(strings.TrimRight(lines[i], " \t"))
				if line == "" || line[0] != '>' {
					break
				}
				inner = append(inner, strings.TrimPrefix(line[1:], " "))
			}
			i--
			blocks = append(blocks, mdParse(inner, true, depth+1)...)
		case strings.Contains(text, "|") && i+1 < len(lines) && mdTableHead(text, lines[i+1]):
			flush()
			table := workfile.Block{Kind: workfile.BlockTable, Header: true, Rows: [][]string{mdCells(text)}}
			for i += 2; i < len(lines) && strings.Contains(lines[i], "|"); i++ {
				table.Rows = append(table.Rows, mdCells(lines[i]))
			}
			i--
			add(table)
		case mdListItem.MatchString(strings.TrimRight(lines[i], " \t")) && !mdRule(text) && mdStartsList(para, text):
			flush()
			var end int
			for _, b := range mdList(lines[i:], &end) {
				add(b)
			}
			i += end - 1
		default:
			para = append(para, text)
		}
	}
	flush()
	return blocks
}

// mdStartsList says whether a list item may begin here. A numbered item that
// does not start at 1 does not interrupt a paragraph.
func mdStartsList(para []string, text string) bool {
	if len(para) == 0 || text[0] == '-' || text[0] == '*' || text[0] == '+' {
		return true
	}
	return strings.HasPrefix(text, "1.") || strings.HasPrefix(text, "1)")
}

// mdHeading reads an ATX heading. It returns nil if text is not one, and a block
// with no Spans if the heading is empty.
func mdHeading(text string) *workfile.Block {
	level := len(text) - len(strings.TrimLeft(text, "#"))
	if level < 1 || level > 6 || len(text) > level && text[level] != ' ' && text[level] != '\t' {
		return nil
	}
	rest := strings.TrimSpace(text[level:])
	// A closing run of # counts only after a space, so "C#" keeps its #.
	if trimmed := strings.TrimRight(rest, "#"); trimmed != rest && (trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t")) {
		rest = strings.TrimSpace(trimmed)
	}
	if rest == "" {
		return &workfile.Block{Kind: workfile.BlockHeading}
	}
	return &workfile.Block{Kind: workfile.BlockHeading, Level: level, Spans: mdSpans(rest, workfile.Span{})}
}

func mdRule(text string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(text)
	return len(compact) >= 3 && strings.Trim(compact, compact[:1]) == "" && strings.Contains("-*_", compact[:1])
}

// mdList reads list items from the start of lines until something else begins.
// end is how many lines it used.
func mdList(lines []string, end *int) []workfile.Block {
	type item struct {
		level  int
		marker string
		lines  []string
	}
	var items []item
	var stack []int
	i := 0
	for ; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" {
			next := i + 1
			if next < len(lines) && mdListItem.MatchString(strings.TrimRight(lines[next], " \t")) {
				continue
			}
			break
		}
		if m := mdListItem.FindStringSubmatch(line); m != nil && !mdRule(strings.TrimSpace(line)) {
			indent := len(m[1])
			for len(stack) > 0 && stack[len(stack)-1] > indent {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 || stack[len(stack)-1] < indent {
				stack = append(stack, indent)
			}
			marker, text := "•", m[3]
			if m[2][0] >= '0' && m[2][0] <= '9' {
				marker = m[2][:len(m[2])-1] + "."
			}
			switch {
			case strings.HasPrefix(text, "[ ] "):
				marker, text = "☐", text[4:]
			case strings.HasPrefix(text, "[x] "), strings.HasPrefix(text, "[X] "):
				marker, text = "✓", text[4:]
			}
			items = append(items, item{len(stack) - 1, marker, []string{text}})
			continue
		}
		indent, text := mdIndent(line)
		if indent <= 3 && (fenceMarker(text) != "" || mdHeading(text) != nil || mdRule(text) || text[0] == '>') {
			break
		}
		last := &items[len(items)-1]
		last.lines = append(last.lines, text)
	}
	*end = i
	blocks := make([]workfile.Block, len(items))
	for n, it := range items {
		blocks[n] = workfile.Block{Kind: workfile.BlockList, Level: it.level, Marker: it.marker, Spans: mdSpans(strings.Join(it.lines, "\n"), workfile.Span{})}
	}
	return blocks
}

func mdCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	start := 0
	for i := 0; i <= len(line); i++ {
		if i == len(line) || line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			cells = append(cells, strings.TrimSpace(spansText(mdSpans(strings.TrimSpace(line[start:i]), workfile.Span{}))))
			start = i + 1
		}
	}
	return cells
}

// mdTableHead says whether header, followed by rule, starts a table.
func mdTableHead(header, rule string) bool {
	rule = strings.TrimSpace(rule)
	if !strings.Contains(rule, "-") {
		return false
	}
	cells := mdCells(rule)
	for _, c := range cells {
		if !mdTableRule.MatchString(c) {
			return false
		}
	}
	return len(cells) == len(mdCells(header))
}

// mdParagraph is one paragraph. If it holds nothing but images, each becomes an
// image block, so the viewer can show it.
func mdParagraph(text string) []workfile.Block {
	if found := mdImage.FindAllStringSubmatch(text, -1); len(found) > 0 && strings.TrimSpace(mdImage.ReplaceAllString(text, "")) == "" {
		blocks := make([]workfile.Block, len(found))
		for i, m := range found {
			blocks[i] = workfile.Block{Kind: workfile.BlockImage, Name: mdImageName(m[1], m[2]), Attachment: m[2]}
		}
		return blocks
	}
	return []workfile.Block{{Kind: workfile.BlockParagraph, Spans: mdSpans(text, workfile.Span{})}}
}

func mdImageName(alt, address string) string {
	if alt = strings.TrimSpace(alt); alt != "" {
		return alt
	}
	if u, err := url.Parse(address); err == nil && path.Base(u.Path) != "." && path.Base(u.Path) != "/" {
		return path.Base(u.Path)
	}
	return "image"
}

func isMdPunct(c byte) bool { return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0 }

func isMdAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// mdLink reads [label](destination) with s[at] on the "[".
func mdLink(s string, at int) (label, dest string, end int, ok bool) {
	closeAt := mdMatch(s, at, '[', ']')
	if closeAt < 0 || closeAt+1 >= len(s) || s[closeAt+1] != '(' {
		return
	}
	paren := mdMatch(s, closeAt+1, '(', ')')
	if paren < 0 {
		return
	}
	fields := strings.Fields(s[closeAt+2 : paren])
	if len(fields) == 0 {
		return
	}
	dest = strings.TrimSuffix(strings.TrimPrefix(fields[0], "<"), ">")
	return s[at+1 : closeAt], dest, paren + 1, true
}

// mdMatch is the index of the bracket closing the one at s[at], or -1.
func mdMatch(s string, at int, open, closing byte) int {
	depth := 0
	for i := at; i < len(s) && i-at < mdWindow; i++ {
		switch s[i] {
		case '\\':
			i++
		case open:
			depth++
		case closing:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

func mdSpans(s string, style workfile.Span) []workfile.Span { return mdInline(s, style, 0) }

// mdSearch remembers searches that found nothing, so a text full of unclosed
// marks is read in one pass, not one pass per mark.
type mdSearch struct {
	noCode map[int]bool
	noMark map[[2]int]bool
}

// mdInline reads inline Markdown. style carries what an outer span already set.
func mdInline(s string, style workfile.Span, depth int) []workfile.Span {
	if depth > mdMaxDepth {
		style.Text = s
		return []workfile.Span{style}
	}
	search := &mdSearch{noCode: map[int]bool{}, noMark: map[[2]int]bool{}}
	var spans []workfile.Span
	var buf strings.Builder
	emit := func() {
		if buf.Len() > 0 {
			span := style
			span.Text = buf.String()
			spans = append(spans, span)
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isMdPunct(s[i+1]):
			buf.WriteByte(s[i+1])
			i += 2
		case c == '`':
			n := mdRun(s, i)
			if j := search.codeEnd(s, i+n, n); j >= 0 {
				emit()
				code := s[i+n : j]
				if len(code) > 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				span := style
				span.Text, span.Code = code, true
				spans = append(spans, span)
				i = j + n
				continue
			}
			buf.WriteString(s[i : i+n])
			i += n
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			if label, dest, end, ok := mdLink(s, i+1); ok {
				emit()
				span := style
				span.Text = mdImageName(label, dest)
				if span.Link == "" {
					span.Link = dest
				}
				spans = append(spans, span)
				i = end
				continue
			}
			buf.WriteByte(c)
			i++
		case c == '[':
			if label, dest, end, ok := mdLink(s, i); ok {
				emit()
				inner := style
				inner.Link = dest
				spans = append(spans, mdInline(label, inner, depth+1)...)
				i = end
				continue
			}
			buf.WriteByte(c)
			i++
		case c == '*' || c == '_':
			n := mdRun(s, i)
			depth2 := min(n, 2)
			if content, end, ok := search.emphasis(s, i, c, depth2); ok && (c == '*' || i == 0 || !isMdAlnum(s[i-1])) {
				emit()
				inner := style
				if depth2 == 2 {
					inner.Bold = true
				} else {
					inner.Italic = true
				}
				spans = append(spans, mdInline(content, inner, depth+1)...)
				i = end
				continue
			}
			buf.WriteString(s[i : i+n])
			i += n
		default:
			buf.WriteByte(c)
			i++
		}
	}
	emit()
	return spans
}

// mdRun is how many times s[at] repeats from at.
func mdRun(s string, at int) int {
	n := 1
	for at+n < len(s) && s[at+n] == s[at] {
		n++
	}
	return n
}

// mdCodeEnd is where a run of exactly n backticks closes a code span, or -1.
func (m *mdSearch) codeEnd(s string, from, n int) int {
	if m.noCode[n] {
		return -1
	}
	if j := mdCodeEnd(s, from, n); j >= 0 {
		return j
	}
	m.noCode[n] = true
	return -1
}

func mdCodeEnd(s string, from, n int) int {
	for j := from; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		if run := mdRun(s, j); run == n {
			return j
		} else {
			j += run
		}
	}
	return -1
}

// mdEmphasis finds the text between depth marks at s[at] and the matching marks,
// and where to carry on after them. A closing run longer than depth gives its
// extra marks to the text inside, so ***x*** is bold around italic.
func (m *mdSearch) emphasis(s string, at int, mark byte, depth int) (content string, end int, ok bool) {
	// A search that found no closer would find none from a later start either.
	if m.noMark[[2]int{int(mark), depth}] {
		return
	}
	if content, end, ok = m.emphasisFrom(s, at, mark, depth); !ok {
		m.noMark[[2]int{int(mark), depth}] = true
	}
	return
}

func (m *mdSearch) emphasisFrom(s string, at int, mark byte, depth int) (content string, end int, ok bool) {
	start := at + depth
	if start >= len(s) || s[start] == ' ' || s[start] == '\t' || s[start] == '\n' {
		return
	}
	for j := start; j < len(s); {
		switch {
		case s[j] == '\\':
			j += 2
		case s[j] == '`':
			if k := m.codeEnd(s, j+mdRun(s, j), mdRun(s, j)); k >= 0 {
				j = k + mdRun(s, j)
			} else {
				j += mdRun(s, j)
			}
		case s[j] == mark:
			run := mdRun(s, j)
			spaced := s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n'
			fits := run == depth || depth == 2 && run > 2
			wordEnd := mark == '*' || j+run >= len(s) || !isMdAlnum(s[j+run])
			if j > start && !spaced && fits && wordEnd {
				return s[start : j+run-depth], j + run, true
			}
			j += run
		default:
			j++
		}
	}
	return
}
