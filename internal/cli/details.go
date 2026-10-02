package cli

import (
	"net/url"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func gateChecks(checks []workfile.Check, name string) []workfile.Check {
	var result []workfile.Check
	for _, check := range checks {
		if check.Gate == name {
			result = append(result, check)
		}
	}
	return result
}
func outcome(checks []workfile.Check) string {
	result := "skip"
	for _, check := range checks {
		if check.Outcome == "fail" {
			return "fail"
		}
		if check.Outcome == "pass" {
			result = "pass"
		}
	}
	return result
}
func checkCell(result string) cell {
	switch result {
	case "pass":
		return styled("✓", green)
	case "fail":
		return styled("✗", red)
	default:
		return styled("—", muted)
	}
}
func routeColor(r workfile.RouteAssessment) string {
	if r.Available() {
		return green
	}
	return red
}
func routeLabel(r workfile.RouteAssessment) string {
	if r.Backward {
		return "↩ " + r.To
	}
	return "→ " + r.To
}
func routes(a workfile.Assessment) []workfile.RouteAssessment {
	result := slices.Clone(a.Routes)
	slices.SortStableFunc(result, func(a, b workfile.RouteAssessment) int {
		if a.Backward != b.Backward {
			if a.Backward {
				return 1
			}
			return -1
		}
		return 0
	})
	return result
}

func (v view) details(w *workfile.Workspace, a workfile.Assessment) {
	width := v.innerWidth()
	v.line("")
	header := textRows(a.Ticket.Title, "", "1", "", width)
	name, _ := a.Ticket.Facts["assignee"].(string)
	if name == "" && a.Ticket.Assignee != "" {
		for _, person := range workfile.SortedKeys(w.People) {
			if w.People[person][w.Policy.Tracker] == a.Ticket.Assignee {
				name = w.DisplayName(person)
				break
			}
		}
		if name == "" {
			name = "name unavailable"
		}
	}
	assigned := "Unassigned"
	if name != "" {
		assigned = "Assigned to " + name
	}
	header = append(header, textRows(assigned, "", muted, "", width)...)
	if !v.color && a.Ticket.URL != "" {
		header = append(header, textRows(a.Ticket.URL, "", muted, "", width)...)
	}
	header = append(header, row{})
	if a.Ticket.Error == "" {
		header = append(header, v.pipeline(w.Policy, a)...)
	} else {
		header = append(header, textRows(a.Ticket.Error, "", red, "", width)...)
	}
	accent := green
	if a.Ticket.Error != "" || a.Violation() || a.Blocked() {
		accent = red
	}
	v.block(accent, cell{a.Ticket.Key, "1", a.Ticket.URL}, header)
	v.block(muted, styled("PULL REQUESTS", "1"), v.pullRequests(w, a))
	var transitions []row
	switch {
	case a.Ticket.Error != "":
		transitions = textRows("Map this ticket's Jira status in providers.yml before checking transitions.", "", red, "", width)
	case len(a.Routes) == 0:
		if a.Violation() {
			transitions = append(transitions, styledRow("Repair the unmet policy gates", red))
			transitions = append(transitions, v.groupedSteps(a, []stepSource{{checks: a.Earlier}}, true)...)
		} else {
			transitions = textRows("Complete. No next steps.", "", green, "", width)
		}
	default:
		transitions = v.matrix(a)
		transitions = append(transitions, row{}, ruleRow("Next steps", width))
		transitions = append(transitions, v.nextSteps(a, routes(a), true)...)
	}
	v.block(accent, styled("TRANSITIONS", "1"), transitions)
}
func styledRow(text, style string) row { return row{styled(text, style)} }

func (v view) matrix(a workfile.Assessment) []row {
	ordered := routes(a)
	var names []string
	for _, route := range ordered {
		for _, check := range route.Checks {
			if !slices.Contains(names, check.Gate) {
				names = append(names, check.Gate)
			}
		}
	}
	fails := func(name string) bool {
		return slices.ContainsFunc(ordered, func(r workfile.RouteAssessment) bool { return outcome(gateChecks(r.Checks, name)) == "fail" })
	}
	slices.SortStableFunc(names, func(a, b string) int {
		if fails(a) != fails(b) {
			if fails(a) {
				return -1
			}
			return 1
		}
		return 0
	})
	nameWidth := runewidth.StringWidth("From "+a.Ticket.State) + 2
	for _, name := range names {
		nameWidth = max(nameWidth, runewidth.StringWidth(human(name))+2)
	}
	nameWidth = max(nameWidth, 22)
	widths := make([]int, len(ordered))
	total := nameWidth
	for i, r := range ordered {
		widths[i] = max(8, runewidth.StringWidth(routeLabel(r))+3)
		total += widths[i]
	}
	var rows []row
	if total > v.innerWidth() {
		// A narrow terminal uses the same information as a list per destination.
		for i, r := range ordered {
			if i > 0 {
				rows = append(rows, row{})
			}
			rows = append(rows, styledRow(routeLabel(r), "1;"+routeColor(r)))
			for _, name := range names {
				checks := gateChecks(r.Checks, name)
				if len(checks) > 0 {
					c := checkCell(outcome(checks))
					rows = append(rows, row{c, plain(" " + human(name))})
				}
			}
			if len(r.Checks) == 0 {
				rows = append(rows, styledRow("No gates", muted))
			}
		}
		return rows
	}
	// Use the available width to keep destination columns evenly spaced.
	spare := v.innerWidth() - total
	for i := range widths {
		widths[i] += spare / len(ordered)
		if i < spare%len(ordered) {
			widths[i]++
		}
	}
	total = nameWidth
	for _, width := range widths {
		total += width
	}
	header := padded(styled("From "+a.Ticket.State, muted), nameWidth)
	for i, r := range ordered {
		header = append(header, centered(styled(routeLabel(r), "1;"+routeColor(r)), widths[i])...)
	}
	rows = append(rows, header, styledRow(strings.Repeat("─", total), muted))
	for _, name := range names {
		style := muted
		if fails(name) {
			style = ""
		}
		label := runewidth.Truncate(human(name), nameWidth-2, "…")
		line := padded(styled(label, style), nameWidth)
		for i, r := range ordered {
			line = append(line, centered(checkCell(outcome(gateChecks(r.Checks, name))), widths[i])...)
		}
		rows = append(rows, line)
	}
	return rows
}

func recordIdentity(record workfile.Record) (string, string) {
	repo, number, _ := strings.Cut(record.ID, "#")
	if record.Repository != "" {
		repo = record.Repository
	}
	if record.Repository == "" {
		if u, err := url.Parse(record.URL); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 4 && parts[2] == "pull" {
				repo = parts[0] + "/" + parts[1]
				number = parts[3]
			}
		}
	}
	if number != "" {
		number = "#" + number
	}
	return repo, number
}

func (v view) pullRequests(w *workfile.Workspace, a workfile.Assessment) []row {
	type entry struct {
		record                            workfile.Record
		repo, number, status, color, next string
		rank                              int
	}
	var entries []entry
	for _, instance := range workfile.SortedKeys(a.Ticket.Records) {
		for _, record := range a.Ticket.Records[instance] {
			var checks []workfile.Check
			all := append(slices.Clone(a.Earlier), a.Current...)
			for _, check := range all {
				for _, r := range check.Results {
					if r.Provider == instance && r.Record == record.ID && r.URL == record.URL {
						checks = append(checks, workfile.Check{Outcome: r.Outcome, Requirement: check.Requirement})
					}
				}
			}
			repo, number := recordIdentity(record)
			e := entry{record: record, repo: repo, number: number, status: "Not assessed", color: muted, rank: 2}
			state, _ := record.Facts["state"].(string)
			switch {
			case outcome(checks) == "fail":
				e.status, e.color, e.rank = "Gate failed", red, 0
				if slices.ContainsFunc(checks, func(c workfile.Check) bool {
					return c.Outcome == "fail" && strings.Contains(strings.ToLower(c.Requirement), "approval")
				}) {
					e.status = "Needs review"
				}
				if state == "merged" {
					e.status = "Merged; gate failed"
				}
			case state == "merged":
				e.status = "Merged"
			case outcome(checks) == "pass":
				e.status, e.color, e.rank = "Gates pass", green, 1
			}
			if state == "open" {
				e.next = "→ merge"
			}
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		message := "No linked PRs in the configured scope and lookback."
		if len(w.Providers) == 1 {
			message = "No GitHub provider configured."
		}
		return textRows(message, "", muted, "", v.innerWidth())
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(a.repo+a.number, b.repo+b.number)
	})
	repoWidth, statusWidth := len("Repository")+2, len("Status")+2
	for _, e := range entries {
		repoWidth = max(repoWidth, runewidth.StringWidth(e.repo)+2)
		statusWidth = max(statusWidth, runewidth.StringWidth(e.status)+2)
	}
	table := repoWidth+8+statusWidth+7 <= v.innerWidth()
	var rows []row
	if table {
		header := padded(styled("Repository", muted), repoWidth)
		header = append(header, padded(styled("PR", muted), 8)...)
		header = append(header, padded(styled("Status", muted), statusWidth)...)
		rows = append(rows, append(header, styled("Next", muted)))
	}
	for i, e := range entries {
		if table {
			line := padded(plain(e.repo), repoWidth)
			line = append(line, padded(cell{e.number, "1", e.record.URL}, 8)...)
			line = append(line, padded(styled(e.status, e.color), statusWidth)...)
			rows = append(rows, append(line, styled(e.next, e.color)))
		} else {
			if i > 0 {
				rows = append(rows, row{})
			}
			rows = append(rows, textRows(e.repo+" "+e.number, "", "1", e.record.URL, v.innerWidth())...)
			rows = append(rows, textRows(strings.TrimSpace(e.status+"  "+e.next), "  ", e.color, "", v.innerWidth())...)
		}
		if !v.color && e.record.URL != "" {
			rows = append(rows, textRows(e.record.URL, "", muted, "", v.innerWidth())...)
		}
	}
	return rows
}
