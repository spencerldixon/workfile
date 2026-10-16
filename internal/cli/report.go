package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"time"

	"workfile/internal/workfile"
)

type reportRoute struct {
	To        string           `json:"to"`
	Backward  bool             `json:"backward"`
	Available bool             `json:"available"`
	Checks    []workfile.Check `json:"checks"`
}
type reportRecord struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Repository string `json:"repository"`
	Provider   string `json:"provider"`
}
type reportTicket struct {
	Key         string            `json:"key"`
	Title       string            `json:"title"`
	URL         string            `json:"url"`
	State       string            `json:"state"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	Records     []reportRecord    `json:"records"`
	EntryChecks []workfile.Check  `json:"entry_checks"`
	Routes      []reportRoute     `json:"routes"`
	Unmet       []workfile.Reason `json:"unmet_requirements"`
}
type machineReport struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	PolicyHash    string         `json:"policy_hash"`
	ObservedAt    string         `json:"observed_at"`
	ExitCode      int            `json:"exit_code"`
	Summary       map[string]int `json:"summary"`
	Warnings      []string       `json:"warnings"`
	Tickets       []reportTicket `json:"tickets"`
}

func selected(a workfile.Assessment, o options) bool {
	if o.failing && group(a) >= 3 {
		return false
	}
	if o.stage != "" && a.Ticket.State != o.stage {
		return false
	}
	if o.gate != "" && !a.FailsGate(o.gate) {
		checks := append(slices.Clone(a.Earlier), a.Current...)
		if !slices.ContainsFunc(checks, func(c workfile.Check) bool { return c.Gate == o.gate && c.Outcome == "unknown" }) {
			return false
		}
	}
	if o.command == "health" && o.listing() && a.Ticket.Error == "" && !a.Violation() {
		return false
	}
	return true
}

func writeReport(out io.Writer, w *workfile.Workspace, all []workfile.Assessment, o options, warnings []string, code int) error {
	r := machineReport{SchemaVersion: 1, Command: o.command, PolicyHash: w.PolicyHash(), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ExitCode: code, Summary: map[string]int{"total": len(all), "cannot_check": 0, "out_of_policy": 0, "needs_work": 0, "ready": 0, "done": 0}, Warnings: append([]string{}, warnings...), Tickets: []reportTicket{}}
	rollup := w.Rollup(all)
	r.Summary["checked"] = rollup.Checked
	r.Summary["in_policy"] = rollup.InPolicy
	statuses := []string{"cannot_check", "out_of_policy", "needs_work", "ready", "done"}
	for _, a := range all {
		status := statuses[group(a)]
		r.Summary[status]++
		if !selected(a, o) {
			continue
		}
		r.Tickets = append(r.Tickets, ticketReport(a))
	}
	return encodeCleanJSON(out, r)
}

// ticketReport is one ticket in the `wf status --json` format. Automations
// hand the same format to their actions.
func ticketReport(a workfile.Assessment) reportTicket {
	statuses := []string{"cannot_check", "out_of_policy", "needs_work", "ready", "done"}
	t := reportTicket{Key: a.Ticket.Key, Title: a.Ticket.Title, URL: a.Ticket.URL, State: a.Ticket.State, Status: statuses[group(a)], Error: a.Ticket.Error, Records: []reportRecord{}, EntryChecks: append([]workfile.Check{}, a.Earlier...), Routes: []reportRoute{}, Unmet: []workfile.Reason{}}
	for _, name := range workfile.SortedKeys(a.Ticket.Records) {
		for _, p := range a.Ticket.Records[name] {
			t.Records = append(t.Records, reportRecord{p.ID, p.URL, p.Title, p.Repository, name})
		}
	}
	checks := slices.Clone(a.Earlier)
	for _, route := range a.Routes {
		t.Routes = append(t.Routes, reportRoute{route.To, route.Backward, a.Ticket.Error == "" && route.Available(), append([]workfile.Check{}, route.Checks...)})
		checks = append(checks, route.Checks...)
	}
	for _, c := range checks {
		for _, reason := range c.Reasons {
			if !slices.Contains(t.Unmet, reason) {
				t.Unmet = append(t.Unmet, reason)
			}
		}
	}
	return t
}

// Scrub untrusted strings without exposing raw provider facts or credentials.
func encodeCleanJSON(out io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = decoder.Decode(&decoded); err != nil {
		return err
	}
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case string:
			return clean(x)
		case []any:
			for i := range x {
				x[i] = scrub(x[i])
			}
		case map[string]any:
			for key, item := range x {
				x[key] = scrub(item)
			}
		}
		return v
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(scrub(decoded))
}
func writeJSONError(out io.Writer, err error) {
	_ = encodeCleanJSON(out, struct {
		SchemaVersion int    `json:"schema_version"`
		ExitCode      int    `json:"exit_code"`
		Error         string `json:"error"`
	}{1, 2, err.Error()})
}

func (v view) evidenceReport(all []workfile.Assessment, o options) {
	for _, a := range all {
		if !selected(a, o) {
			continue
		}
		checks := slices.Clone(a.Earlier)
		for _, route := range a.Routes {
			checks = append(checks, route.Checks...)
		}
		if !o.evidence {
			checks = slices.DeleteFunc(checks, func(c workfile.Check) bool {
				return c.Outcome != "unknown" && !slices.ContainsFunc(c.Results, func(r workfile.RuleResult) bool { return r.Outcome == "unknown" })
			})
		}
		seen := map[string]bool{}
		var rows []row
		accent := muted
		for _, c := range checks {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			if c.Outcome == "fail" || c.Outcome == "unknown" {
				accent = red
			} else if c.Outcome == "pass" && accent != red {
				accent = green
			}
			icon, color := "—", muted
			switch c.Outcome {
			case "pass":
				icon, color = "✓", green
			case "fail":
				icon, color = "✗", red
			case "unknown":
				icon, color = "!", red
			}
			rows = append(rows, textRows(icon+" "+c.Requirement, "", color, "", v.innerWidth())...)
			if len(c.Results) == 0 {
				rows = append(rows, textRows("Not applied: no linked PRs. Require github.prs separately to insist on a PR.", "  ", muted, "", v.innerWidth())...)
			}
			for _, result := range c.Results {
				rows = append(rows, textRows(result.Record+" · "+result.Outcome+" · "+result.Observed, "  ", muted, "", v.innerWidth())...)
			}
		}
		if len(rows) > 0 {
			v.block(accent, cell{a.Ticket.Key + " · EVIDENCE", "1", a.Ticket.URL}, rows)
		}
	}
}
