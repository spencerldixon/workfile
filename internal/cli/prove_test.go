package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

// branchingWorkspace copies the branching example and adds invariants.
func branchingWorkspace(t *testing.T, invariants string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", "branching", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "policy.yml" {
			data = append(data, invariants...)
		}
		path := filepath.Join(root, ".workfile", name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const reviewedDone = "\ninvariants:\n  done: [code_review, merged]\n"

func TestProveShowsTheBypassAndHowToCloseIt(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	f := &fakeSource{}
	code, out, _ := invoke(t, f, "prove", "--dir", root)
	if code != 1 {
		t.Fatalf("%d %s", code, out)
	}
	for _, want := range []string{
		"1 of 2 invariants can be skipped.",
		"✗ done requires code review",
		"✓ done requires merged",
		"review → security → release → done",
		"Require code review on review → security or security → release.",
		"WHAT EACH STAGE GUARANTEES",
	} {
		if !strings.Contains(strings.ToUpper(out), strings.ToUpper(want)) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	assertPlain(t, out)
	if f.tested != nil || f.keys != nil {
		t.Fatal("prove must not read a provider")
	}
}

func TestProveJSONForAgents(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	code, out, _ := invoke(t, &fakeSource{}, "prove", "--dir", root, "--json")
	var report proveReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if code != 1 || report.ExitCode != 1 || report.Command != "prove" || len(report.Invariants) != 2 {
		t.Fatalf("%d %+v", code, report)
	}
	broken := report.Invariants[0]
	if broken.Holds || len(broken.Bypass) != 8 || len(broken.Fixes) != 2 || broken.Fixes[0].From != "review" {
		t.Fatalf("%+v", broken)
	}
	if report.Guarantees[0].Stage != "intake" || report.Guarantees[0].Gates == nil {
		t.Fatalf("guarantees must follow the pipeline, with empty lists not null: %+v", report.Guarantees[0])
	}
	if !strings.HasPrefix(report.PolicyHash, "sha256:") {
		t.Fatal(report.PolicyHash)
	}
}

func TestProveWithoutInvariantsStillReportsGuarantees(t *testing.T) {
	code, out, _ := invoke(t, &fakeSource{}, "prove")
	if code != 0 || !strings.Contains(out, "No invariants yet") || strings.Contains(out, "INVARIANTS\n") {
		t.Fatalf("%d %s", code, out)
	}
	root := branchingWorkspace(t, "\ninvariants:\n  done: [merged]\n")
	if code, out, _ := invoke(t, &fakeSource{}, "prove", "--dir", root); code != 0 || !strings.Contains(out, "Every invariant holds. 1 invariant checked.") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestProveFitsNarrowTerminals(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, &fakeSource{}, "prove", "--dir", root)
		assertWidth(t, out, width)
		assertPlain(t, out)
	}
}

func TestProveModelFollowsTheWindow(t *testing.T) {
	w, err := workfile.Load(branchingWorkspace(t, reviewedDone))
	if err != nil {
		t.Fatal(err)
	}
	for _, color := range []bool{true, false} {
		for _, width := range []int{40, 60} {
			m := newProveModel(view{width: 100, color: color}, w, w.Policy.Prove())
			m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
			out := m.render()
			assertWidth(t, ansi.Strip(out), width)
			if !color {
				assertPlain(t, out)
			}
			// Names are never split across lines.
			if !strings.Contains(out, "risk assessment") || !strings.Contains(out, "security") {
				t.Fatalf("%d columns:\n%s", width, out)
			}
		}
	}
	t.Setenv("COLUMNS", "100")
	_, wide, _ := invoke(t, &fakeSource{}, "prove", "--dir", branchingWorkspace(t, reviewedDone))
	if !strings.Contains(wide, "release    refinement") {
		t.Fatalf("wide screens put gates beside the stage:\n%s", wide)
	}
}

func TestProveTakesNoTickets(t *testing.T) {
	for _, args := range [][]string{{"prove", "APP-1"}, {"prove", "--to", "done"}, {"prove", "--me"}} {
		if code, _, errOut := invoke(t, &fakeSource{}, args...); code != 2 || errOut == "" {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
	}
	if code, _, errOut := invoke(t, &fakeSource{}, "prove", "--dir", branchingWorkspace(t, "\ninvariants:\n  done: [nope]\n")); code != 2 || !strings.Contains(errOut, "unknown gate nope") {
		t.Fatalf("%d %s", code, errOut)
	}
	if !slices.Contains(strings.Fields(help), "prove") {
		t.Fatal("help must list prove")
	}
}
