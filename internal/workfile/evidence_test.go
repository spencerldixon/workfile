package workfile

import (
	"slices"
	"strings"
	"testing"
)

func TestUnknownEvidenceNeverPassesOrBecomesViolation(t *testing.T) {
	for _, condition := range []string{"github.checks == success", "github.draft == false", "github.fresh_approvals >= 1", "github.labels excludes blocked"} {
		t.Run(condition, func(t *testing.T) {
			w := example(t)
			w.Policy.Gates["code_review"] = []Rule{{If: condition, ID: "review-ready"}}
			if err := w.validate(); err != nil {
				t.Fatal(err)
			}
			tk := ticket("review", Record{ID: "api#1", Facts: Facts{}})
			a := w.Assess(tk)
			if a.Ticket.Error == "" || a.Violation() || a.Routes[0].Available() {
				t.Fatalf("unsafe assessment: %+v", a)
			}
			c := a.Current[0]
			if c.ID != "review-ready" || c.Outcome != "unknown" || c.Results[0].Outcome != "unknown" || len(c.Reasons) != 1 {
				t.Fatalf("missing explanation: %+v", c)
			}
			rollup := w.Rollup([]Assessment{a})
			if rollup.Unchecked != 1 || rollup.Checked != 0 {
				t.Fatalf("unknown counted as checked: %+v", rollup)
			}
		})
	}
}

func TestUnknownGuardAndFalseGuard(t *testing.T) {
	w := example(t)
	w.Policy.Gates["code_review"] = []Rule{{If: "github.checks == success", When: "github.draft == false"}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	r := Record{ID: "api#1", Facts: Facts{"draft": "true"}}
	a := w.Assess(ticket("review", r))
	if a.Ticket.Error != "" || a.Current[0].Outcome != "skip" || !strings.Contains(a.Current[0].Results[0].Observed, "Only required") {
		t.Fatalf("false guard read missing requirement: %+v", a)
	}
	delete(r.Facts, "draft")
	a = w.Assess(ticket("review", r))
	if a.Current[0].Outcome != "unknown" {
		t.Fatal("missing guard treated as false")
	}
}

func TestKnownFailureWinsOverUnknownOnAnotherPR(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		w := example(t)
		w.Policy.Gates["code_review"] = []Rule{{If: "github.checks == success"}}
		if err := w.validate(); err != nil {
			t.Fatal(err)
		}
		records := []Record{{ID: "api#1", Facts: Facts{"checks": "failure"}}, {ID: "api#2", Facts: Facts{}}}
		if reverse {
			slices.Reverse(records)
		}
		a := w.Assess(ticket("done", records...))
		if a.Ticket.Error != "" || !a.Violation() || a.Earlier[len(a.Earlier)-1].Outcome != "fail" {
			t.Fatalf("known failure hidden: %+v", a)
		}
	}
}

func TestKnownAlternativeRemainsAvailable(t *testing.T) {
	w := example(t)
	w.Policy.States = []string{"todo", "doing", "review", "qa", "done"}
	w.Policy.Transitions["review"] = Transition{Routes: map[string][]string{"qa": {}, "done": {"code_review"}}}
	w.Policy.Transitions["qa"] = Transition{To: []string{"done"}, Requires: []string{"code_review"}}
	p := w.Providers["jira"]
	p.Statuses["qa"] = Names{"QA"}
	w.Providers["jira"] = p
	w.Policy.Gates["code_review"] = []Rule{{If: "github.checks == success"}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	record := pr("api#1", "", "low-risk")
	delete(record.Facts, "checks")
	a := w.Assess(ticket("review", record))
	if a.Ticket.Error != "" || a.Blocked() || !a.Routes[0].Available() || a.Routes[1].Available() {
		t.Fatalf("known alternative hidden: %+v", a)
	}
}

func TestKnownEmptyEvidenceIsNotUnknown(t *testing.T) {
	w := example(t)
	a := w.Assess(ticket("review", pr("api#1", "", "low-risk")))
	if a.Ticket.Error != "" || a.Current[0].Outcome != "fail" {
		t.Fatalf("empty approvals should fail: %+v", a)
	}
	a = w.Assess(ticket("doing"))
	if a.Current[0].Outcome != "fail" {
		t.Fatal("known absence of PRs should fail count rule")
	}
	checks := w.checkGates([]string{"code_review"}, ticket("doing"))
	if checks[0].Outcome != "skip" || !strings.Contains(checks[0].Results[0].Observed, "No linked PRs") {
		t.Fatal("missing skip explanation")
	}
}

func TestRuleIDsAndPolicyHash(t *testing.T) {
	w := example(t)
	rules := w.Policy.Gates["code_review"]
	id := ruleID("code_review", rules[0])
	reordered := slices.Clone(rules)
	slices.Reverse(reordered)
	if ruleID("code_review", reordered[len(reordered)-1]) != id {
		t.Fatal("reordering changed ID")
	}
	rules[0].Message = "A clearer message"
	if ruleID("code_review", rules[0]) != id {
		t.Fatal("wording changed ID")
	}
	before := w.PolicyHash()
	p := w.Providers["github"]
	p.Auth["token"] = "${DIFFERENT_TOKEN}"
	w.Providers["github"] = p
	if w.PolicyHash() != before {
		t.Fatal("credentials changed policy hash")
	}
	w.Policy.Gates["code_review"] = []Rule{{ID: "same", If: "github.approvals >= 1"}, {ID: "same", If: "github.approvals >= 2"}}
	if err := w.validate(); err == nil || !strings.Contains(err.Error(), "duplicate rule ID") {
		t.Fatalf("duplicate IDs accepted: %v", err)
	}
}
