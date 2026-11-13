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

func TestLintShowsTheBypassAndHowToCloseIt(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	f := &fakeSource{}
	code, out, _ := invoke(t, f, "lint", "--dir", root)
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
		t.Fatal("lint must not read a provider")
	}
}

func TestLintJSONForAgents(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	code, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if code != 1 || report.ExitCode != 1 || report.Command != "lint" || len(report.Invariants) != 2 || report.Problems == nil || report.Warnings == nil {
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

func TestLintWithoutInvariantsStillReportsGuarantees(t *testing.T) {
	code, out, _ := invoke(t, &fakeSource{}, "lint")
	if code != 0 || !strings.Contains(out, "No invariants to check.") || !strings.Contains(out, "WARNINGS") || !strings.Contains(out, "No invariants. Add one") || strings.Contains(out, "INVARIANTS\n") {
		t.Fatalf("%d %s", code, out)
	}
	root := branchingWorkspace(t, "\ninvariants:\n  done: [merged]\n")
	if code, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root); code != 0 || !strings.Contains(out, "Every invariant holds. 1 invariant checked.") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestLintFitsNarrowTerminals(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root)
		assertWidth(t, out, width)
		assertPlain(t, out)
	}
}

func TestLintModelFollowsTheWindow(t *testing.T) {
	w, err := workfile.Load(branchingWorkspace(t, reviewedDone))
	if err != nil {
		t.Fatal(err)
	}
	for _, color := range []bool{true, false} {
		for _, width := range []int{40, 60} {
			m := newLintModel(view{width: 100, color: color}, w, nil, w.Policy.Lint(), w.Policy.Prove())
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
	_, wide, _ := invoke(t, &fakeSource{}, "lint", "--dir", branchingWorkspace(t, reviewedDone))
	if !strings.Contains(wide, "release    refinement") {
		t.Fatalf("wide screens put gates beside the stage:\n%s", wide)
	}
}

func TestLintTakesNoTickets(t *testing.T) {
	for _, args := range [][]string{{"lint", "APP-1"}, {"lint", "--to", "done"}, {"lint", "--me"}} {
		if code, _, errOut := invoke(t, &fakeSource{}, args...); code != 2 || errOut == "" {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
	}
	if code, _, errOut := invoke(t, &fakeSource{}, "lint", "--dir", t.TempDir()); code != 2 || !strings.Contains(errOut, "no .workfile/ found") {
		t.Fatalf("%d %s", code, errOut)
	}
	if !slices.Contains(strings.Fields(help), "lint") {
		t.Fatal("help must list lint")
	}
}

func TestLintReportsPolicyProblemsAsFindings(t *testing.T) {
	root := branchingWorkspace(t, "\ninvariants:\n  done: [nope]\n")
	code, out, errOut := invoke(t, &fakeSource{}, "lint", "--dir", root)
	if code != 1 || errOut != "" {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	for _, want := range []string{"WORKFILE LINT", "PROBLEMS", "unknown gate nope", "1 problem in .workfile/"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "INVARIANTS") || strings.Contains(out, "GUARANTEES") {
		t.Fatalf("a broken policy has no invariants to show:\n%s", out)
	}
	assertPlain(t, out)
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root)
		assertWidth(t, out, width)
	}

	code, out, _ = invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if code != 1 || report.ExitCode != 1 || report.Command != "lint" || report.SchemaVersion != 1 || len(report.Problems) != 1 ||
		report.Problems[0].Severity != "error" || !strings.Contains(report.Problems[0].Message, "unknown gate nope") ||
		report.Warnings == nil || report.Invariants == nil || report.Guarantees == nil {
		t.Fatalf("%d %+v\n%s", code, report, out)
	}
}

func TestLintWarningsAloneExitZero(t *testing.T) {
	code, out, _ := invoke(t, &fakeSource{}, "lint")
	if code != 0 || !strings.Contains(out, "WARNINGS") || strings.Contains(out, "PROBLEMS") {
		t.Fatalf("%d %s", code, out)
	}
	root := branchingWorkspace(t, "\ninvariants:\n  done: [merged]\n")
	_, out, _ = invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if report.Problems == nil || len(report.Problems) != 0 || report.Warnings == nil {
		t.Fatalf("%+v", report)
	}
}

func TestProveIsALintAlias(t *testing.T) {
	root := branchingWorkspace(t, reviewedDone)
	codeLint, outLint, errLint := invoke(t, &fakeSource{}, "lint", "--dir", root)
	code, out, errOut := invoke(t, &fakeSource{}, "prove", "--dir", root)
	if code != codeLint || out != outLint || errLint != "" {
		t.Fatalf("prove must print what lint prints:\n%s\n%s", out, outLint)
	}
	if errOut != "wf: prove is now lint. The old name will stop working in a later release.\n" {
		t.Fatalf("%q", errOut)
	}
	if strings.Contains(out, "prove is now lint") {
		t.Fatal("the notice goes to stderr only")
	}
}
