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

// branchingWorkspace copies the branching example. extra is inserted at the top
// of its transitions.
func branchingWorkspace(t *testing.T, extra string) string {
	t.Helper()
	return exampleWorkspace(t, "branching", extra)
}

// exampleWorkspace copies a named example. extra is inserted at the top of its
// transitions.
func exampleWorkspace(t *testing.T, example, extra string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", example, ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "policy.yml" {
			data = []byte(strings.Replace(string(data), "transitions:\n", "transitions:\n"+extra, 1))
		}
		path := filepath.Join(root, ".workfile", name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// anyDone is a policy line that requires code review on every move into done.
const anyDone = "  any:\n    to: [done]\n    requires: [code_review]\n"

func TestLintPassesAndDrawsThePolicy(t *testing.T) {
	root := exampleWorkspace(t, "jira-only", "")
	f := &fakeSource{}
	code, out, _ := invoke(t, f, "lint", "--dir", root)
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	for _, want := range []string{"✓ Pass", "Nothing can slip through", "Flow", "doing"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The verdict comes first.
	if strings.Index(out, "Pass") > strings.Index(out, "Flow") {
		t.Fatalf("verdict must lead:\n%s", out)
	}
	assertPlain(t, out)
	if f.tested != nil || f.keys != nil {
		t.Fatal("lint must not read a provider")
	}
}

func TestLintListsPossibleGapsWithoutFailing(t *testing.T) {
	root := branchingWorkspace(t, "")
	code, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root)
	if code != 0 {
		t.Fatalf("gaps are advice, not a failure: %d\n%s", code, out)
	}
	for _, want := range []string{"! Pass  4 gates can be skipped", "Gates that can be skipped", "release: code review is skipped by security → release"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// An any entry closes the gap, and the list shrinks.
	root = branchingWorkspace(t, "  any:\n    to: [release]\n    requires: [code_review]\n")
	_, out, _ = invoke(t, &fakeSource{}, "lint", "--dir", root)
	if strings.Contains(out, "code review is skipped") || !strings.Contains(out, "3 gates can be skipped") {
		t.Fatalf("%s", out)
	}
	_, out, _ = invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || len(report.Gaps) != 3 || len(report.Gaps[0].Open) == 0 {
		t.Fatalf("%v %+v", err, report.Gaps)
	}
}

func TestLintJSONForAgents(t *testing.T) {
	root := branchingWorkspace(t, "")
	code, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if code != 0 || report.ExitCode != 0 || report.Command != "lint" || report.Problems == nil || report.Warnings == nil {
		t.Fatalf("%d %+v", code, report)
	}
	if report.Guarantees[0].Stage != "intake" || report.Guarantees[0].Gates == nil {
		t.Fatalf("guarantees must follow the pipeline, with empty lists not null: %+v", report.Guarantees[0])
	}
	if strings.Contains(out, "invariants") {
		t.Fatalf("invariants are gone from the report:\n%s", out)
	}
	if !strings.HasPrefix(report.PolicyHash, "sha256:") {
		t.Fatal(report.PolicyHash)
	}
}

func TestLintFitsNarrowTerminals(t *testing.T) {
	root := branchingWorkspace(t, "")
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root)
		assertWidth(t, out, width)
		assertPlain(t, out)
	}
}

func TestLintModelFollowsTheWindow(t *testing.T) {
	w, err := workfile.Load(branchingWorkspace(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, color := range []bool{true, false} {
		for _, width := range []int{40, 60} {
			m := newLintModel(view{width: 100, color: color}, w, nil, w.Policy.Lint(), w.Policy.Gaps())
			m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
			out := m.render()
			assertWidth(t, ansi.Strip(out), width)
			if !color {
				assertPlain(t, out)
			}
			// Names are never split across lines.
			if !strings.Contains(out, "security") || !strings.Contains(out, "Gates that can be skipped") {
				t.Fatalf("%d columns:\n%s", width, out)
			}
		}
	}
	t.Setenv("COLUMNS", "100")
	_, wide, _ := invoke(t, &fakeSource{}, "lint", "--dir", branchingWorkspace(t, ""))
	if !strings.Contains(wide, "Flow") {
		t.Fatalf("wide screens draw the flow:\n%s", wide)
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
	if !slices.Contains(strings.Fields(helpText(".")), "lint") {
		t.Fatal("help must list lint")
	}
}

func TestLintReportsPolicyProblemsAsFindings(t *testing.T) {
	root := branchingWorkspace(t, "  any:\n    to: [done]\n    requires: [nope]\n")
	code, out, errOut := invoke(t, &fakeSource{}, "lint", "--dir", root)
	if code != 1 || errOut != "" {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	for _, want := range []string{"wf lint", "Problems", "unknown gate nope", "1 problem in .workfile/", "✗ Fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Flow") {
		t.Fatalf("a broken policy has no diagram to show:\n%s", out)
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
		report.Warnings == nil || report.Gaps == nil || report.Guarantees == nil {
		t.Fatalf("%d %+v\n%s", code, report, out)
	}
}

func TestLintWarningsAloneExitZero(t *testing.T) {
	root := branchingWorkspace(t, "")
	_, out, _ := invoke(t, &fakeSource{}, "lint", "--dir", root, "--json")
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if report.Problems == nil || len(report.Problems) != 0 || report.Warnings == nil || report.Gaps == nil {
		t.Fatalf("%+v", report)
	}
}

func TestProveIsALintAlias(t *testing.T) {
	root := branchingWorkspace(t, "")
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

func TestLintDrawsThePolicyAsAnImageWhenTheTerminalCanShowOne(t *testing.T) {
	w, err := workfile.Load(exampleWorkspace(t, "jira-only", ""))
	if err != nil {
		t.Fatal(err)
	}
	m := newLintModel(view{width: 100, color: true}, w, nil, w.Policy.Lint(), w.Policy.Gaps())
	if out := m.render(); strings.Contains(out, "\x1b_G") || !strings.Contains(ansi.Strip(out), "┌") && !strings.Contains(ansi.Strip(out), "──→") {
		t.Fatalf("without graphics the flow is text:\n%s", ansi.Strip(out))
	}
	m.gfx = testGraphics
	out := m.render()
	if !strings.Contains(out, "\x1b_G") || !strings.ContainsRune(out, 0x10EEEE) {
		t.Fatalf("with graphics the flow is a picture drawn in placeholder cells:\n%q", out)
	}
	// Plain output never carries a picture, even if graphics were asked for.
	m = newLintModel(view{width: 100}, w, nil, nil, nil)
	m.gfx = testGraphics
	if strings.Contains(m.render(), "\x1b") {
		t.Fatal("no colour means no escapes")
	}
}

func TestFlowLayoutAlignsStagesThatShareAPosition(t *testing.T) {
	stages := []string{"todo", "doing", "done", "rejected"}
	edges := []pipelineEdge{{from: 0, to: 1, lane: -1, color: green}, {from: 1, to: 2, lane: -1, color: muted}, {from: 0, to: 3, lane: 0, color: muted}}
	l := newFlowLayout(stages, map[string]bool{"done": true, "rejected": true}, edges, nil)
	done, rejected := l.nodes[2], l.nodes[3]
	if done.col != rejected.col || done.row == rejected.row || l.cols != 3 || l.rows != 2 {
		t.Fatalf("the two end stages share the last column, one above the other: %+v cols=%d rows=%d", l.nodes, l.cols, l.rows)
	}
	// rejected goes on top: the arc over the top lands straight on it, instead of
	// dropping down through the arrow into done.
	if rejected.row != 0 || l.cost() >= 1 {
		t.Fatalf("stacked stages are ordered to avoid crossings: rejected row %d, cost %v", rejected.row, l.cost())
	}
	if l.moves[2].kind != moveLong || l.lanes != 1 || len(l.tops[0]) != 1 {
		t.Fatalf("a move that skips a column goes over the top: %+v", l.moves)
	}
	rows, ok := view{color: false}.textFlow(&l, 80)
	if !ok {
		t.Fatal("text flow should fit")
	}
	var text []string
	for _, r := range rows {
		line := ""
		for _, c := range r {
			line += c.text
		}
		text = append(text, line)
	}
	column := func(name string) int {
		for _, line := range text {
			if i := strings.Index(line, name); i >= 0 {
				return len([]rune(line[:i]))
			}
		}
		return -1
	}
	if column("done") != column("rejected") || column("done") < 0 {
		t.Fatalf("done and rejected must line up:\n%s", strings.Join(text, "\n"))
	}
	for _, line := range text {
		if len([]rune(line)) > 80 {
			t.Fatalf("too wide: %q", line)
		}
	}
	if _, ok := (view{}).textFlow(&l, 10); ok {
		t.Fatal("a narrow terminal gets strips, not columns")
	}
}

func TestFlowImageFallsBackWhenItCannotBeDrawnWell(t *testing.T) {
	stages := []string{"todo", "doing", "done"}
	edges := []pipelineEdge{{from: 0, to: 1, lane: -1, color: green}, {from: 1, to: 2, lane: -1, color: muted}, {from: 0, to: 2, lane: 0, color: red}}
	l := newFlowLayout(stages, map[string]bool{"done": true}, edges, map[string]bool{"done": true})
	img, rows, ok := flowImage(&l, testGraphics, 60)
	if !ok || img.Bounds().Dx() != 60*testGraphics.cellW || img.Bounds().Dy() != rows*testGraphics.cellH {
		t.Fatalf("%v %d %v", ok, rows, img)
	}
	// A terminal that never tells us its text colour still gets a picture.
	if _, _, ok := flowImage(&l, graphics{on: true, cellW: 8, cellH: 16}, 60); !ok {
		t.Fatal("a terminal that does not report its text colour still gets a picture")
	}
	if _, _, ok := flowImage(&l, graphics{cellW: 8, cellH: 16}, 60); ok {
		t.Fatal("a terminal without graphics gets text")
	}
	if _, _, ok := flowImage(&l, testGraphics, 12); ok {
		t.Fatal("a very narrow terminal gets text")
	}
	many := make([]string, 40)
	for i := range many {
		many[i] = "a-long-stage-name"
	}
	if _, _, ok := flowImage(func() *flowLayout { l := newFlowLayout(many, nil, nil, nil); return &l }(), testGraphics, 100); ok {
		t.Fatal("too many stages to read gets text")
	}
}
