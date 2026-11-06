package cli

import (
	"regexp"
	"strings"

	bubblehelp "charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// theme holds the look of the Bubble Tea screens: 16 ANSI colours so the
// person's terminal theme picks the shades, plus an optional panel background.
// With colour off every helper returns its text untouched.
type theme struct {
	bg    string // SGR parameters for the panel background, or "" for none
	color bool
}

// help is the key line at the foot of a screen, in the theme's colours.
func (t theme) help() bubblehelp.Model {
	h := bubblehelp.New()
	h.ShortSeparator = "  ·  "
	h.Styles.ShortKey = t.style(func(st lipgloss.Style) lipgloss.Style { return st })
	h.Styles.ShortDesc = t.style(func(st lipgloss.Style) lipgloss.Style { return st.Faint(true) })
	h.Styles.ShortSeparator = h.Styles.ShortDesc
	h.Styles.Ellipsis = h.Styles.ShortDesc
	return h
}

func newTheme(color bool, bg string) theme {
	if !color {
		bg = ""
	}
	return theme{bg: bg, color: color}
}

// style applies f to a fresh style when colour is on. With colour off it gives
// an empty style, which renders text as it is.
func (t theme) style(f func(lipgloss.Style) lipgloss.Style) lipgloss.Style {
	if !t.color {
		return lipgloss.NewStyle()
	}
	return f(lipgloss.NewStyle())
}

func (t theme) render(s string, f func(lipgloss.Style) lipgloss.Style) string {
	if !t.color {
		return s
	}
	return f(lipgloss.NewStyle()).Render(s)
}

func (t theme) bold(s string) string {
	return t.render(s, func(st lipgloss.Style) lipgloss.Style { return st.Bold(true) })
}
func (t theme) dim(s string) string {
	return t.render(s, func(st lipgloss.Style) lipgloss.Style { return st.Faint(true) })
}
func (t theme) good(s string) string {
	return t.render(s, func(st lipgloss.Style) lipgloss.Style { return st.Foreground(lipgloss.ANSIColor(2)) })
}
func (t theme) bad(s string) string {
	return t.render(s, func(st lipgloss.Style) lipgloss.Style { return st.Foreground(lipgloss.ANSIColor(1)) })
}
func (t theme) accent(s string) string {
	return t.render(s, func(st lipgloss.Style) lipgloss.Style { return st.Bold(true).Foreground(lipgloss.ANSIColor(6)) })
}

var sgrReset = regexp.MustCompile("\x1b\\[0?m")

// panel draws content on a soft background with room to breathe: no edge, no
// border. Every reset inside the content is followed by the background again so
// styled text does not punch holes in the panel.
func (t theme) panel(content string, width int) string {
	width = max(width, 10)
	lines := []string{""}
	lines = append(lines, strings.Split(strings.TrimRight(content, "\n"), "\n")...)
	lines = append(lines, "")
	for i, line := range lines {
		if t.bg == "" {
			lines[i] = "  " + line
			continue
		}
		lines[i] = t.onPanel("  "+line, width)
	}
	return strings.Join(lines, "\n")
}

// onPanel pads one line to width on the panel background. Without a
// background the line is only padded.
func (t theme) onPanel(line string, width int) string {
	pad := strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	if t.bg == "" {
		return line + pad
	}
	on := "\x1b[" + t.bg + "m"
	return on + sgrReset.ReplaceAllString(line, "\x1b[0m"+on) + pad + "\x1b[0m"
}

// para wraps text to a width.
func (t theme) para(text string, width int) string {
	lines := strings.Split(lipgloss.NewStyle().Width(max(width, 10)).Render(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}
