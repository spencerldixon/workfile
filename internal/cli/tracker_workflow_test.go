package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"workfile/internal/workfile"
)

func TestWorkfileDiagramShowsConfiguredWorkflowOnce(t *testing.T) {
	policy := workfile.Policy{States: []string{"todo", "doing", "review", "qa", "done"}, Transitions: map[string]workfile.Transition{
		"todo": {To: []string{"doing"}}, "doing": {To: []string{"review", "todo"}}, "review": {To: []string{"qa"}}, "qa": {To: []string{"done"}}, "done": {End: true},
	}}
	assessment := workfile.Assessment{Ticket: workfile.Ticket{State: "doing"}}
	for _, width := range []int{20, 36, 56} {
		pane := workfilePane{policy: policy, assessment: &assessment, width: width, t: newTheme(false, "")}
		text := strings.Join(pane.where(), "\n")
		for _, state := range []string{"Todo", "Doing", "Review", "Qa", "Done"} {
			if strings.Count(text, state) != 1 {
				t.Fatalf("stage %s was lost or repeated:\n%s", state, text)
			}
		}
		if !strings.Contains(text, "[Doing]") {
			t.Fatalf("current stage is not marked:\n%s", text)
		}
		for _, line := range strings.Split(text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("workflow overflows %d: %s", width, line)
			}
		}
	}
	// State ordering alone must never invent a transition.
	policy.Transitions = nil
	pane := workfilePane{policy: policy, assessment: &assessment, width: 80, t: newTheme(false, "")}
	if strings.Contains(strings.Join(pane.where(), "\n"), ">") {
		t.Fatal("diagram invented an unconfigured move")
	}
}

func TestWorkfileDiagramDoesNotClaimUnknownEvidenceIsPassing(t *testing.T) {
	assessment := workfile.Assessment{Ticket: workfile.Ticket{State: "doing"}, Routes: []workfile.RouteAssessment{{To: "review", Checks: []workfile.Check{{Outcome: "unknown"}}}}}
	pane := workfilePane{policy: workfile.Policy{States: []string{"doing", "review"}, Transitions: map[string]workfile.Transition{"doing": {To: []string{"review"}}}}, assessment: &assessment, width: 40, t: newTheme(true, ""), v: view{color: true}}
	text := strings.Join(pane.where(), "\n")
	if strings.Contains(text, "38;5;2") || strings.Contains(text, "38;5;1") {
		t.Fatal("structural diagram must not turn uncertainty into a passing/failing verdict")
	}
}

func TestTicketWorkflowDiagramHidesGates(t *testing.T) {
	policy := workfile.Policy{
		States: []string{"doing", "review", "released"},
		Transitions: map[string]workfile.Transition{
			"doing":           {Routes: map[string][]string{"review": {"approval"}, "released": {}}},
			"review":          {Routes: map[string][]string{"doing": {"rework"}, "released": {"checks"}}},
			workfile.AnyStage: {To: []string{"released"}, Requires: []string{"signoff"}},
		},
	}
	assessment := workfile.Assessment{Ticket: workfile.Ticket{State: "review"}}
	for _, width := range []int{20, 40, 60} {
		pane := workfilePane{policy: policy, assessment: &assessment, width: width, t: newTheme(false, "")}
		text := strings.Join(pane.where(), "\n")
		assertWidth(t, text, width)
		assertPlain(t, text)
		for _, name := range []string{"🔒", "Approval", "Rework", "Checks", "Signoff"} {
			if strings.Contains(text, name) {
				t.Fatalf("%d columns: ticket diagram showed %q:\n%s", width, name, text)
			}
		}
	}
}

func TestTrackerWorkflowModesAndNarrowWidths(t *testing.T) {
	for _, mode := range []string{"auto", "text", "off"} {
		for _, width := range []int{40, 60} {
			d := driveWithSettings(t, trackerSource(), width, 40, graphics{}, options{}, workfile.TrackerSettings{Preview: workfile.TrackerPreview{Workflow: mode}})
			sections := d.m.paneFor(d.m.ticketReads.byKey["APP-1"], width).sections()
			text := strings.Join(sections[0], "\n")
			if strings.Contains(text, "\x1b") {
				t.Fatal("plain workflow contains terminal sequences")
			}
			if mode == "off" {
				if !strings.Contains(text, "Current stage: Doing") || strings.Contains(text, "[Doing]") {
					t.Fatalf("off must keep the stage without a diagram:\n%s", text)
				}
			} else if !strings.Contains(text, "[Doing]") {
				t.Fatalf("map lost current stage:\n%s", text)
			}
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > width-4 {
					t.Fatalf("workflow overflows %d: %s", width, line)
				}
			}
			d.press("enter", "down")
		}
	}
}

func TestTrackerKeepsTwoColumnsForBranchingWorkflow(t *testing.T) {
	d := driveTracker(t, trackerSource(), 160, 60)
	d.m.e.w.Policy = branchingTrackerPolicy()
	delete(d.m.ticketReads.byKey, d.m.current)
	d.run(d.m.read(d.m.current))
	if _, right := d.m.columns(); right == 0 {
		t.Fatal("branching workflow took the ticket column")
	}
	if !strings.Contains(d.m.side.View(), "Doing") || !strings.Contains(d.m.side.View(), "Can move on") {
		t.Fatalf("workflow missing from Workfile column:\n%s", d.m.side.View())
	}
	d.check()
}

func workflowCase(tone string) (workfile.Policy, workfile.Assessment) {
	policy := workfile.Policy{States: []string{"todo", "doing", "review", "done"}, Transitions: map[string]workfile.Transition{
		"todo": {To: []string{"doing"}}, "doing": {To: []string{"review", "todo"}}, "review": {To: []string{"done"}}, "done": {End: true},
	}}
	route := workfile.RouteAssessment{To: "review"}
	if tone == red {
		route.Checks = []workfile.Check{{Outcome: "fail"}}
	}
	return policy, workfile.Assessment{Ticket: workfile.Ticket{State: "doing"}, Routes: []workfile.RouteAssessment{route}}
}

func TestWorkfileDiagramColoursCurrentStageAndSaysWhy(t *testing.T) {
	for tone, caption := range map[string]string{green: "✓ Can move on", red: "✗ Blocked"} {
		policy, assessment := workflowCase(tone)
		pane := workfilePane{policy: policy, assessment: &assessment, width: 60, t: newTheme(true, ""), v: view{color: true}}
		text := strings.Join(pane.where(), "\n")
		if !strings.Contains(text, "\x1b[1;4;"+tone+"mDoing") {
			t.Fatalf("current stage is not bold %s:\n%q", tone, text)
		}
		if strings.Contains(text, "[Doing]") {
			t.Fatal("coloured output kept the plain marker")
		}
		if !strings.Contains(ansi.Strip(text), caption) {
			t.Fatalf("colour needs words too, missing %q", caption)
		}
	}
}

func TestWorkfileDiagramIsCentred(t *testing.T) {
	policy, assessment := workflowCase(green)
	for _, width := range []int{40, 60, 90} {
		pane := workfilePane{policy: policy, assessment: &assessment, width: width, t: newTheme(false, "")}
		lines := pane.where()
		chain := ""
		for _, line := range lines {
			if strings.Contains(line, "[Doing]") {
				chain = line
			}
		}
		text := strings.TrimLeft(chain, " ")
		left := ansi.StringWidth(chain) - ansi.StringWidth(text)
		if right := width - ansi.StringWidth(chain); left == 0 || left-right > 1 || right-left > 1 {
			t.Fatalf("width %d: chain is not centred (%d left, %d right): %q", width, left, right, chain)
		}
	}
}

func TestTrackerWorkflowPictureWithTextFallback(t *testing.T) {
	d := graphicTickets(t, trackerSource(), 60, 60)
	d.m.v.color = true
	d.run(d.m.loadWorkflow())
	if len(d.m.flows) != 1 {
		t.Fatalf("expected one workflow picture, got %d", len(d.m.flows))
	}
	sections := d.m.paneFor(d.m.ticketReads.byKey[d.m.current], 60).sections()
	where := strings.Join(sections[0], "\n")
	if !strings.ContainsRune(where, kitty.Placeholder) || strings.Contains(where, "->") {
		t.Fatalf("picture did not replace the text diagram:\n%s", where)
	}
	for _, line := range sections[0] {
		if strings.ContainsRune(line, kitty.Placeholder) && !strings.HasPrefix(line, " ") && ansi.StringWidth(line) < 56 {
			t.Fatal("narrow picture is not centred")
		}
		if ansi.StringWidth(line) > 56 {
			t.Fatalf("picture overflows: %d", ansi.StringWidth(line))
		}
	}
	if got, want := strings.Count(d.m.forgetAll(), "a=d"), len(d.m.images)+len(d.m.titles)+len(d.m.headings)+len(d.m.flows); got != want {
		t.Fatalf("forgot %d of %d terminal images", got, want)
	}
	// Text mode never asks for a picture.
	d = driveWithSettings(t, trackerSource(), 60, 60, testGraphics, options{}, workfile.TrackerSettings{Preview: workfile.TrackerPreview{Workflow: "text"}})
	d.m.v.color = true
	d.run(d.m.loadWorkflow())
	if len(d.m.flows) != 0 {
		t.Fatal("text mode drew a picture")
	}
}
