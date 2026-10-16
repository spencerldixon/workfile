package workfile

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// workspaceWith copies the example workspace and adds files to it.
func workspaceWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".workfile")
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = files[name] + withoutName(data)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const aliceFile = `automations:
  - id: move-when-approved
    on:
      becomes: github.approvals >= 1
    stage: review
    agent: |
      {record} on {ticket} "{title}" was just approved.
    mode: ask
  - id: announce-done
    on:
      ready: done
    when: github.labels excludes do-not-merge
    run: [./scripts/announce.sh, "{ticket}", "{to}"]
`

func TestSharedAutomationsLoadWithTheWorkspace(t *testing.T) {
	w, err := Load(workspaceWith(t, map[string]string{"policy.yml": "name: example-web\n", "automations/alice.yml": aliceFile, "automations/README.md": "ignored"}))
	if err != nil {
		t.Fatal(err)
	}
	if w.Policy.Name != "example-web" || len(w.Automations) != 1 {
		t.Fatalf("name %q, files %d", w.Policy.Name, len(w.Automations))
	}
	f := w.SharedFile("alice")
	if f == nil || f.Private || len(f.Automations) != 2 {
		t.Fatalf("alice's file: %+v", f)
	}
	a := f.Automations[0]
	if !a.PerRecord() || a.EffectiveMode() != ModeAsk || a.Guard != nil {
		t.Fatalf("parsed automation: %+v", a)
	}
	if f.Automations[1].EffectiveMode() != ModeObserve || f.Automations[1].Guard == nil {
		t.Fatal("mode defaults to observe and when is parsed")
	}
}

func TestPolicyNameDoesNotChangeThePolicyHash(t *testing.T) {
	w := example(t)
	before := w.PolicyHash()
	w.Policy.Name = "example-web"
	if w.PolicyHash() != before {
		t.Fatal("renaming a workspace must not change its policy hash")
	}
}

func TestAutomationMistakesAreErrors(t *testing.T) {
	cases := map[string]struct{ policy, file, want string }{
		"unsafe name":       {policy: "name: ../escape\n", file: aliceFile, want: "name must be"},
		"unknown person":    {file: aliceFile, want: "not a person in people.yml"},
		"two triggers":      {file: "automations:\n  - id: x\n    on: {ready: done, enters: review}\n    run: [echo]\n", want: "exactly one of ready"},
		"no trigger":        {file: "automations:\n  - id: x\n    on: {}\n    run: [echo]\n", want: "exactly one of ready"},
		"ready unreachable": {file: "automations:\n  - id: x\n    on: {ready: nowhere}\n    run: [echo]\n", want: "no move in policy.yml leads to nowhere"},
		"enters unknown":    {file: "automations:\n  - id: x\n    on: {enters: nowhere}\n    run: [echo]\n", want: "not a stage"},
		"bad expression":    {file: "automations:\n  - id: x\n    on: {becomes: github.nothing >= 1}\n    run: [echo]\n", want: "becomes:"},
		"bad stage":         {file: "automations:\n  - id: x\n    on: {enters: review}\n    stage: nowhere\n    run: [echo]\n", want: "stage: nowhere"},
		"both actions":      {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n    agent: hi\n", want: "exactly one of run or agent"},
		"no action":         {file: "automations:\n  - id: x\n    on: {enters: review}\n", want: "exactly one of run or agent"},
		"bad mode":          {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n    mode: always\n", want: "mode must be"},
		"bad scope":         {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n    scope: team\n", want: "scope must be"},
		"duplicate id":      {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n  - id: x\n    on: {enters: done}\n    run: [echo]\n", want: "used twice"},
		"record for ticket": {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo, \"{record}\"]\n", want: "{record} is not available"},
		"to without ready":  {file: "automations:\n  - id: x\n    on: {enters: review}\n    agent: \"{to}\"\n", want: "{to} is not available"},
		"repos in shared":   {file: "repos: [github.com/example-team/web]\nautomations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n", want: "repos belongs in a private file"},
		"unknown field":     {file: "automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n    retry: 3\n", want: "unknown field"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			person := "alice"
			if name == "unknown person" {
				person = "dave"
			}
			_, err := Load(workspaceWith(t, map[string]string{"policy.yml": tc.policy, "automations/" + person + ".yml": tc.file}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestYAMLExtensionIsAnError(t *testing.T) {
	_, err := Load(workspaceWith(t, map[string]string{"automations/alice.yaml": aliceFile}))
	if err == nil || !strings.Contains(err.Error(), "<person>.yml") {
		t.Fatalf("got %v", err)
	}
}

func TestPrivateFilesNeedReposAndUniqueIDs(t *testing.T) {
	root := workspaceWith(t, map[string]string{"policy.yml": "name: example-web\n", "automations/alice.yml": aliceFile})
	w, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "automations.yml")
	if f, err := w.ReadPrivateAutomations(path); f != nil || err != nil {
		t.Fatalf("a missing private file is not an error: %v %v", f, err)
	}
	os.WriteFile(path, []byte("automations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n"), 0644)
	if _, err := w.ReadPrivateAutomations(path); err == nil || !strings.Contains(err.Error(), "repos must list") {
		t.Fatalf("want a repos error, got %v", err)
	}
	os.WriteFile(path, []byte("repos: [\"not a remote\"]\nautomations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n"), 0644)
	if _, err := w.ReadPrivateAutomations(path); err == nil || !strings.Contains(err.Error(), "not a git remote") {
		t.Fatalf("want a remote error, got %v", err)
	}
	os.WriteFile(path, []byte("repos: [git@github.com:example-team/web.git]\nautomations:\n  - id: announce-done\n    on: {enters: done}\n    run: [echo]\n"), 0644)
	private, err := w.ReadPrivateAutomations(path)
	if err != nil || !private.Private {
		t.Fatal(err)
	}
	if problems := CheckUniqueIDs(w.SharedFile("alice"), private); len(problems) != 1 || !strings.Contains(problems[0], "announce-done") {
		t.Fatalf("the same id in both files is an error: %v", problems)
	}
}

func TestNormalizeRemote(t *testing.T) {
	want := "github.com/example-team/web"
	for _, remote := range []string{
		"git@github.com:example-team/web.git",
		"https://github.com/example-team/web",
		"https://example-alice:example-token@github.com/example-team/web.git",
		"ssh://git@github.com/example-team/web.git",
		"github.com/Example-Team/web/",
	} {
		if got := NormalizeRemote(remote); got != want {
			t.Errorf("%s: got %q", remote, got)
		}
	}
	for _, remote := range []string{"", "web", "../web", "/srv/git/web.git", "file:///srv/web", "github.com/../x", "not a remote"} {
		if got := NormalizeRemote(remote); got != "" {
			t.Errorf("%q should not be a remote, got %q", remote, got)
		}
	}
}

func automationFor(t *testing.T, w *Workspace, a Automation) Automation {
	t.Helper()
	f := &AutomationFile{Person: "alice", Automations: []Automation{a}}
	if problems := w.CheckAutomations(f); len(problems) > 0 {
		t.Fatal(problems)
	}
	return f.Automations[0]
}

func TestTriggerFacts(t *testing.T) {
	w := example(t)
	approved := pr("api#1", "example-alice", "low-risk")
	waiting := pr("api#2", "", "low-risk")
	review := w.Assess(ticket("review", approved, waiting))

	ready := automationFor(t, w, Automation{ID: "r", On: Trigger{Ready: "done"}, Run: []string{"echo"}})
	facts, _ := w.TriggerFacts(ready, []Assessment{review})
	if facts["APP-42"] != FactFalse {
		t.Fatalf("one PR lacks an approval, so done is not ready: %v", facts)
	}
	facts, _ = w.TriggerFacts(ready, []Assessment{w.Assess(ticket("review", approved))})
	if facts["APP-42"] != FactTrue {
		t.Fatalf("every gate for done passes: %v", facts)
	}

	enters := automationFor(t, w, Automation{ID: "e", On: Trigger{Enters: "review"}, Run: []string{"echo"}})
	facts, _ = w.TriggerFacts(enters, []Assessment{review})
	if facts["APP-42"] != FactTrue {
		t.Fatalf("enters: %v", facts)
	}

	becomes := automationFor(t, w, Automation{ID: "b", On: Trigger{Becomes: "github.approvals >= 1"}, Run: []string{"echo"}})
	facts, unread := w.TriggerFacts(becomes, []Assessment{review})
	if facts["APP-42 github api#1"] != FactTrue || facts["APP-42 github api#2"] != FactFalse || len(unread) != 0 {
		t.Fatalf("one fact per PR: %v", facts)
	}
	gone := ticket("review")
	gone.Unavailable = map[string]string{"github.prs": "GitHub is down"}
	facts, unread = w.TriggerFacts(becomes, []Assessment{w.Assess(gone)})
	if len(facts) != 0 || !slices.Equal(unread, []string{"APP-42"}) {
		t.Fatalf("unread PRs are reported, not forgotten: %v %v", facts, unread)
	}

	unknown := ticket("review", approved)
	unknown.Records["github"][0].Unavailable = map[string]string{"approvals": "GitHub is down"}
	delete(unknown.Records["github"][0].Facts, "approvals")
	facts, _ = w.TriggerFacts(becomes, []Assessment{w.Assess(unknown)})
	if facts["APP-42 github api#1"] != FactUnknown {
		t.Fatalf("missing evidence is unknown: %v", facts)
	}
}

func TestChangesFireOnceAndNeverFromUnknown(t *testing.T) {
	steps := []struct {
		now    map[string]string
		unread []string
		fired  []string
	}{
		{now: map[string]string{"A": FactTrue, "B": FactFalse}}, // first check: a starting point only
		{now: map[string]string{"A": FactTrue, "B": FactTrue}, fired: []string{"B"}},
		{now: map[string]string{"A": FactTrue, "B": FactTrue}},     // each change fires once
		{now: map[string]string{"A": FactFalse, "B": FactUnknown}}, // unknown keeps B true
		{now: map[string]string{"A": FactTrue, "B": FactTrue}, fired: []string{"A"}},
		{now: map[string]string{"C": FactTrue}},                // A and B left the search
		{now: map[string]string{"A": FactTrue, "C": FactTrue}}, // A came back: a new starting point
		{now: map[string]string{"A": FactFalse, "C": FactTrue, "D x y": FactFalse}},
		{now: map[string]string{"A": FactFalse}, unread: []string{"D"}}, // D's PRs could not be read
		{now: map[string]string{"A": FactFalse, "D x y": FactTrue}, fired: []string{"D x y"}},
	}
	saved := map[string]string{}
	for i, step := range steps {
		var fired []string
		saved, fired = Changes(saved, step.now, step.unread)
		if !slices.Equal(fired, step.fired) {
			t.Fatalf("step %d: fired %v, want %v (saved %v)", i, fired, step.fired, saved)
		}
	}
}

func TestFingerprintChangesWithTheTriggerOnly(t *testing.T) {
	a := Automation{ID: "x", On: Trigger{Enters: "review"}, Run: []string{"echo"}}
	b := a
	b.Run = []string{"true"}
	b.Mode = ModeAuto
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("changing the action keeps the starting point")
	}
	b.Stage = Names{"review"}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("changing the stage resets the starting point")
	}
}

func TestHoldsForStageAndWhen(t *testing.T) {
	w := example(t)
	a := automationFor(t, w, Automation{ID: "x", On: Trigger{Ready: "done"}, Stage: Names{"review"}, When: "github.labels excludes do-not-merge", Run: []string{"echo"}})
	ok := pr("api#1", "example-alice", "low-risk")
	if reason := w.HoldsFor(a, w.Assess(ticket("review", ok)), nil); reason != "" {
		t.Fatal(reason)
	}
	if reason := w.HoldsFor(a, w.Assess(ticket("doing", ok)), nil); !strings.Contains(reason, "only for review") {
		t.Fatal(reason)
	}
	held := pr("api#2", "example-alice", "do-not-merge")
	if reason := w.HoldsFor(a, w.Assess(ticket("review", ok, held)), nil); !strings.Contains(reason, "api#2") {
		t.Fatalf("a when about PRs must hold for every PR: %q", reason)
	}
	perPR := automationFor(t, w, Automation{ID: "y", On: Trigger{Becomes: "github.approvals >= 1"}, When: "github.labels excludes do-not-merge", Run: []string{"echo"}})
	if reason := w.HoldsFor(perPR, w.Assess(ticket("review", ok, held)), &ok); reason != "" {
		t.Fatalf("a per-PR trigger checks when on its own PR: %q", reason)
	}
}

// withoutName drops the example's name, so each test chooses its own.
func withoutName(data []byte) string {
	return regexp.MustCompile(`(?m)^name:.*\n`).ReplaceAllString(string(data), "")
}
