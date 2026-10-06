package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestCIFixIsSharedAcrossDestinationsAndNamedOnce(t *testing.T) {
	w, tk := complexFixture(t)
	for i := range tk.Records["github"] {
		tk.Records["github"][i].Facts["approvals"] = []workfile.Actor{{ID: "example-alice", Value: "example-alice"}}
	}
	tk.Records["github"][0].Facts["checks"] = "pending"
	a := w.Assess(tk)
	for _, route := range a.Routes {
		if route.To == "qa" && route.Available() {
			t.Fatal("QA skipped required CI")
		}
	}
	text := renderDetails(w, tk, 140, false)
	if strings.Count(text, "Wait for CI to finish successfully") != 1 {
		t.Fatalf("CI fix repeated or missing: %s", text)
	}
	_, tail, _ := strings.Cut(text, "Wait for CI to finish successfully")
	_, destinations, _ := strings.Cut(tail, "unblocks")
	destinations, _, _ = strings.Cut(destinations, "\n")
	if !strings.Contains(destinations, "→ qa") || !strings.Contains(destinations, "→ release") || strings.Contains(destinations, "→ security") {
		t.Fatalf("wrong CI destinations: %s", destinations)
	}
}

func TestCodeReviewCIOutputAndJSON(t *testing.T) {
	for _, tc := range []struct {
		status, action string
		code           int
	}{
		{"success", "", 0},
		{"pending", "Wait for CI to finish successfully", 1},
		{"failure", "Fix the failing CI checks, then rerun them", 1},
		{"none", "Run CI for this PR's current commit", 1},
		{"unknown", "Restore access to the missing evidence", 2},
	} {
		t.Run(tc.status, func(t *testing.T) {
			tk := fixtureTicket("APP-42", "review", strings.Repeat("x", 120))
			record := workfile.Record{ID: "api#1", URL: "https://github.com/example-team/api/pull/1", Facts: workfile.Facts{"state": "open", "labels": []string{"low-risk"}, "approvals": []workfile.Actor{{ID: "example-alice", Value: "example-alice"}}}}
			if tc.status != "unknown" {
				record.Facts["checks"] = tc.status
			}
			tk.Records = map[string][]workfile.Record{"github": {record}}
			f := &fakeSource{tickets: []workfile.Ticket{tk}}
			code, out, errOut := invoke(t, f, "status", "APP-42", "--json")
			var report machineReport
			if code != tc.code || errOut != "" || json.Unmarshal([]byte(out), &report) != nil {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
			checks := report.Tickets[0].Routes[0].Checks
			at := slices.IndexFunc(checks, func(c workfile.Check) bool { return c.Condition.Fact == "checks" })
			if at < 0 || checks[at].Requirement != "Passing CI on the current commit" || report.Tickets[0].Routes[0].Available != (tc.status == "success") {
				t.Fatalf("CI contract lost: %s", out)
			}
			if tc.status == "success" {
				return
			}
			t.Setenv("NO_COLOR", "1")
			for _, width := range []int{40, 60} {
				t.Setenv("COLUMNS", fmt.Sprint(width))
				code, out, errOut = invoke(t, f, "status", "APP-42")
				if code != tc.code || errOut != "" || strings.Contains(out, "\x1b") {
					t.Fatalf("%d %s %s", code, out, errOut)
				}
				if !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(out, "▌", "")), " "), tc.action) {
					t.Fatalf("CI action missing: %s", out)
				}
				for _, line := range strings.Split(out, "\n") {
					if rowWidth(row{plain(line)}) > width {
						t.Fatalf("CI output exceeds %d: %q", width, line)
					}
				}
			}
		})
	}
}
