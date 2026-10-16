package cli

import (
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
