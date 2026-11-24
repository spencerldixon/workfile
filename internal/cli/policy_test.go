package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func TestPolicyJSONDescribesWorkspaceOffline(t *testing.T) {
	root := branchingWorkspace(t, anyDone)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Project instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"policy", "--json", "--dir", root}, &out, &stderr, "test", nil)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d: %s\n%s", code, out.String(), stderr.String())
	}
	var report policyReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	p := report.Workspace
	if report.Command != "policy" || report.SchemaVersion != 1 || report.PolicyHash == "" || p == nil {
		t.Fatalf("unexpected report: %s", out.String())
	}
	if p.Root != root || p.Tracker != "jira" || p.Stages[0] != "intake" || len(p.Gates["code_review"]) < 1 ||
		len(p.Providers["jira"].Statuses["doing"]) == 0 || p.Files["instructions"] != "AGENTS.md" {
		t.Fatalf("missing workspace data: %s", out.String())
	}
	if !slices.ContainsFunc(p.Moves, func(move policyMove) bool {
		return move.From == "release" && move.To == "done" && slices.Contains(move.Requires, "code_review")
	}) || !slices.ContainsFunc(p.Moves, func(move policyMove) bool {
		return move.From == "review" && move.To == "doing" && move.Backward && slices.Contains(move.Requires, "rework_note")
	}) {
		t.Fatalf("missing effective moves: %s", out.String())
	}
	if strings.Contains(out.String(), `"auth"`) || strings.Contains(out.String(), "EXAMPLE_JIRA_TOKEN") {
		t.Fatalf("credential settings in report: %s", out.String())
	}
}

func TestPolicyViewFitsPlainTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	w, err := workfile.LoadPolicy(branchingWorkspace(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	m := newLintModel(view{out: &bytes.Buffer{}}, w, nil, nil, nil)
	m.policy = describePolicy(w)
	for _, width := range []int{40, 60} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		out := m.View().Content
		assertPlain(t, out)
		for _, want := range []string{"wf policy", "Flow", "Gates", "Providers", "github.checks", "doing →"} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d misses %q", width, want)
			}
		}
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d overflows: %q", width, line)
			}
		}
	}
}

func TestPolicyInspectionReportsInvalidPolicy(t *testing.T) {
	root := branchingWorkspace(t, strings.Replace(anyDone, "code_review", "missing_gate", 1))
	code, out, _ := invoke(t, &fakeSource{}, "policy", "--json", "--dir", root)
	var report policyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if code != 1 || report.ExitCode != 1 || report.Command != "policy" || len(report.Problems) != 1 || report.Workspace != nil {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestProvePreservesJSONContract(t *testing.T) {
	root := branchingWorkspace(t, "")
	oldCode, oldOut, notice := invoke(t, &fakeSource{}, "prove", "--json", "--dir", root)
	newCode, newOut, stderr := invoke(t, &fakeSource{}, "policy", "--lint", "--json", "--dir", root)
	if oldCode != 0 || newCode != oldCode || newOut != strings.Replace(oldOut, `"command": "lint"`, `"command": "policy"`, 1) ||
		stderr != "" || !strings.Contains(notice, "prove is now policy --lint") {
		t.Fatalf("old: %d %s %s\nnew: %d %s %s", oldCode, oldOut, notice, newCode, newOut, stderr)
	}
}
