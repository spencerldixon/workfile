package cli

import (
	"fmt"
	"slices"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// gateBoxes explains one gate: how often it fails, which rule fails, and where
// the failing tickets are.
func (v view) gateBoxes(w *workfile.Workspace, all []workfile.Assessment, gate string) {
	r := w.Rollup(all)
	width := v.innerWidth()
	var g *workfile.GateStat
	for i := range r.Gates {
		if r.Gates[i].Name == gate {
			g = &r.Gates[i]
		}
	}
	title := styled(upper(gate), "1")
	switch {
	case g == nil:
		v.block(muted, title, []row{styledRow("No ticket has to pass this gate yet.", muted)})
	case g.Failing == 0:
		v.block(green, title, []row{styledRow(fmt.Sprintf("✓ All %d tickets that must pass this gate do.", g.Required), green)})
	default:
		filled, rest := bar(g.Failing, g.Required, 24)
		rows := []row{
			leftRight(row{styled(fmt.Sprintf("%d of %d", g.Failing, g.Required), red), plain(" tickets that must pass this gate fail it")}, styled(percent(g.Failing, g.Required), red), width),
			{styled(filled, red), styled(rest, muted)},
			{},
		}
		textW, countW := 0, len(fmt.Sprint(g.Failing))
		for _, rule := range g.Rules {
			textW = max(textW, runewidth.StringWidth(rule.Text))
		}
		textW = min(textW, max(10, width-barWidth-countW-4))
		for _, rule := range g.Rules {
			f, rs := bar(rule.Failing, g.Required, barWidth)
			color := red
			if rule.Failing == 0 {
				color = muted
			}
			rows = append(rows, row{plain(pad(runewidth.Truncate(rule.Text, textW, "…"), textW) + "  "), styled(f, color), styled(rs, muted), styled("  "+lpad(fmt.Sprint(rule.Failing), countW), color)})
		}
		if len(g.Rules) > 1 {
			rows = append(rows, row{}, styledRow("A ticket can fail more than one rule.", muted))
		}
		v.block(red, title, rows)
		var stages []stat
		for i, s := range g.Stages {
			if s.Total > 0 {
				stages = append(stages, stat{fmt.Sprintf("%d. %s", i+1, s.Name), s.Failing, s.Total})
			}
		}
		v.block(red, styled("WHERE THEY ARE", "1"), v.rateRows(stages))
	}
}

func (v view) healthList(w *workfile.Workspace, all []workfile.Assessment, o options) {
	name := "health"
	if o.gate != "" {
		name += " · gate " + human(o.gate)
	}
	if o.stage != "" {
		name += " · stage " + o.stage
	}
	v.heading(name)
	if len(all) == 0 {
		v.wrap("No tickets match this selection.", "  ")
		v.line("")
		return
	}
	shown := slices.DeleteFunc(slices.Clone(all), func(a workfile.Assessment) bool { return !selected(a, o) })
	if o.gate != "" {
		v.gateBoxes(w, all, o.gate)
	} else {
		meta := quantity(len(all), "ticket", "tickets") + " checked"
		if o.limited() {
			meta += fmt.Sprintf(" · limit %d", o.limit)
		}
		v.wrap(meta, "  ")
		v.line("")
		if len(shown) == 0 {
			v.wrap("✓ No policy violations in the tickets checked.", "  ")
			v.line("")
			return
		}
	}
	v.stageBoxes(w, shown, func(a workfile.Assessment) []row {
		checks := a.Earlier
		if o.gate != "" {
			checks = slices.DeleteFunc(slices.Clone(checks), func(c workfile.Check) bool { return c.Gate != o.gate })
		}
		rows := []row{v.ticketHead("", "✗", red, a, cell{})}
		rows = append(rows, v.reasonRows(checks, "    ", o.gate == "")...)
		if a.Fallback != "" && o.gate == "" {
			rows = append(rows, textRows("First unmet gate on this policy path: "+a.Fallback+".", "    ", muted, "", v.innerWidth())...)
		}
		return rows
	})
}
