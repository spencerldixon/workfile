package workfile

import (
	"slices"
	"testing"
)

func branching(t *testing.T) *Workspace {
	t.Helper()
	w, err := Load("../../example/branching")
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func routeTo(t *testing.T, a Assessment, to string) RouteAssessment {
	t.Helper()
	for _, r := range a.Routes {
		if r.To == to {
			return r
		}
	}
	t.Fatalf("missing route to %s", to)
	return RouteAssessment{}
}
func TestDestinationGatesAndValidReturn(t *testing.T) {
	w := branching(t)
	tk := ticket("review", pr("api#1", "example-bob", "high-risk"))
	tk.Facts["headings"] = []string{"Acceptance Criteria", "Threat Model", "Rework"}
	a := w.Assess(tk)
	if a.Violation() || a.Blocked() {
		t.Fatal("security is a valid forward route")
	}
	for _, tc := range []struct {
		to    string
		ready bool
	}{{"qa", false}, {"release", false}, {"security", true}, {"doing", true}} {
		r := routeTo(t, a, tc.to)
		if r.Available() != tc.ready {
			t.Fatalf("%s readiness = %v", tc.to, r.Available())
		}
	}
	release := routeTo(t, a, "release")
	var failedGates []string
	for _, c := range release.Checks {
		if c.Outcome == "fail" {
			failedGates = append(failedGates, c.Gate)
		}
	}
	if !slices.Equal(failedGates, []string{"code_review", "release_waiver"}) {
		t.Fatalf("release lost blockers: %v", failedGates)
	}
	tk.Facts["headings"] = []string{"Acceptance Criteria"}
	a = w.Assess(tk)
	if !a.Blocked() || routeTo(t, a, "doing").Available() {
		t.Fatal("return must enforce its own gate")
	}
}
func TestBranchHealthDoesNotRequireUnvisitedPaths(t *testing.T) {
	w := branching(t)
	tk := ticket("release", pr("api#1", "example-bob", "low-risk"))
	tk.Facts["headings"] = []string{"Acceptance Criteria", "Threat Model"}
	tk.Facts["labels"] = []Actor{{Value: "security-approved"}}
	a := w.Assess(tk)
	if a.Violation() {
		t.Fatal("security path must not require code review, a waiver or QA signoff")
	}
	tk.Facts["labels"] = []Actor{}
	if !w.Assess(tk).Violation() {
		t.Fatal("no valid incoming path must be a violation")
	}
	tk.State = "review"
	a = w.Assess(tk)
	if a.Violation() {
		t.Fatal("later branches must not apply before entering them")
	}
}
func TestReturnCanRepairViolationWithoutDemandingSourceGates(t *testing.T) {
	w := branching(t)
	tk := ticket("review") // no linked PR; review is unjustified
	tk.Facts["headings"] = []string{"Acceptance Criteria", "Rework"}
	a := w.Assess(tk)
	if !a.Violation() || !routeTo(t, a, "doing").Available() || routeTo(t, a, "qa").Available() {
		t.Fatal("return should require destination prerequisites, not unmet source prerequisites")
	}
	if a.Fallback != "doing" {
		t.Fatalf("health hint must name the first unmet gate's stage, not an unrelated branch: %s", a.Fallback)
	}
	tk.State = "blocked"
	a = w.Assess(tk)
	if len(a.Routes) != 1 || !a.Routes[0].Available() || a.Blocked() {
		t.Fatal("a return-only state is not terminal or stuck")
	}
}
func TestRouteValidation(t *testing.T) {
	for _, change := range []func(*Workspace){
		func(w *Workspace) {
			v := w.Policy.Transitions["review"]
			v.To = []string{"done"}
			w.Policy.Transitions["review"] = v
		},
		func(w *Workspace) { w.Policy.Transitions["review"].Routes["missing"] = nil },
		func(w *Workspace) { w.Policy.Transitions["review"].Routes["qa"] = []string{"missing"} },
		func(w *Workspace) { w.Policy.Transitions["review"].Routes["review"] = nil },
		func(w *Workspace) { w.Policy.Transitions["intake"] = Transition{End: true} },
	} {
		w := branching(t)
		change(w)
		if err := w.validate(); err == nil {
			t.Fatal("invalid graph accepted")
		}
	}
}
