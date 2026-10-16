package cli

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

const barWidth = 12

func percent(n, d int) string {
	if d == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (n*100+d/2)/d)
}

// bar fills in proportion to n/d, but never shows an empty bar for a non-zero count.
func bar(n, d, width int) (filled, rest string) {
	f := 0
	if d > 0 && n > 0 {
		f = max(1, min(width, (n*width+d/2)/d))
	}
	return strings.Repeat("█", f), strings.Repeat("░", width-f)
}

type stat struct {
	label string
	n, d  int
}

func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-runewidth.StringWidth(text)))
}

func lpad(text string, width int) string {
	return strings.Repeat(" ", max(0, width-runewidth.StringWidth(text))) + text
}

// rateRows lays out label, bar, "n / d" and a percentage. Narrow terminals drop
// the bar first, then the counts, so the percentage always survives.
func (v view) rateRows(stats []stat) []row {
	labelW, countW := 0, 0
	for _, s := range stats {
		labelW = max(labelW, runewidth.StringWidth(s.label))
		countW = max(countW, len(fmt.Sprintf("%d / %d", s.n, s.d)))
	}
	const rateW = 4
	showBar, showCount := true, true
	fits := func() bool {
		w := labelW + 2 + rateW
		if showBar {
			w += barWidth + 2
		}
		if showCount {
			w += countW + 2
		}
		return w <= v.innerWidth()
	}
	if !fits() {
		showBar = false
	}
	if !fits() {
		showCount = false
	}
	var rows []row
	for _, s := range stats {
		r := row{plain(pad(s.label, labelW) + "  ")}
		if showBar {
			filled, rest := bar(s.n, s.d, barWidth)
			r = append(r, styled(filled, red), styled(rest, muted), plain("  "))
		}
		if showCount {
			r = append(r, plain(lpad(fmt.Sprintf("%d / %d", s.n, s.d), countW)+"  "))
		}
		color := green
		if s.n > 0 {
			color = red
		}
		rows = append(rows, append(r, styled(lpad(percent(s.n, s.d), rateW), color)))
	}
	return rows
}
