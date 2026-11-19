package cli

import (
	"fmt"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

// Leave room for the message when replies have several levels of nesting.
const minCommentWidth = 24

func (p ticketPreview) comments(d workfile.Detail) []ticketBlock {
	if len(d.Comments) == 0 {
		return []ticketBlock{{lines: []string{p.t.dim("No comments.")}}}
	}
	shown := countComments(d.Comments)
	total := max(shown, d.CommentTotal)
	heading := []string{p.t.bold(fmt.Sprintf("Comments (%d)", total))}
	if shown < total {
		heading = append(heading, fit(p.t.dim(fmt.Sprintf("Showing %d of %d. Press o to read them all.", shown, total)), "", "", p.textWidth())...)
	}
	blocks := []ticketBlock{{lines: heading}}
	comments := d.Comments
	if len(comments) > 1 && !slices.ContainsFunc(comments, func(c workfile.Comment) bool { return len(c.Replies) > 0 }) {
		comments = slices.Clone(comments)
		slices.SortStableFunc(comments, func(a, b workfile.Comment) int { return b.Created.Compare(a.Created) })
	}
	for _, c := range comments {
		blocks = p.appendComment(blocks, c, 0)
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

func (p ticketPreview) commentSize(depth int) (indent, width int) {
	// The panel spans the preview minus its left gutter, with two cells of
	// padding on either side of its content.
	width = p.textWidth() - 2
	if depth == 0 {
		return 0, width
	}
	width -= 2 // reply icon and its following space
	indent = max(0, min(min(depth, 4)*3, width-minCommentWidth))
	return indent, width - indent
}

func (p ticketPreview) appendComment(blocks []ticketBlock, c workfile.Comment, depth int) []ticketBlock {
	indent, width := p.commentSize(depth)
	icon, bodyIndent := "", ""
	if depth > 0 {
		icon = "↳"
		if p.t.color {
			icon = "󰑀"
		}
		icon = p.t.accent(icon) + " "
		bodyIndent = "  "
	}
	author := strings.TrimSpace(clean(c.Author))
	if author == "" {
		author = "Unknown author"
	}
	meta := p.t.bold(author)
	if when := ago(c.Created, p.now); when != "" {
		meta += p.t.dim(" · " + when)
	}
	lines := append(fit(meta, "", "", width), p.blocks(c.Body, width)...)
	for i, line := range lines {
		prefix := bodyIndent
		if i == 0 {
			prefix = icon
		}
		lines[i] = prefix + line
	}
	panelWidth := p.textWidth() + 2 - indent
	panel := strings.Split(p.panelTheme.panel(strings.Join(lines, "\n"), panelWidth), "\n")
	lead := strings.Repeat(" ", indent)
	for i := range panel {
		panel[i] = lead + panel[i]
	}
	blocks = append(blocks, ticketBlock{lines: panel})
	for _, reply := range c.Replies {
		blocks = p.appendComment(blocks, reply, depth+1)
	}
	return blocks
}
