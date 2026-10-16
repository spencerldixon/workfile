package cli

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The welcome animation: a little page checks itself line by line, smiles, and
// spells out the name. Frame animLast is the finished picture, which is also
// what plain output and narrow terminals show.
const animLast = 25

const pageInner = 16

var animBars = [3]int{8, 6, 7}

// animFrame draws frame t. Styles come from the theme, so plain output has the
// same characters without colour.
func (t theme) animFrame(frame int) string {
	frame = max(0, min(frame, animLast))
	done := frame >= animLast

	eyes := "•   •"
	switch {
	case done:
		eyes = "^   ^"
	case frame%9 == 8:
		eyes = "-   -"
	}
	mouth := "‿"
	if done {
		mouth = "◡"
	}

	edge := t.dim("│")
	row := func(content string) string {
		pad := pageInner - lipgloss.Width(content)
		return edge + content + strings.Repeat(" ", max(0, pad)) + edge
	}

	lines := []string{
		t.dim("╭" + strings.Repeat("─", pageInner) + "╮"),
		row("   " + eyes),
		row("      " + mouth),
		row(""),
	}
	for i, n := range animBars {
		progress := max(0, min(4, frame-2-i*4))
		bar := strings.Repeat("▬", n*progress/4)
		check := " "
		if progress == 4 && frame >= 2+i*4+5 {
			check = t.good("✓")
		}
		lines = append(lines, row("  "+bar+strings.Repeat(" ", 8-n*progress/4+1)+check))
	}
	lines = append(lines, t.dim("╰"+strings.Repeat("─", pageInner)+"╯"))

	letters := max(0, min(len("workfile"), frame-17))
	mark := ""
	if letters > 0 {
		mark = t.bold(strings.Join(strings.Split("workfile"[:letters], ""), " "))
	}
	if done {
		mark += "  " + t.good("✦")
	}
	lines = append(lines, "", " "+mark)

	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// wordmark is the one-line fallback for terminals too narrow for the picture.
func (t theme) wordmark() string {
	return "  " + t.bold("w o r k f i l e") + "  " + t.good("✦")
}
