package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"time"

	"workfile/internal/workfile"
)

type reportCoverage struct {
	Scope         string             `json:"jira_scope"`
	User          string             `json:"user,omitempty"`
	Me            bool               `json:"me,omitempty"`
	Gate          string             `json:"gate,omitempty"`
	Stage         string             `json:"stage,omitempty"`
	Failing       bool               `json:"failing,omitempty"`
	Sample        bool               `json:"sample"`
	Limit         int                `json:"limit,omitempty"`
	RequestedKeys []string           `json:"requested_keys"`
	MissingKeys   []string           `json:"missing_keys"`
	Providers     []providerCoverage `json:"providers"`
	Boundary      string             `json:"boundary"`
}
type providerCoverage struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Organization string `json:"organization,omitempty"`
	Lookback     string `json:"lookback,omitempty"`
}
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
	Coverage      reportCoverage `json:"coverage"`
	Summary       map[string]int `json:"summary"`
	Warnings      []string       `json:"warnings"`
	Tickets       []reportTicket `json:"tickets"`
}

const coverageBoundary = "Current facts only; limited to the configured search and token permissions. GitHub includes visible, non-archived repositories and open or merged PRs updated within lookback. Readiness is not permission to merge or move work."

func coverage(w *workfile.Workspace, o options, missing []string) reportCoverage {
	c := reportCoverage{Scope: w.Providers[w.Policy.Tracker].Scope, User: o.user, Me: o.me, Gate: o.gate, Stage: o.stage, Failing: o.failing, Sample: o.limited(), RequestedKeys: append([]string{}, o.keys...), MissingKeys: append([]string{}, missing...), Providers: []providerCoverage{}, Boundary: coverageBoundary}
	if c.Sample {
		c.Limit = o.limit
	}
	for _, name := range workfile.SortedKeys(w.Providers) {
		p := w.Providers[name]
		c.Providers = append(c.Providers, providerCoverage{name, p.Kind, p.Org, p.Lookback})
	}
	return c
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

func writeReport(out io.Writer, w *workfile.Workspace, all []workfile.Assessment, o options, warnings, missing []string, code int) error {
	r := machineReport{SchemaVersion: 1, Command: o.command, PolicyHash: w.PolicyHash(), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ExitCode: code, Coverage: coverage(w, o, missing), Summary: map[string]int{"total": len(all), "cannot_check": 0, "out_of_policy": 0, "needs_work": 0, "ready": 0, "done": 0}, Warnings: append([]string{}, warnings...), Tickets: []reportTicket{}}
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
		t := reportTicket{Key: a.Ticket.Key, Title: a.Ticket.Title, URL: a.Ticket.URL, State: a.Ticket.State, Status: status, Error: a.Ticket.Error, Records: []reportRecord{}, EntryChecks: append([]workfile.Check{}, a.Earlier...), Routes: []reportRoute{}, Unmet: []workfile.Reason{}}
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
		r.Tickets = append(r.Tickets, t)
	}
	return encodeCleanJSON(out, r)
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

func (v view) coverage(w *workfile.Workspace, o options) {
	var rows []row
	rows = append(rows, textRows("Search: "+w.Providers[w.Policy.Tracker].Scope, "", muted, "", v.innerWidth())...)
	if o.limited() {
		rows = append(rows, textRows("Selection: sample of up to "+quantity(o.limit, "ticket", "tickets")+"; not the whole search.", "", muted, "", v.innerWidth())...)
	}
	if len(o.keys) > 0 {
		rows = append(rows, textRows("Selection: named tickets only.", "", muted, "", v.innerWidth())...)
	}
	if o.me {
		rows = append(rows, textRows("Selection: tickets involving you.", "", muted, "", v.innerWidth())...)
	} else if o.user != "" {
		rows = append(rows, textRows("Selection: tickets involving "+o.user+".", "", muted, "", v.innerWidth())...)
	}
	rows = append(rows, textRows("Only data visible to your tokens is checked; permissions can hide tickets and repositories.", "", muted, "", v.innerWidth())...)
	for _, name := range workfile.SortedKeys(w.Providers) {
		p := w.Providers[name]
		if p.Kind == "github" {
			rows = append(rows, textRows(name+": active repositories in "+p.Org+" · open/merged PRs updated within "+p.Lookback, "", muted, "", v.innerWidth())...)
		}
	}
	v.block(muted, styled("COVERAGE · current facts, not history", "1"), rows)
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
