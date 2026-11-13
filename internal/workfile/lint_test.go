package workfile

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

func TestGapsFindGatesSomeRoutesSkip(t *testing.T) {
	p := Policy{
		States: []string{"todo", "fast", "slow", "done"},
		Transitions: map[string]Transition{
			"todo": {Routes: map[string][]string{"fast": nil, "slow": nil}},
			"fast": {To: []string{"done"}, Requires: []string{"review"}},
			"slow": {To: []string{"done"}},
			"done": {End: true},
		},
		Gates: map[string][]Rule{"review": nil},
	}
	gaps := p.Gaps()
	if len(gaps) != 1 || gaps[0].Stage != "done" || gaps[0].Gate != "review" ||
		len(gaps[0].Guarded) != 1 || gaps[0].Guarded[0].From != "fast" ||
		len(gaps[0].Open) != 1 || gaps[0].Open[0].From != "slow" {
		t.Fatalf("%+v", gaps)
	}
	// Requiring it on every move into done closes it.
	p.Transitions["slow"] = Transition{To: []string{"done"}, Requires: []string{"review"}}
	if gaps := p.Gaps(); len(gaps) != 0 {
		t.Fatalf("%+v", gaps)
	}
	// A gate passed earlier is not skipped later.
	p.Transitions["todo"] = Transition{Routes: map[string][]string{"fast": {"review"}, "slow": {"review"}}}
	p.Transitions["fast"] = Transition{To: []string{"done"}}
	p.Transitions["slow"] = Transition{To: []string{"done"}}
	if gaps := p.Gaps(); len(gaps) != 0 {
		t.Fatalf("%+v", gaps)
	}
}

func TestAnyAddsGatesToEveryForwardMoveIntoTheStage(t *testing.T) {
	w := branching(t)
	w.Policy.Transitions[AnyStage] = Transition{To: []string{"done"}, Requires: []string{"code_review"}}
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	// Every path to done now passes code review, wherever the move starts.
	if got := w.Policy.Guarantees()["done"]; !slices.Contains(got, "code_review") {
		t.Fatalf("done guarantees %v", got)
	}
	var into int
	for _, state := range w.Policy.States {
		for _, route := range w.Policy.Routes(state) {
			if route.To != "done" {
				continue
			}
			into++
			if !slices.Contains(route.Requires, "code_review") {
				t.Errorf("%s -> done skips code_review: %v", state, route.Requires)
			}
		}
	}
	if into == 0 {
		t.Fatal("expected moves into done")
	}
	// Returns are not entries: going back to an earlier stage is untouched.
	w.Policy.Transitions[AnyStage] = Transition{To: []string{"intake"}, Requires: []string{"code_review"}}
	for _, state := range w.Policy.States {
		for _, route := range w.Policy.Routes(state) {
			if route.To == "intake" && slices.Contains(route.Requires, "code_review") {
				t.Errorf("%s -> intake is a return and must not gain gates", state)
			}
		}
	}
}

func TestAnyDoesNotCreateMovesAndCountsAsUse(t *testing.T) {
	w := branching(t)
	before := len(w.Policy.Routes("intake"))
	w.Policy.Transitions[AnyStage] = Transition{To: []string{"done"}, Requires: []string{"code_review"}}
	if len(w.Policy.Routes("intake")) != before {
		t.Fatal("any must only add gates to moves that exist")
	}
	for _, f := range w.Policy.Lint() {
		if f.Rule == "unused-gate" && strings.Contains(f.Message, "code_review") {
			t.Fatalf("%+v", f)
		}
	}
}

func TestAnyIsValidated(t *testing.T) {
	for name, entry := range map[string]Transition{
		"unknown stage": {To: []string{"nowhere"}, Requires: []string{"code_review"}},
		"unknown gate":  {To: []string{"done"}, Requires: []string{"missing"}},
		"no gates":      {To: []string{"done"}},
		"no stages":     {Requires: []string{"code_review"}},
		"twice":         {To: []string{"done", "done"}, Requires: []string{"code_review"}},
		"routes":        {To: []string{"done"}, Requires: []string{"code_review"}, Routes: map[string][]string{"done": nil}},
		"end":           {End: true},
	} {
		w := branching(t)
		w.Policy.Transitions[AnyStage] = entry
		if err := w.validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	w := branching(t)
	w.Policy.States = append(w.Policy.States, AnyStage)
	if err := w.validate(); err == nil {
		t.Error("a stage called any must be refused")
	}
}

func TestInvariantsAreRemoved(t *testing.T) {
	w := branching(t)
	w.Policy.Invariants = map[string][]string{"done": {"code_review"}}
	err := w.validate()
	if err == nil || !strings.Contains(err.Error(), "invariants has been removed") || !strings.Contains(err.Error(), "any") {
		t.Fatalf("want a message that points at any, got %v", err)
	}
}

func TestLintFindsPlainMistakes(t *testing.T) {
	clean := branching(t).Policy
	unused := branching(t).Policy
	unused.Gates = map[string][]Rule{}
	for name, rules := range clean.Gates {
		unused.Gates[name] = rules
	}
	unused.Gates["risk_assessment_extra"] = clean.Gates["code_review"]
	for _, tc := range []struct {
		name   string
		policy Policy
		want   []Finding
	}{
		{"clean", clean, nil},
		{"unused gate", unused, []Finding{{"unused-gate", "warning", "Gate risk_assessment_extra is defined but no move requires it. Require it on a move, or remove it."}}},
	} {
		if got := tc.policy.Lint(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLoadReturnsAPolicyError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".workfile")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", "branching", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "policy.yml" {
			data = append(data, "\ninvariants:\n  done: [nope]\n"...)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Load(root)
	var pe *PolicyError
	if !errors.As(err, &pe) || len(pe.Problems) == 0 {
		t.Fatalf("want a *PolicyError, got %v", err)
	}
	if !strings.Contains(pe.Error(), "invariants has been removed") {
		t.Fatal(pe.Error())
	}
}
