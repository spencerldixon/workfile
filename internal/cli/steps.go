package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// reasonTarget names what a fix applies to: the ticket, or one PR by repository
// and number.
func reasonTarget(a workfile.Assessment, reason workfile.Reason) (string, string) {
	target, link := a.Ticket.Key, a.Ticket.URL
	if reason.Record != "" && !reason.OnTicket {
		target, link = reason.Record, reason.URL
		for _, record := range a.Ticket.Records[reason.Provider] {
			if record.ID == reason.Record && record.URL == reason.URL {
				if repo, number := recordIdentity(record); repo != "" {
					target = strings.TrimSpace(repo + " " + number)
				}
			}
		}
	}
	return target, link
}

// stepSource is the set of unmet checks behind one destination.
type stepSource struct {
	label  string
	checks []workfile.Check
}

type step struct {
	target, link, action, gate string
	goes                       []string
}

// groupedSteps lists each fix once under the ticket or PR it applies to. A line
// beneath it says which destinations the fix unblocks.
//
// ticketHeading names the ticket's own fixes by its key, as the single-ticket
// view does. A list that already sits under the ticket's row says "On the
// ticket" instead of repeating the key.
func (v view) groupedSteps(a workfile.Assessment, sources []stepSource, ticketHeading bool) []row {
	var steps []*step
	for _, source := range sources {
		for _, check := range source.checks {
			for _, reason := range check.Reasons {
				target, link := reasonTarget(a, reason)
				action := strings.TrimSuffix(reason.Action, ".")
				at := slices.IndexFunc(steps, func(s *step) bool {
					return s.target == target && s.link == link && s.action == action && s.gate == check.Gate
				})
				if at < 0 {
					steps = append(steps, &step{target: target, link: link, action: action, gate: check.Gate})
					at = len(steps) - 1
				}
				if source.label != "" && !slices.Contains(steps[at].goes, source.label) {
					steps[at].goes = append(steps[at].goes, source.label)
				}
			}
		}
	}
	// The ticket's own fixes come first, then each PR in the order it appeared.
	var targets []*step
	for _, s := range steps {
		if !slices.ContainsFunc(targets, func(t *step) bool { return t.target == s.target && t.link == s.link }) {
			targets = append(targets, s)
		}
	}
	slices.SortStableFunc(targets, func(x, y *step) int {
		return map[bool]int{true: 0, false: 1}[x.target == a.Ticket.Key] - map[bool]int{true: 0, false: 1}[y.target == a.Ticket.Key]
	})
	var rows []row
	n := 0
	for _, t := range targets {
		if len(rows) > 0 {
			rows = append(rows, row{})
		}
		if t.target == a.Ticket.Key && !ticketHeading {
			rows = append(rows, row{plain("  "), styled("On the ticket", "1")})
		} else {
			rows = append(rows, row{plain("  "), {t.target, "1", t.link}})
		}
		for _, s := range steps {
			if s.target != t.target || s.link != t.link {
				continue
			}
			n++
			prefix := fmt.Sprintf("    %d. ", n)
			lines := wrapped(s.action+" — "+human(s.gate)+".", max(1, v.innerWidth()-runewidth.StringWidth(prefix)))
			for i, line := range lines {
				lead := strings.Repeat(" ", runewidth.StringWidth(prefix))
				if i == 0 {
					lead = prefix
				}
				rows = append(rows, row{styled(lead, red), styled(line, red)})
			}
			if len(sources) > 1 && len(s.goes) > 0 {
				rows = append(rows, textRows("unblocks "+strings.Join(s.goes, " · "), strings.Repeat(" ", runewidth.StringWidth(prefix)), muted, "", v.innerWidth())...)
			}
		}
	}
	return rows
}

// nextSteps shows the moves that are open now, then the blocked ones as one
// grouped list of fixes.
func (v view) nextSteps(a workfile.Assessment, ordered []workfile.RouteAssessment, ticketHeading bool) []row {
	width := v.innerWidth()
	var rows []row
	var blocked []stepSource
	var open []workfile.RouteAssessment
	for _, route := range ordered {
		if route.Available() {
			open = append(open, route)
		} else {
			blocked = append(blocked, stepSource{label: routeLabel(route), checks: route.Checks})
		}
	}
	if len(blocked) > 0 {
		rows = append(rows, v.groupedSteps(a, blocked, ticketHeading)...)
	}
	if len(open) > 0 {
		if len(rows) > 0 {
			rows = append(rows, row{})
		}
		rows = append(rows, styledRow("Ready now", "1;"+green))
		for _, route := range open {
			verb := "Move "
			if route.Backward {
				verb = "Return "
			}
			rows = append(rows, textRows(verb+a.Ticket.Key+" to "+route.To+".", "  ", green, a.Ticket.URL, width)...)
		}
	}
	return rows
}
