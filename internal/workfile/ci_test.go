package workfile

import (
	"slices"
	"strings"
	"testing"
)

func TestCodeReviewRequiresPassingCIOnEveryPR(t *testing.T) {
	for _, status := range []string{"success", "pending", "failure", "none", "unknown"} {
		t.Run(status, func(t *testing.T) {
			w := example(t)
			record := pr("api#1", "example-alice", "low-risk")
			if status == "unknown" {
				delete(record.Facts, "checks")
			} else {
				record.Facts["checks"] = status
			}
			a := w.Assess(ticket("review", record))
			at := slices.IndexFunc(a.Current, func(c Check) bool { return c.Condition.Fact == "checks" })
			if at < 0 {
				t.Fatal("code_review has no CI rule")
			}
			check := a.Current[at]
			if check.Requirement != "Passing CI on the current commit" {
				t.Fatalf("unclear CI requirement: %s", check.Requirement)
			}
			if status == "success" {
				if a.Ticket.Error != "" || a.Blocked() || check.Outcome != "pass" || !routeTo(t, a, "done").Available() {
					t.Fatalf("passing CI blocked: %+v", a)
				}
			} else if status == "unknown" {
				if a.Ticket.Error == "" || check.Outcome != "unknown" || routeTo(t, a, "done").Available() {
					t.Fatal("unavailable CI allowed completion")
				}
			} else {
				if a.Ticket.Error != "" || !a.Blocked() || check.Outcome != "fail" || a.Violation() || routeTo(t, a, "done").Available() {
					t.Fatalf("non-passing CI allowed completion: %+v", a)
				}
				if len(check.Reasons) != 1 || check.Reasons[0].Action != ciNextAction(status) {
					t.Fatalf("wrong fix: %+v", check)
				}
				record.Facts["checks"] = status
				if !w.Assess(ticket("done", record)).Violation() {
					t.Fatal("finished ticket with non-passing CI was in policy")
				}
			}
		})
	}
	w := example(t)
	passing := pr("api#1", "example-alice", "low-risk")
	failing := pr("web#2", "example-alice", "low-risk")
	failing.Facts["checks"] = "failure"
	a := w.Assess(ticket("review", passing, failing))
	check := a.Current[slices.IndexFunc(a.Current, func(c Check) bool { return c.Condition.Fact == "checks" })]
	if len(check.Results) != 2 || check.Results[0].Outcome != "pass" || check.Results[1].Outcome != "fail" || len(check.Reasons) != 1 || check.Reasons[0].Record != "web#2" {
		t.Fatalf("CI did not check each linked PR: %+v", check)
	}
}

func TestCIFixesAndEvidence(t *testing.T) {
	w := example(t)
	for _, tc := range []struct{ status, action, evidence string }{
		{"pending", "Wait for CI to finish successfully.", "CI is still running on the current commit"},
		{"failure", "Fix the failing CI checks, then rerun them.", "CI failed on the current commit"},
		{"none", "Run CI for this PR's current commit.", "No CI checks reported on the current commit"},
	} {
		record := pr("api#1", "example-alice", "low-risk")
		record.Facts["checks"] = tc.status
		checks := w.checkGates([]string{"code_review"}, ticket("review", record))
		check := checks[slices.IndexFunc(checks, func(c Check) bool { return c.Condition.Fact == "checks" })]
		if check.Results[0].Observed != tc.evidence || check.Reasons[0].Text != "api#1: "+tc.evidence || check.Reasons[0].Action != tc.action {
			t.Fatalf("%s: %+v", tc.status, check)
		}
	}
}

func TestConditionalCIAndCustomMessagesRemainSupported(t *testing.T) {
	w := example(t)
	w.Policy.Gates["code_review"] = []Rule{{If: "github.checks == success", When: "github.labels contains high-risk", Message: "Repair CI for {record}."}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	r := pr("api#1", "example-alice", "low-risk")
	delete(r.Facts, "checks")
	a := w.Assess(ticket("review", r))
	if a.Current[0].Outcome != "skip" || a.Ticket.Error != "" {
		t.Fatal("false CI guard read missing evidence")
	}
	r.Facts["labels"] = []string{"high-risk"}
	r.Facts["checks"] = "failure"
	a = w.Assess(ticket("review", r))
	if a.Current[0].Reasons[0].Action != "Repair CI for api#1." || !strings.Contains(a.Current[0].Results[0].Observed, "CI failed") {
		t.Fatal("CI lost custom message or evidence")
	}
}
