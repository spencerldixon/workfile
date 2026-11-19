package cli

import "strings"

const animLast = 30

var setupSpring = springFrames(animLast)

// Both sizes keep the same seven-pixel letter height. The compact mark fits a
// 40-column terminal, and the wide one has space for two-column strokes.
var setupPixel = [][7]string{
	{"      ", "      ", "##  ##", "##  ##", "######", "######", " ## ##"},
	{"      ", "      ", " #### ", "##  ##", "##  ##", "##  ##", " #### "},
	{"      ", "      ", "## ###", "###   ", "##    ", "##    ", "##    "},
	{"##    ", "##    ", "##  ##", "## ## ", "####  ", "## ## ", "##  ##"},
	{"  ####", " ##   ", "##### ", " ##   ", " ##   ", " ##   ", " ##   "},
	{" ## ", "    ", "### ", " ## ", " ## ", " ## ", "####"},
	{"### ", " ## ", " ## ", " ## ", " ## ", " ## ", "  ##"},
	{"      ", "      ", " #### ", "##  ##", "######", "##    ", " #####"},
}

var setupPixelCompact = [][7]string{
	{"    ", "    ", "#  #", "#  #", "####", "####", "#  #"},
	{"    ", "    ", " ## ", "#  #", "#  #", "#  #", " ## "},
	{"    ", "    ", "# ##", "##  ", "#   ", "#   ", "#   "},
	{"#   ", "#   ", "#  #", "# # ", "##  ", "# # ", "#  #"},
	{"  ##", " #  ", "####", " #  ", " #  ", " #  ", " #  "},
	{" # ", "   ", "## ", " # ", " # ", " # ", "###"},
	{"## ", " # ", " # ", " # ", " # ", " # ", "  #"},
	{"    ", "    ", " ## ", "#  #", "####", "#   ", " ###"},
}

func (t theme) animFrame(frame, columns int) string {
	if columns < 40 {
		return t.bold("workfile")
	}
	font := setupPixel
	if columns < 60 {
		font = setupPixelCompact
	}
	progress := setupSpring[max(0, min(frame, animLast))]
	rows := make([]string, 7)
	for y := range rows {
		letters := make([]string, len(font))
		for i, glyph := range font {
			letters[i] = glyph[y]
		}
		line := []byte(strings.Join(letters, " "))
		width := len(line)
		for x, cell := range line {
			if cell != '#' || progress == 1 {
				continue
			}
			// Twice the row gives a diagonal in cells that are taller than wide.
			age := progress*float64(width+12) - float64(x+2*y)
			if age <= 0 {
				line[x] = ' '
			} else if age < 2*float64(width+12)/animLast {
				line[x] = '.'
			}
		}
		inset := strings.Repeat(" ", (columns-width)/2)
		rows[y] = inset + t.accent(string(line))
	}
	return strings.Join(rows, "\n")
}
