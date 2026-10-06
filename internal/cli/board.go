package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func upper(name string) string { return strings.ToUpper(human(name)) }

// leftRight puts right at the end of the line when both fit.
func leftRight(left row, right cell, width int) row {
	gap := width - rowWidth(left) - runewidth.StringWidth(right.text)
	if right.text == "" || gap < 2 {
		return left
	}
	return append(left, plain(strings.Repeat(" ", gap)), right)
}

// ticketHead is one line: lead text, status icon, linked key and a truncated
// title, with an optional tag on the right.
func (v view) ticketHead(lead, icon, color string, a workfile.Assessment, tag cell) row {
	width := v.innerWidth()
	key := clean(a.Ticket.Key)
	left := row{plain(lead), styled(icon+" ", color), {key, "1", a.Ticket.URL}}
	room := width - rowWidth(left) - 2
	if tag.text != "" {
		room -= runewidth.StringWidth(tag.text) + 2
	}
	if title := runewidth.Truncate(clean(a.Ticket.Title), max(0, room), "…"); title != "" {
		left = append(left, plain("  "), cell{title, "", a.Ticket.URL})
	}
	return leftRight(left, tag, width)
}

func (v view) reasonRows(checks []workfile.Check, indent string, withGate bool) []row {
	var rows []row
	seen := map[string]bool{}
	for _, c := range checks {
		for _, r := range c.Reasons {
			text := r.Text
			if withGate {
				text = human(c.Gate) + ": " + text
			}
			if !seen[text] {
				seen[text] = true
				rows = append(rows, textRows(text, indent, "", "", v.innerWidth())...)
			}
		}
	}
	return rows
}

// stageBoxes draws one box per stage in pipeline order. Tickets that cannot be
// checked have no stage, so they come first.
func (v view) stageBoxes(w *workfile.Workspace, shown []workfile.Assessment, body func(workfile.Assessment) []row) {
	var cannot []workfile.Assessment
	byStage := map[string][]workfile.Assessment{}
	for _, a := range shown {
		if a.Ticket.Error != "" {
			cannot = append(cannot, a)
		} else {
			byStage[a.Ticket.State] = append(byStage[a.Ticket.State], a)
		}
	}
	if len(cannot) > 0 {
		var rows []row
		for i, a := range cannot {
			if i > 0 {
				rows = append(rows, row{})
			}
			rows = append(rows, v.ticketHead("", "!", red, a, cell{}))
			rows = append(rows, textRows(a.Ticket.Error, "    ", red, "", v.innerWidth())...)
		}
		v.block(red, styled("CANNOT CHECK · "+quantity(len(cannot), "ticket", "tickets"), "1"), rows)
	}
	for _, state := range w.Policy.States {
		list := byStage[state]
		if len(list) == 0 {
			continue
		}
		var rows []row
		for i, a := range list {
			if i > 0 {
				rows = append(rows, row{})
			}
			rows = append(rows, body(a)...)
		}
		accent := muted
		for _, a := range list {
			switch g := group(a); {
			case g <= 2:
				accent = red
			case g == 3 && accent != red:
				accent = green
			}
		}
		v.block(accent, styled(strings.ToUpper(state)+" · "+quantity(len(list), "ticket", "tickets"), "1"), rows)
	}
}

func (v view) statusRows(a workfile.Assessment) []row {
	checks := append(slices.Clone(a.Earlier), a.Current...)
	switch group(a) {
	case 1:
		return append([]row{v.ticketHead("", "‼", red, a, styled("OUT OF POLICY", red))}, v.reasonRows(checks, "    ", true)...)
	case 2:
		return append([]row{v.ticketHead("", "✗", red, a, cell{})}, v.reasonRows(checks, "    ", true)...)
	case 3:
		tag := cell{}
		if len(a.Next) > 0 {
			tag = styled("→ "+strings.Join(a.Next, " or "), green)
		}
		return []row{v.ticketHead("", "✓", green, a, tag)}
	}
	return []row{v.ticketHead("", "·", muted, a, cell{})}
}

type tally struct{ cannot, out, needs, ready, done int }

func count(all []workfile.Assessment) tally {
	var t tally
	for _, a := range all {
		switch group(a) {
		case 0:
			t.cannot++
		case 1:
			t.out++
		case 2:
			t.needs++
		case 3:
			t.ready++
		default:
			t.done++
		}
	}
	return t
}

func (v view) statusSummary(w *workfile.Workspace, all []workfile.Assessment, o options) []row {
	meta := quantity(len(all), "ticket", "tickets") + " checked"
	if o.limited() {
		meta += fmt.Sprintf(" · limit %d", o.limit)
	}
	rows := []row{{plain(meta)}}
	t := count(all)
	countW := len(fmt.Sprint(len(all)))
	line := func(icon, text, color string, n int) {
		if n == 0 && icon == "!" {
			return
		}
		filled, rest := bar(n, len(all), 20)
		rows = append(rows, row{styled(icon+" ", color), plain(pad(text, 16) + lpad(fmt.Sprint(n), countW) + "  " + lpad(percent(n, len(all)), 4) + "  "), styled(filled, color), styled(rest, muted)})
	}
	line("!", "cannot check", red, t.cannot)
	line("‼", "out of policy", red, t.out)
	line("✗", "needs work", red, t.needs)
	line("✓", "ready to move", green, t.ready)
	line("·", "done", muted, t.done)
	return rows
}

func (v view) pipelineRows(w *workfile.Workspace, all []workfile.Assessment) []row {
	type stage struct{ ok, bad int }
	stages := make([]stage, len(w.Policy.States))
	for _, a := range all {
		i := slices.Index(w.Policy.States, a.Ticket.State)
		if a.Ticket.Error != "" || i < 0 {
			continue
		}
		if g := group(a); g == 1 || g == 2 {
			stages[i].bad++
		} else {
			stages[i].ok++
		}
	}
	labelW, numW := 0, 1
	for i, name := range w.Policy.States {
		labelW = max(labelW, runewidth.StringWidth(fmt.Sprintf("%d. %s", i+1, name)))
		numW = max(numW, len(fmt.Sprint(stages[i].ok+stages[i].bad)))
	}
	showBar := labelW+2+(2+numW)+2+(2+numW)+2+barWidth+2+numW+8 <= v.innerWidth()
	var rows []row
	for i, name := range w.Policy.States {
		s := stages[i]
		total := s.ok + s.bad
		okColor, badColor := muted, muted
		if s.ok > 0 {
			okColor = green
		}
		if s.bad > 0 {
			badColor = red
		}
		r := row{plain(pad(fmt.Sprintf("%d. %s", i+1, name), labelW) + "  "), styled("✓ "+lpad(fmt.Sprint(s.ok), numW), okColor), plain("  "), styled("✗ "+lpad(fmt.Sprint(s.bad), numW), badColor), plain("  ")}
		if showBar {
			filled, rest := bar(s.bad, total, barWidth)
			r = append(r, styled(filled, red), styled(rest, muted), plain("  "))
		}
		rows = append(rows, append(r, styled(lpad(fmt.Sprint(total), numW)+" "+map[bool]string{true: "ticket", false: "tickets"}[total == 1], muted)))
	}
	return rows
}

func (v view) statusBoard(w *workfile.Workspace, all []workfile.Assessment, o options) {
	v.heading("status")
	t := count(all)
	accent := green
	if t.cannot+t.out+t.needs > 0 {
		accent = red
	}
	v.block(accent, styled("SUMMARY", "1"), v.statusSummary(w, all, o))
	v.block(muted, styled("PIPELINE", "1"), v.pipelineRows(w, all))
	shown := slices.DeleteFunc(slices.Clone(all), func(a workfile.Assessment) bool { return o.failing && group(a) >= 3 })
	if len(shown) == 0 {
		v.wrap("✓ No failing tickets in the selection.", "  ")
		v.line("")
		return
	}
	slices.SortStableFunc(shown, func(a, b workfile.Assessment) int { return group(a) - group(b) })
	v.stageBoxes(w, shown, v.statusRows)
}
