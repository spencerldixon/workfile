package cli

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// screen holds what every drawn-once screen needs: the view and its theme.
type screen struct {
	v view
	t theme
}

func newScreen(v view) screen { return screen{v: v, t: newTheme(v.color, v.laneBackground())} }

func (s screen) panelWidth() int { return max(14, s.v.width-len(ticketIndent)) }
func (s screen) width() int      { return s.panelWidth() - 4 }

func (s screen) wrap(text string, style func(string) string) []string {
	lines := wrapped(text, s.width())
	for i, line := range lines {
		lines[i] = style(line)
	}
	return lines
}

// indented wraps text under a hanging indent.
func (s screen) indented(text, indent string, style func(string) string) []string {
	lines := wrapped(text, max(1, s.width()-runewidth.StringWidth(indent)))
	for i, line := range lines {
		lines[i] = indent + style(line)
	}
	return lines
}

// tagged puts a tag at the right edge of a line, or under it when the line is
// too narrow for both.
func (s screen) tagged(left, tag string, styleLeft, styleTag func(string) string) []string {
	if tag == "" {
		return s.wrap(left, styleLeft)
	}
	gap := s.width() - runewidth.StringWidth(left) - runewidth.StringWidth(tag)
	if gap >= 2 {
		return []string{styleLeft(left) + strings.Repeat(" ", gap) + styleTag(tag)}
	}
	return append(s.wrap(left, styleLeft), s.indented(tag, "  ", styleTag)...)
}

func (s screen) title(text string) string { return s.t.dim(strings.ToUpper(text)) }
func (s screen) verdict(text string, ok bool) string {
	if ok {
		return s.t.good("✓ ") + s.t.bold(strings.ToUpper(text))
	}
	return s.t.bad("✗ ") + s.t.bold(strings.ToUpper(text))
}

// mark is a sentence-case title with an icon, for blocks about one thing,
// such as one check that finished.
func (s screen) mark(text string, ok bool) string {
	if ok {
		return s.t.good("✓ ") + s.t.bold(text)
	}
	return s.t.bad("✗ ") + s.t.bold(text)
}

func plainText(s string) string { return s }

func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func truncate(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
