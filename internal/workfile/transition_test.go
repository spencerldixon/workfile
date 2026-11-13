package workfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func decodeTransition(t *testing.T, source string) Transition {
	t.Helper()
	var transition Transition
	if err := yaml.Unmarshal([]byte(source), &transition); err != nil {
		t.Fatal(err)
	}
	return transition
}

func destinationWorkflow(t *testing.T, requirements string) *Workspace {
	t.Helper()
	w := example(t)
	w.Policy.States = []string{"todo", "ready", "done"}
	w.Policy.Transitions = map[string]Transition{
		"todo":  decodeTransition(t, "to: [ready, done]\nrequires: "+requirements+"\n"),
		"ready": {To: []string{"done"}}, "done": {End: true},
	}
	p := w.Providers["jira"]
	p.Statuses = map[string]Names{"todo": {"To Do"}, "ready": {"Ready"}, "done": {"Done"}}
	w.Providers["jira"] = p
	return w
}

func TestSharedAndDestinationSpecificRequires(t *testing.T) {
	for _, tc := range []struct {
		requires string
		done     bool
	}{
		{"[refinement]", false},
		{"{ready: [refinement], done: []}", true},
		{"{ready: [refinement]}", true},
	} {
		w := destinationWorkflow(t, tc.requires)
		if err := w.validate(); err != nil {
			t.Fatal(err)
		}
		tk := ticket("todo")
		tk.Facts["description"] = "short"
		a := w.Assess(tk)
		if routeTo(t, a, "ready").Available() || routeTo(t, a, "done").Available() != tc.done {
			t.Fatalf("%s: %+v", tc.requires, a)
		}
		tk.State = "done"
		if w.Assess(tk).Violation() == tc.done {
			t.Fatalf("%s: wrong entry policy", tc.requires)
		}
		tk.State = "ready"
		if !w.Assess(tk).Violation() {
			t.Fatalf("%s: ready skipped refinement", tc.requires)
		}
		if tc.done {
			ready := w.Assess(tk)
			tk.State = "done"
			r := w.Rollup([]Assessment{ready, w.Assess(tk)})
			if len(r.Gates) != 1 || r.Gates[0].Required != 1 || r.Gates[0].Failing != 1 {
				t.Fatalf("shortcut counted against refinement: %+v", r)
			}
		}
	}
}

func TestDestinationRequiresMatchRoutesAndGateOrder(t *testing.T) {
	mapped := destinationWorkflow(t, "{ready: [refinement], done: []}")
	if err := mapped.validate(); err != nil {
		t.Fatal(err)
	}
	original := mapped.Policy.Routes("todo")
	mapped.Policy.Transitions["todo"] = Transition{Routes: map[string][]string{"ready": {"refinement"}, "done": {}}}
	for i, route := range mapped.Policy.Routes("todo") {
		if route.To != original[i].To || !slices.Equal(route.Requires, original[i].Requires) {
			t.Fatal("mapping differs from routes")
		}
	}
	if !slices.Equal(mapped.Policy.GateOrder(), []string{"refinement"}) {
		t.Fatal("gate order changed")
	}
}

func TestDestinationRequiresOnReturns(t *testing.T) {
	w := example(t)
	w.Policy.Transitions["review"] = decodeTransition(t, "to: [done, todo]\nrequires:\n  done: [code_review]\n  todo: [refinement]\n")
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	tk := ticket("review", pr("api#1", "example-bob", "low-risk"))
	tk.Facts["description"] = "short"
	if routeTo(t, w.Assess(tk), "todo").Available() {
		t.Fatal("explicit return requirements skipped")
	}
	tk.Facts["description"] = strings.Repeat("a", 120)
	if !routeTo(t, w.Assess(tk), "todo").Available() {
		t.Fatal("valid return blocked by source gates")
	}
}

func TestDestinationRequiresValidation(t *testing.T) {
	for _, source := range []string{
		"to: [ready, done]\nrequires: {other: [refinement]}",
		"to: [ready, done]\nrequires: {ready: [missing]}",
		"routes: {ready: [], done: []}\nrequires: {ready: [refinement]}",
		"end: true\nrequires: {}",
	} {
		w := destinationWorkflow(t, "[]")
		w.Policy.Transitions["todo"] = decodeTransition(t, source)
		if err := w.validate(); err == nil {
			t.Fatalf("invalid transition accepted: %s", source)
		}
	}
}

func TestTransitionDecoderRemainsStrict(t *testing.T) {
	for _, source := range []string{
		"to: [ready]\nrequire: [refinement]",
		"to: [ready]\nto: [done]",
		"to: [ready]\nrequires: {ready: [], ready: [refinement]}",
		"to: [ready]\nrequires: {ready: refinement}",
		"to: [ready]\nrequires: {ready: null}",
		"to: [ready]\nrequires: refinement",
		"[ready, done]",
	} {
		path := filepath.Join(t.TempDir(), "policy.yml")
		if err := os.WriteFile(path, []byte("tracker: jira\ntransitions:\n  todo:\n    "+strings.ReplaceAll(source, "\n", "\n    ")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		var policy Policy
		if err := ReadYAML(path, &policy, false); err == nil {
			t.Fatalf("invalid YAML accepted: %s", source)
		}
	}
}
