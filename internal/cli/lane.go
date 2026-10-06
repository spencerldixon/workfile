package cli

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// block is a lane that picks up the terminal's tinted background.
func (v view) block(accent string, title cell, rows []row) {
	v.lane(accent, title, rows, v.laneBackground())
}

// lane draws a block with a coloured edge and, when bg is set, a tinted fill
// that runs the full width. Without a background the edge and the blank line
// between lanes still set each block apart.
func (v view) lane(accent string, title cell, rows []row, bg string) {
	inner := v.innerWidth()
	lv := v
	lv.bg = bg
	line := func(r row) {
		start, end, pad := "", "", ""
		if bg != "" && v.color {
			start, end = "\x1b["+bg+"m", "\x1b[0m"
			pad = strings.Repeat(" ", max(0, inner-rowWidth(r))) + "  "
		}
		v.line(strings.TrimRight("  "+start+lv.paint(accent, "▌")+"  "+lv.render(r)+pad+end, " "))
	}
	blank := row{}
	if bg != "" {
		line(blank)
	}
	title.text = runewidth.Truncate(clean(title.text), inner, "…")
	seenURLs := map[string]bool{}
	showURLs := func(r row) {
		if v.hyperlinks() {
			return
		}
		for _, c := range r {
			if c.url == "" || seenURLs[c.url] {
				continue
			}
			seenURLs[c.url] = true
			for _, urlRow := range textRows(c.url, "", muted, "", inner) {
				line(urlRow)
			}
		}
	}
	line(row{title})
	showURLs(row{title})
	line(blank)
	for _, r := range rows {
		for _, wrapped := range splitRow(r, inner) {
			line(wrapped)
		}
		showURLs(r)
	}
	if bg != "" {
		line(blank)
	}
	v.line("")
}
