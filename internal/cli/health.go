package cli

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
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

func (v view) summary(r workfile.Rollup, o options, w *workfile.Workspace) []row {
	meta := quantity(r.Checked, "ticket", "tickets") + " checked"
	if o.limitSet {
		meta += fmt.Sprintf(" · sampled, limit %d", o.limit)
	}
	if o.me {
		meta += " · involving you"
	}
	if o.user != "" {
		meta += " · involving " + w.DisplayName(o.user)
	}
	rows := []row{{plain(meta)}}
	if r.Checked > 0 {
		countW := len(fmt.Sprint(max(r.InPolicy, r.OutOfPolicy)))
		line := func(icon, text, color string, n int) row {
			filled, rest := bar(n, r.Checked, 24)
			return row{styled(icon+" ", color), plain(pad(text, 14) + lpad(fmt.Sprint(n), countW) + "  " + lpad(percent(n, r.Checked), 4) + "  "), styled(filled, color), styled(rest, muted)}
		}
		rows = append(rows, line("✓", "in policy", green, r.InPolicy), line("✗", "out of policy", red, r.OutOfPolicy))
	}
	if r.Unchecked > 0 {
		rows = append(rows, row{styled("! CANNOT CHECK · "+fmt.Sprint(r.Unchecked), red)})
	}
	return rows
}

func (v view) healthSummary(w *workfile.Workspace, all []workfile.Assessment, o options) {
	r := w.Rollup(all)
	v.heading("health")
	accent := red
	if r.OutOfPolicy == 0 && r.Unchecked == 0 {
		accent = green
	}
	v.block(accent, styled("SUMMARY", "1"), v.summary(r, o, w))
	if r.OutOfPolicy == 0 {
		return
	}
	var gates, stages []stat
	var reasons []row
	countW, nameW := 0, 0
	for i, g := range r.Gates {
		countW = max(countW, len(fmt.Sprint(g.ReasonCount)))
		nameW = max(nameW, runewidth.StringWidth(fmt.Sprintf("%d. %s", i+1, human(g.Name))))
	}
	firstGate, firstStage := "", ""
	for i, g := range r.Gates {
		gates = append(gates, stat{fmt.Sprintf("%d. %s", i+1, human(g.Name)), g.Failing, g.Required})
		if g.Failing > 0 {
			if firstGate == "" {
				firstGate = g.Name
			}
			reasons = append(reasons, row{plain(pad(fmt.Sprintf("%d. %s", i+1, human(g.Name)), nameW) + "  "), styled(lpad(fmt.Sprint(g.ReasonCount), countW)+"×", red), plain("  " + g.Reason)})
		}
	}
	for i, s := range r.Stages {
		stages = append(stages, stat{fmt.Sprintf("%d. %s", i+1, s.Name), s.Failing, s.Total})
		if s.Failing > 0 && firstStage == "" {
			firstStage = s.Name
		}
	}
	v.block(red, styled("GATES · failing of tickets that must pass", "1"), v.rateRows(gates))
	v.block(red, styled("STAGES · failing of tickets in stage", "1"), v.rateRows(stages))
	v.block(red, styled("MOST COMMON REASONS", "1"), reasons)
	hint := "Drill down: wf health --gate " + firstGate
	if firstStage != "" {
		hint += " · wf health --stage " + firstStage
	}
	v.wrap(hint, "  ")
	v.line("")
}
