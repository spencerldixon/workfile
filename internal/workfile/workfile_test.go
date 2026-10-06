package workfile

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func example(t *testing.T) *Workspace {
	t.Helper()
	w, err := Load(filepath.Join("..", "..", "example"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestExamplesAndParentDiscovery(t *testing.T) {
	for _, dir := range []string{"../../example", "../../example/jira-only", "../../example/destination-requires"} {
		if _, err := Load(dir); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".workfile"), 0755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	got, err := Find(child)
	if err != nil || got != filepath.Join(root, ".workfile") {
		t.Fatalf("Find = %s, %v", got, err)
	}
}

func TestExpressionOperators(t *testing.T) {
	cases := []struct {
		source string
		value  any
		want   bool
	}{
		{`jira.description.length >= 3`, "é猫a", true},
		{`jira.description > 3`, "é猫a", false},
		{`jira.labels contains approved`, []string{"approved"}, true},
		{`jira.labels contains_any [bug, "customer, urgent"]`, []string{"customer, urgent"}, true},
		{`jira.labels excludes blocked`, []string{"ready"}, true},
		{`jira.labels matches "*-RISK"`, []string{"medium-risk"}, true},
		{`jira.summary matches "feature/*"`, "Feature/a/b", true},
		{`github.prs == 0`, nil, true},
		{`jira.type != Story`, "Bug", true},
		{`jira.labels.count <= 1`, []string{"one", "two"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			e, err := ParseExpression(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if got := e.Evaluate(tc.value); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, source := range []string{`jira.labels contains_any approved`, `jira.labels > many`, `jira.labels matches "["`, `jira.labels.count contains x`, `jira.summary.length == abc`, `jira.labels contains_any ["open]`, `jira.x.foo == 1`, `jira.x == [a]`} {
		if _, err := ParseExpression(source); err == nil {
			t.Errorf("accepted %q", source)
		}
	}
}

func ticket(state string, records ...Record) Ticket {
	return Ticket{Key: "APP-42", State: state, Title: "Improve account settings", Facts: Facts{
		"description": strings.Repeat("a", 120), "labels": []Actor{}, "type": "Story", "headings": []string{"Acceptance Criteria"},
	}, Records: map[string][]Record{"github": records}}
}

func pr(id, reviewer, risk string) Record {
	approvals := []Actor{}
	if reviewer != "" {
		approvals = append(approvals, Actor{ID: reviewer, Value: reviewer})
	}
	return Record{ID: id, URL: "https://github.com/example-team/api/pull/1", Facts: Facts{"approvals": approvals, "labels": []string{risk}, "checks": "success"}}
}

func TestAssessmentSeparatesHealthFromNextGate(t *testing.T) {
	w := example(t)
	a := w.Assess(ticket("todo"))
	if a.Violation() || a.Blocked() || !reflect.DeepEqual(a.Next, []string{"doing"}) {
		t.Fatalf("unexpected ready assessment: %+v", a)
	}
	a = w.Assess(ticket("doing"))
	if a.Violation() || !a.Blocked() {
		t.Fatal("missing PR should block the next move, not violate an earlier gate")
	}
	a = w.Assess(ticket("review"))
	if !a.Violation() || a.Fallback != "doing" || a.Blocked() {
		t.Fatalf("missing PR at review: %+v", a)
	}
	a = w.Assess(ticket("done", pr("api#1", "", "low-risk")))
	if !a.Violation() || a.Fallback != "review" || len(a.Current) != 0 {
		t.Fatalf("unreviewed done ticket: %+v", a)
	}
	a = w.Assess(ticket("done", pr("api#1", "example-alice", "high-risk")))
	if a.Violation() || a.Blocked() {
		t.Fatal("approved done ticket should pass")
	}
}

func TestEveryPRMustPassAndByOnlyCountsNamedPeople(t *testing.T) {
	w := example(t)
	a := w.Assess(ticket("review", pr("api#1", "example-alice", "high-risk"), pr("web#2", "example-bob", "high-risk")))
	if !a.Blocked() || a.Violation() {
		t.Fatalf("unexpected: %+v", a)
	}
	last := a.Current[len(a.Current)-1]
	if last.Outcome != "fail" || len(last.Reasons) != 1 || last.Reasons[0].Record != "web#2" || !strings.Contains(last.Reasons[0].Text, "Alice") {
		t.Fatalf("wrong failure: %+v", last)
	}
	a = w.Assess(ticket("review", pr("api#1", "example-bob", "low-risk")))
	if a.Blocked() || a.Current[len(a.Current)-1].Outcome != "skip" {
		t.Fatal("low risk must skip named reviewer rule")
	}
}

func TestTrackerGuardsAndCustomMessages(t *testing.T) {
	w := example(t)
	tk := ticket("todo")
	tk.Facts["headings"] = []string{}
	a := w.Assess(tk)
	if !a.Blocked() || a.Current[2].Reasons[0].Text != "APP-42 needs an Acceptance Criteria heading" {
		t.Fatalf("unexpected %+v", a.Current)
	}
	tk.Facts["type"] = "Bug"
	if w.Assess(tk).Blocked() {
		t.Fatal("heading rule should not apply to bugs")
	}
	gate := w.Policy.Gates["code_review"]
	gate[len(gate)-1].When = `jira.labels contains security`
	w.Policy.Gates["code_review"] = gate
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	tk = ticket("review", pr("api#1", "example-bob", "low-risk"))
	tk.Facts["labels"] = []Actor{{Value: "security"}}
	if !w.Assess(tk).Blocked() {
		t.Fatal("ticket condition should apply to linked PR")
	}
}

func TestConfigRejectsInvalidRulesAndStates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Workspace)
	}{
		{"unknown gate", func(w *Workspace) {
			w.Policy.Transitions["todo"] = Transition{To: []string{"doing"}, Requires: []string{"missing"}}
		}},
		{"unknown fact", func(w *Workspace) { w.Policy.Gates["x"] = []Rule{{If: "github.unknown >= 1"}} }},
		{"missing identity", func(w *Workspace) { delete(w.People["alice"], "github") }},
		{"duplicate state", func(w *Workspace) { w.Policy.States = append(w.Policy.States, "todo") }},
		{"bad credential", func(w *Workspace) {
			p := w.Providers["github"]
			p.Auth["token"] = "secret-sentinel"
			w.Providers["github"] = p
		}},
		{"cross records", func(w *Workspace) {
			w.Providers["other"] = w.Providers["github"]
			w.Policy.Gates["x"] = []Rule{{If: "github.labels contains ok", When: "other.labels contains risk"}}
		}},
		{"unsupported actor", func(w *Workspace) {
			w.Policy.Gates["x"] = []Rule{{If: "github.labels contains ok", By: []string{"alice"}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := example(t)
			tc.change(w)
			err := w.validate()
			if err == nil {
				t.Fatal("expected config error")
			}
			if strings.Contains(err.Error(), "secret-sentinel") {
				t.Fatal("error leaked a credential")
			}
		})
	}
}

func TestStrictYAMLDoesNotEchoValues(t *testing.T) {
	for _, content := range []string{"actions: {}", "gates: secret-sentinel", "states: [todo]\n---\nstates: [done]", "tracker: a\ntracker: b"} {
		file := filepath.Join(t.TempDir(), "policy.yml")
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		var p Policy
		err := ReadYAML(file, &p, false)
		if err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatalf("unsafe or missing error: %v", err)
		}
	}
}

func TestCredentialsPrecedenceAndPermissions(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(file, []byte("# personal\nWF_TEST_SECRET='from-file'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Credentials{Path: file}
	t.Setenv("WF_TEST_SECRET", "from-env")
	if got, err := c.Get("${WF_TEST_SECRET}"); err != nil || got != "from-env" {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("WF_TEST_SECRET", "")
	if got, err := c.Get("${WF_TEST_SECRET}"); err != nil || got != "from-file" {
		t.Fatalf("%q %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	c = Credentials{Path: file}
	if _, err := c.Get("${WF_TEST_SECRET}"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("wrong permissions result: %v", err)
	}
}

func TestRuleResultsPreservePassingFailingAndSkippedPRs(t *testing.T) {
	w := example(t)
	a := w.Assess(ticket("review",
		pr("api#1", "example-alice", "high-risk"),
		pr("web#2", "example-bob", "high-risk"),
		pr("docs#3", "example-bob", "low-risk")))
	check := a.Current[len(a.Current)-1]
	if check.Outcome != "fail" || len(check.Results) != 3 {
		t.Fatalf("missing per-PR results: %+v", check)
	}
	for i, want := range []string{"pass", "fail", "skip"} {
		if check.Results[i].Outcome != want || check.Results[i].Provider != "github" {
			t.Fatalf("result %d: %+v", i, check.Results[i])
		}
	}
	if !strings.Contains(check.Results[1].Observed, "approved by example-bob") || !strings.Contains(check.Results[1].Observed, "0 qualifying approvals") {
		t.Fatal("failure lost actual reviewer evidence")
	}
	if check.Reasons[0].Action != "Get at least 1 approval from Alice." || check.Reasons[0].OnTicket {
		t.Fatalf("wrong next action: %+v", check.Reasons[0])
	}
}

func TestPRConditionCanRequireAnActionOnTheTicket(t *testing.T) {
	w := example(t)
	w.Policy.Gates["code_review"] = []Rule{{If: "jira.labels contains security-reviewed", When: "github.labels contains high-risk"}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	a := w.Assess(ticket("review", pr("api#1", "example-alice", "high-risk")))
	reason := a.Current[0].Reasons[0]
	if !reason.OnTicket || reason.Action != `Add the "security-reviewed" label.` {
		t.Fatalf("wrong action target: %+v", reason)
	}
}

func TestRollupCountsGatesWherePathRequiresThem(t *testing.T) {
	w := example(t)
	r := w.Rollup([]Assessment{
		w.Assess(ticket("todo")),
		w.Assess(ticket("review")),
		w.Assess(ticket("done", pr("api#1", "example-alice", "high-risk"))),
		{Ticket: Ticket{Key: "APP-9", Error: "Unmapped Jira status"}},
	})
	if r.Checked != 3 || r.Unchecked != 1 || r.OutOfPolicy != 1 || r.InPolicy != 2 {
		t.Fatalf("%+v", r)
	}
	if got := w.Policy.GateOrder(); got[0] != "refinement" {
		t.Fatalf("gates must follow the pipeline: %v", got)
	}
	for _, g := range r.Gates {
		if g.Name == "refinement" && (g.Required != 2 || g.Failing != 0) {
			t.Fatalf("todo ticket must not count against refinement: %+v", g)
		}
	}
	for _, g := range r.Gates {
		if g.Name == "refinement" && (len(g.Rules) == 0 || len(g.Stages) != len(w.Policy.States) || g.Stages[0].Total != 0) {
			t.Fatalf("per-rule and per-stage counts: %+v", g)
		}
	}
	if r.Stages[0].Name != "todo" || r.Stages[0].Total != 1 {
		t.Fatalf("%+v", r.Stages)
	}
}
