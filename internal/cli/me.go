package cli

import (
	"fmt"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

// roleOf says why a ticket involves the person: the first of assigned, PR
// author or asked to review.
func roleOf(w *workfile.Workspace, a workfile.Assessment, who map[string]string) string {
	if id := who[w.Policy.Tracker]; id != "" && a.Ticket.Assignee == id {
		return "assigned"
	}
	role := ""
	for _, instance := range workfile.SortedKeys(a.Ticket.Records) {
		id := who[instance]
		if id == "" {
			continue
		}
		for _, r := range a.Ticket.Records[instance] {
			if author, _ := r.Facts["author"].(string); author == id {
				return "PR author"
			}
			if requested, _ := r.Facts["review_requests"].([]string); slices.Contains(requested, id) {
				role = "asked to review"
			}
		}
	}
	return role
}

func (v view) ended(w *workfile.Workspace, a workfile.Assessment) bool {
	return a.Ticket.Error == "" && w.Policy.Transitions[a.Ticket.State].End
}

func reasonCount(a workfile.Assessment) int {
	n := 0
	for _, c := range append(slices.Clone(a.Earlier), a.Current...) {
		n += len(c.Reasons)
	}
	return n
}

// prChecks keeps only the fixes that belong to a PR, for a ticket the person
// is involved in but not assigned to. Fixes to the ticket itself are its
// assignee's.
func prChecks(a workfile.Assessment) []workfile.Check {
	checks := a.Current
	if a.Violation() {
		checks = a.Earlier
	}
	var result []workfile.Check
	for _, check := range checks {
		kept := check
		kept.Reasons = nil
		for _, reason := range check.Reasons {
			if reason.Record != "" && !reason.OnTicket {
				kept.Reasons = append(kept.Reasons, reason)
			}
		}
		if len(kept.Reasons) > 0 {
			result = append(result, kept)
		}
	}
	return result
}

func (v view) meBoard(w *workfile.Workspace, all []workfile.Assessment, o options, who map[string]string) {
	suffix := "you"
	if o.user != "" {
		suffix = w.DisplayName(o.user)
	}
	v.heading("status · involving " + suffix)
	bg := v.laneBackground()

	var mine, others []workfile.Assessment
	for _, a := range all {
		if v.ended(w, a) {
			continue
		}
		switch roleOf(w, a, who) {
		case "assigned":
			mine = append(mine, a)
		case "":
		default:
			others = append(others, a)
		}
	}
	if o.failing {
		mine = slices.DeleteFunc(mine, func(a workfile.Assessment) bool { return a.Ticket.Error == "" && group(a) >= 3 })
	}

	// A count strip: how much is assigned, how it is doing, and where it sits.
	t := count(mine)
	summary := row{plain(quantity(len(mine), "ticket", "tickets") + " assigned to " + map[bool]string{true: "you", false: suffix}[o.user == ""])}
	for _, part := range []struct {
		n          int
		icon, text string
		color      string
	}{{t.cannot, "!", "cannot check", red}, {t.out, "‼", "out of policy", red}, {t.needs, "✗", "need work", red}, {t.ready, "✓", "ready", green}} {
		if part.n > 0 {
			summary = append(summary, styled(fmt.Sprintf("  ·  %s %d %s", part.icon, part.n, part.text), part.color))
		}
	}
	for _, line := range splitRow(summary, v.width-4) {
		v.line("  " + v.render(line))
	}
	var strip []string
	for _, state := range w.Policy.States {
		n := 0
		for _, a := range mine {
			if a.Ticket.Error == "" && a.Ticket.State == state {
				n++
			}
		}
		if n > 0 {
			strip = append(strip, fmt.Sprintf("%s %d", state, n))
		}
	}
	if len(strip) > 0 {
		v.wrap(strings.Join(strip, " · "), "  ")
	}
	v.line("")

	// Worst first inside a lane; tickets with nothing to fix go last.
	slices.SortStableFunc(mine, func(a, b workfile.Assessment) int { return reasonCount(b) - reasonCount(a) })
	var cannot []workfile.Assessment
	byStage := map[string][]workfile.Assessment{}
	for _, a := range mine {
		if a.Ticket.Error != "" {
			cannot = append(cannot, a)
		} else {
			byStage[a.Ticket.State] = append(byStage[a.Ticket.State], a)
		}
	}
	shown := false
	if len(cannot) > 0 {
		var rows []row
		for i, a := range cannot {
			if i > 0 {
				rows = append(rows, row{})
			}
			rows = append(rows, v.ticketHead("", "!", red, a, cell{}))
			rows = append(rows, textRows(a.Ticket.Error, "      ", red, "", v.innerWidth())...)
		}
		v.lane(red, styled("CANNOT CHECK · "+quantity(len(cannot), "ticket", "tickets"), "1"), rows, bg)
		shown = true
	}
	for _, state := range w.Policy.States {
		list := byStage[state]
		if len(list) == 0 {
			continue
		}
		accent := green
		var rows []row
		for i, a := range list {
			if i > 0 {
				rows = append(rows, row{})
			}
			icon, color, tag := "✓", green, cell{}
			if len(a.Next) > 0 {
				tag = styled("→ "+strings.Join(a.Next, " or "), muted)
			}
			if g := group(a); g == 1 || g == 2 {
				icon, color, accent = "✗", red, red
				if g == 1 {
					icon = "‼"
					tag = styled("OUT OF POLICY", red)
				}
			}
			rows = append(rows, v.ticketHead("", icon, color, a, tag))
			// The same grouped fixes the single-ticket view shows.
			if len(a.Routes) > 0 && group(a) != 3 {
				rows = append(rows, v.nextSteps(a, routes(a), false)...)
			} else if a.Violation() {
				rows = append(rows, v.groupedSteps(a, []stepSource{{checks: a.Earlier}}, false)...)
			}
		}
		v.lane(accent, styled(strings.ToUpper(state)+" · "+quantity(len(list), "ticket", "tickets"), "1"), rows, bg)
		shown = true
	}

	// Work that reaches you through a PR: only what you can do about the PR,
	// grouped by PR exactly as in the single-ticket view.
	var involved []row
	for _, a := range others {
		checks := prChecks(a)
		if len(checks) == 0 {
			continue
		}
		if len(involved) > 0 {
			involved = append(involved, row{})
		}
		involved = append(involved, v.ticketHead("", "·", muted, a, styled(roleOf(w, a, who), muted)))
		involved = append(involved, v.groupedSteps(a, []stepSource{{checks: checks}}, false)...)
	}
	if len(involved) > 0 {
		v.lane(muted, styled("ALSO INVOLVES YOU", "1"), involved, bg)
		shown = true
	}
	if !shown {
		v.lane(green, styled("✓ Nothing needs you right now.", "1;"+green), nil, bg)
	}
}
