package cli

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (t theme) roundedTag(body string, fill, ink lipgloss.ANSIColor) string {
	cap := t.style(func(st lipgloss.Style) lipgloss.Style { return st.Foreground(fill) })
	middle := t.style(func(st lipgloss.Style) lipgloss.Style {
		return st.Foreground(ink).Background(fill)
	})
	return cap.Render("") + middle.Render(body) + cap.Render("")
}

// alignPair fits two styled labels and keeps the second against the right edge.
func alignPair(left, right string, width int) string {
	right = ansi.Truncate(right, max(0, width), "")
	room := max(0, width-ansi.StringWidth(right))
	if right != "" {
		room = max(0, room-2)
	}
	left = ansi.Truncate(left, room, "…")
	return left + strings.Repeat(" ", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))) + right
}
