package workfile

import (
	"slices"
	"testing"
)

func moves(path []Move) []string {
	var names []string
	for _, m := range path {
		names = append(names, m.From+"→"+m.To)
	}
	return names
}

func TestGuaranteesKeepOnlyGatesOnEveryPath(t *testing.T) {
	g := branching(t).Policy.Guarantees()
	reviewed := []string{"refinement", "linked_pull_requests", "risk_assessment"}
	for _, tc := range []struct {
		stage string
		gates []string
	}{
		{"intake", nil},
		{"ready", nil},
		{"doing", []string{"refinement"}},
		{"blocked", []string{"refinement"}},
		{"review", reviewed},
		{"qa", append(slices.Clone(reviewed), "code_review")},
		{"security", append(slices.Clone(reviewed), "threat_model")},
		// Three paths reach release, and they share only what review needed.
		{"release", reviewed},
		{"done", append(slices.Clone(reviewed), "merged")},
	} {
		if got := g[tc.stage]; !slices.Equal(got, tc.gates) {
			t.Errorf("%s guarantees %v, want %v", tc.stage, got, tc.gates)
		}
	}
}

func TestProveFindsTheShortestBypassAndTheMovesThatCloseIt(t *testing.T) {
	p := branching(t).Policy
	p.Invariants = map[string][]string{"done": {"code_review", "merged"}, "qa": {"code_review"}}
	results := p.Prove()
	if len(results) != 3 {
		t.Fatalf("got %d results", len(results))
	}
	// Results follow the pipeline: qa before done, then the listed gate order.
	if r := results[0]; r.Stage != "qa" || !r.Holds || r.Bypass != nil {
		t.Fatalf("qa: %+v", r)
	}
	broken := results[1]
	if broken.Stage != "done" || broken.Gate != "code_review" || broken.Holds {
		t.Fatalf("done: %+v", broken)
	}
	want := []string{"intake→triage", "triage→discovery", "discovery→ready", "ready→doing", "doing→review", "review→security", "security→release", "release→done"}
	if got := moves(broken.Bypass); !slices.Equal(got, want) {
		t.Fatalf("bypass %v", got)
	}
	if got := broken.Bypass[5].Requires; !slices.Equal(got, []string{"threat_model"}) {
		t.Fatalf("review→security requires %v", got)
	}
	// review→done and release→done also lie on reviewed paths, so requiring
	// code review there would be a second review, not a fix.
	if got := moves(broken.Fixes); !slices.Equal(got, []string{"review→security", "security→release"}) {
		t.Fatalf("fixes %v", got)
	}
	if r := results[2]; r.Gate != "merged" || !r.Holds {
		t.Fatalf("merged: %+v", r)
	}
}

func TestProveSuggestsNoSingleFixForSeparateBypasses(t *testing.T) {
	p := Policy{
		States: []string{"todo", "fast", "slow", "done"},
		Transitions: map[string]Transition{
			"todo": {Routes: map[string][]string{"fast": nil, "slow": nil, "done": {"review"}}},
			"fast": {To: []string{"done"}},
			"slow": {To: []string{"done"}},
			"done": {End: true},
		},
		Gates:      map[string][]Rule{"review": nil},
		Invariants: map[string][]string{"done": {"review"}},
	}
	r := p.Prove()[0]
	if r.Holds || !slices.Equal(moves(r.Bypass), []string{"todo→fast", "fast→done"}) || r.Fixes != nil {
		t.Fatalf("%+v", r)
	}
	// Making both ways round require it closes the hole.
	p.Transitions["fast"] = Transition{To: []string{"done"}, Requires: []string{"review"}}
	p.Transitions["slow"] = Transition{To: []string{"done"}, Requires: []string{"review"}}
	if r := p.Prove()[0]; !r.Holds {
		t.Fatalf("%+v", r)
	}
}

func TestTicketsStartBeforeAnyGate(t *testing.T) {
	p := branching(t).Policy
	p.Invariants = map[string][]string{"intake": {"refinement"}}
	r := p.Prove()[0]
	if r.Holds || len(r.Bypass) != 0 || r.Bypass == nil {
		t.Fatalf("the first stage needs an empty, non-nil bypass: %+v", r)
	}
}

func TestInvariantsAreCheckedAndLeftOutOfTheHash(t *testing.T) {
	w := branching(t)
	before := w.PolicyHash()
	w.Policy.Invariants = map[string][]string{"done": {"code_review"}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	if w.PolicyHash() != before {
		t.Fatal("invariants describe the rules; they must not change the hash")
	}
	for _, bad := range []map[string][]string{
		{"nowhere": {"code_review"}},
		{"done": {"missing"}},
		{"done": {"merged", "merged"}},
		{"done": {}},
	} {
		w.Policy.Invariants = bad
		if err := w.validate(); err == nil {
			t.Errorf("%v: want an error", bad)
		}
	}
}
