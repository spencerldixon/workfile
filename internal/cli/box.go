package cli

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const (
	green = "32"
	red   = "31"
	muted = "2"
)

// Layout uses plain text cells. Styling and terminal links are applied last.
type cell struct{ text, style, url string }
type row []cell

func plain(text string) cell         { return cell{text: clean(text)} }
func styled(text, style string) cell { return cell{text: clean(text), style: style} }
func rowWidth(r row) int {
	n := 0
	for _, c := range r {
		n += runewidth.StringWidth(c.text)
	}
	return n
}
func padded(c cell, width int) row {
	return row{c, plain(strings.Repeat(" ", max(0, width-runewidth.StringWidth(c.text))))}
}
func centered(c cell, width int) row {
	left := max(0, (width-runewidth.StringWidth(c.text))/2)
	return append(row{plain(strings.Repeat(" ", left))}, padded(c, width-left)...)
}
func (v view) innerWidth() int { return max(6, v.width-10) }

func textRows(text, indent, style, url string, width int) []row {
	var rows []row
	for _, line := range wrapped(text, max(1, width-runewidth.StringWidth(indent))) {
		rows = append(rows, row{plain(indent), {text: line, style: style, url: url}})
	}
	return rows
}

func splitRow(r row, width int) []row {
	rows := []row{{}}
	used := 0
	for _, c := range r {
		part := c
		part.text = ""
		for _, char := range clean(c.text) {
			size := runewidth.RuneWidth(char)
			if used+size > width {
				if part.text != "" {
					rows[len(rows)-1] = append(rows[len(rows)-1], part)
					part.text = ""
				}
				rows = append(rows, row{})
				used = 0
			}
			part.text += string(char)
			used += size
		}
		if part.text != "" {
			rows[len(rows)-1] = append(rows[len(rows)-1], part)
		}
	}
	return rows
}

func (v view) render(r row) string {
	var b strings.Builder
	for _, c := range r {
		text := c.text
		if c.url != "" {
			text = v.link(text, c.url)
		}
		if c.style != "" {
			text = v.paint(c.style, text)
		}
		b.WriteString(text)
	}
	return b.String()
}

func ruleRow(title string, width int) row {
	return row{styled(title+" ", "1"), styled(strings.Repeat("─", max(0, width-runewidth.StringWidth(title)-1)), muted)}
}
func human(name string) string { return strings.ReplaceAll(name, "_", " ") }
