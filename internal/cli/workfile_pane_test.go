package cli

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func exampleWorkfilePane(width int) workfilePane {
	policy := workfile.Policy{States: []string{"doing", "review", "qa", "released"}, Transitions: map[string]workfile.Transition{
		"doing": {To: []string{"review"}}, "review": {To: []string{"qa", "doing"}}, "qa": {To: []string{"released"}}, "released": {End: true},
	}}
	assessment := &workfile.Assessment{Ticket: workfile.Ticket{Key: "APP-7", State: "review", Records: map[string][]workfile.Record{"host": {
		{ID: "example/api#42", URL: "https://example.com/api/pull/42", Facts: workfile.Facts{"state": "open", "checks": "failure"}},
		{ID: "example/web#7", URL: "https://example.com/web/pull/7", Facts: workfile.Facts{"state": "open", "review_requests": []string{"Alice"}}},
		{ID: "example/worker#9", URL: "https://example.com/worker/pull/9"},
		{ID: "example/docs#3", URL: "https://example.com/docs/pull/3", Facts: workfile.Facts{"state": "merged"}},
	}}}, Routes: []workfile.RouteAssessment{{To: "qa", Checks: []workfile.Check{{Outcome: "fail"}}}, {To: "doing", Backward: true}}}
	actions := workfile.NextActions{State: workfile.NeedsMe, Summary: "Fix failing checks", Actions: []workfile.WorkAction{
		{State: workfile.NeedsMe, Target: "example/api#42", URL: "https://example.com/api/pull/42", Text: "Fix failing checks", Destinations: []string{"qa", "released"}},
		{State: workfile.Waiting, Target: "example/api#42", URL: "https://example.com/api/pull/42", Text: "Wait for approval"},
		{State: workfile.NeedsMe, Target: "APP-7", Text: "Add acceptance criteria", Destinations: []string{"qa"}},
		{State: workfile.Waiting, Target: "example/web#7", URL: "https://example.com/web/pull/7", Text: "Waiting for approval"},
		{State: workfile.AttentionUnknown, Target: "example/worker#9", URL: "https://example.com/worker/pull/9", Text: "Check results unavailable"},
	}}
	stamp := time.Date(2026, 1, 2, 12, 34, 0, 0, time.Local)
	return workfilePane{policy: policy, assessment: assessment, actions: actions, width: width, t: newTheme(false, ""), checkedAt: stamp, now: stamp}
}

func paneText(p workfilePane) string {
	sections := p.sections()
	var text []string
	for _, section := range sections {
		text = append(text, strings.Join(section, "\n"))
	}
	return strings.Join(text, "\n\n")
}

func TestWorkfilePaneListsEachPRInItsOwnStatusBox(t *testing.T) {
	p := exampleWorkfilePane(60)
	before := make([]workfile.WorkAction, len(p.actions.Actions))
	copy(before, p.actions.Actions)
	for i := range before {
		before[i].Destinations = slices.Clone(before[i].Destinations)
	}
	text := paneText(p)
	order := []string{"Where it is", "Ticket actions", "Pull requests", "▎  api#42", "▎  worker#9", "▎  web#7", "▎  docs#3", "Checked at"}
	last := -1
	for _, heading := range order {
		at := strings.Index(text, heading)
		if at < 0 || at <= last {
			t.Fatalf("wrong section order for %q:\n%s", heading, text)
		}
		last = at
	}
	for _, target := range []string{"api#42", "web#7", "worker#9", "docs#3"} {
		if strings.Count(text, target) != 1 {
			t.Fatalf("target %s should be named once:\n%s", target, text)
		}
	}
	for _, want := range []string{"[Review]", "1. Add acceptance criteria", "2. Fix failing checks", "1 PR needs you", "unblocks → Qa · → Released", "Wait for approval", "Waiting for Alice to review", "CI failed", "Review pending", "Merged", "Checked at 12:34"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "No action needed") {
		t.Fatalf("PR with no actions should omit the action line:\n%s", text)
	}
	if strings.Count(text, "Fix failing checks") != 1 {
		t.Fatal("same fix repeated across gates or destinations")
	}
	if !reflect.DeepEqual(before, p.actions.Actions) {
		t.Fatal("rendering mutated assessment action data")
	}
}

func TestWorkfilePaneKeepsDistinctRequiredReviewGroups(t *testing.T) {
	p := exampleWorkfilePane(60)
	p.actions = workfile.NextActions{State: workfile.Waiting, Summary: "Waiting for approval", Actions: []workfile.WorkAction{
		{State: workfile.Waiting, Target: "example/api#42", Text: "Waiting for the required approval", Destinations: []string{"qa"}},
		{State: workfile.Waiting, Target: "example/api#42", Text: "Waiting for the required approval", Destinations: []string{"qa"}},
	}}
	text := paneText(p)
	if strings.Count(text, "Waiting for the required approval") != 2 || strings.Count(text, "api#42") != 1 {
		t.Fatal("display grouping collapsed distinct domain actions or repeated the PR")
	}
}

func TestWorkfilePaneStatesAreHonestAndUnmetOnly(t *testing.T) {
	for _, tc := range []struct {
		state         workfile.AttentionState
		summary, want string
	}{
		{workfile.Waiting, "Waiting for approval", "… Waiting for approval"},
		{workfile.ReadyToMove, "Move to qa", "✓ Move to Qa"},
		{workfile.AttentionUnknown, "Unavailable", "! Cannot establish next action"},
		{workfile.AttentionDone, "Done", "· Done. No next action."},
	} {
		p := exampleWorkfilePane(60)
		p.actions = workfile.NextActions{State: tc.state, Summary: tc.summary}
		if tc.state == workfile.Waiting {
			p.actions.Actions = []workfile.WorkAction{{State: tc.state, Target: "example/web#7", Text: tc.summary}}
		}
		text := paneText(p)
		if !strings.Contains(text, tc.want) {
			t.Fatalf("state %s missing %q:\n%s", tc.state, tc.want, text)
		}
		if strings.Contains(text, "PR needs you") {
			t.Fatal("no unmet personal actions should mean no PR attention count")
		}
	}
	p := exampleWorkfilePane(60)
	p.assessment.Routes = []workfile.RouteAssessment{{To: "qa"}}
	if !strings.Contains(paneText(p), "Policy allows → Qa. Unresolved actions remain.") {
		t.Fatal("operational work was incorrectly presented as a policy block")
	}
}

func TestWorkfilePaneFitsPlainAndColourAtNarrowWidths(t *testing.T) {
	for _, color := range []bool{false, true} {
		for _, terminal := range []int{40, 60} {
			p := exampleWorkfilePane(terminal - 4)
			p.t, p.panelTheme, p.v.color = newTheme(color, ""), newTheme(color, "48;5;236"), color
			const injectedColour = "\x1b[35m"
			p.actions.Actions[0].Text = strings.Repeat("界fix ", 20) + injectedColour
			p.policy.States[0] = strings.Repeat("very_long_", 6)
			sections := p.sections()
			renderer := ticketPreview{t: p.t, width: terminal}
			text := renderer.sections(panelBlocks(sections), terminal)
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > terminal {
					t.Fatalf("%d-column pane overflowed: %q", terminal, line)
				}
			}
			if color {
				for _, border := range []string{p.t.bad("▎"), p.t.warn("▎"), p.t.good("▎")} {
					stripe := strings.SplitAfter(border, "▎")[0]
					var rows []string
					for _, line := range strings.Split(text, "\n") {
						if strings.Contains(line, stripe) {
							rows = append(rows, strings.TrimSpace(ansi.Strip(line)))
						}
					}
					if len(rows) < 3 || rows[0] != "▎" || rows[len(rows)-1] != "▎" {
						t.Fatalf("missing padded status border %q: %q", border, rows)
					}
				}
			}
			if !color && strings.Contains(text, "\x1b") {
				t.Fatal("NO_COLOR pane contains escapes")
			}
			if strings.Contains(text, injectedColour) {
				t.Fatal("provider text injected terminal colour")
			}
		}
	}
}

func TestWorkfilePaneLoadingErrorsAndUnknownOwnersNeverLookReady(t *testing.T) {
	for _, mode := range []string{"pending", "error", "owners"} {
		p := exampleWorkfilePane(40)
		switch mode {
		case "pending":
			p.pending = true
			p.assessment = nil
		case "error":
			p.err = errors.New("provider returned HTTP 503")
			p.assessment = nil
		case "owners":
			p.ownersPending = true
		}
		text := paneText(p)
		if strings.Contains(text, "Ready to move") || strings.Contains(text, "PR needs you") {
			t.Fatalf("%s falsely assigned readiness or actions", mode)
		}
		if !strings.Contains(text, "Where it is") || !strings.Contains(text, "Next action") {
			t.Fatal("pane lost stable headings")
		}
	}
}

func TestTrackerWorkfileChecklistClearsOnlyFromNewEvidence(t *testing.T) {
	f := inboxSource(1)
	d := driveWithSettings(t, f, 160, 50, graphics{}, options{}, boardSettings())
	enableInbox(d)
	if !strings.Contains(d.pane(), "PR needs you") || !strings.Contains(d.pane(), "Fix failing checks") {
		t.Fatal("missing actionable checklist")
	}
	d.press("enter", "e") // Inbox dismissal must not hide a ticket requirement.
	if !strings.Contains(d.pane(), "Fix failing checks") {
		t.Fatal("unmet requirement was dismissible locally")
	}
	record := f.records["APP-1"]["github"][0]
	record.Facts = maps.Clone(record.Facts)
	record.Facts["checks"] = "success"
	f.records["APP-1"] = map[string][]workfile.Record{"github": {record}}
	d.press("r")
	if strings.Contains(d.pane(), "Fix failing checks") || strings.Contains(d.pane(), "PR needs you") {
		t.Fatalf("resolved requirement remained after refresh:\n%s", d.pane())
	}
}

func TestTrackerWorkfileChecklistScrollsPastShortTicketDescription(t *testing.T) {
	f := inboxSource(1)
	record := f.records["APP-1"]["github"][0]
	var records []workfile.Record
	for i := 1; i <= 18; i++ {
		r := record
		r.ID = fmt.Sprintf("example/app#%d", i)
		r.URL = fmt.Sprintf("https://example.com/pr/%d", i)
		records = append(records, r)
	}
	f.records["APP-1"] = map[string][]workfile.Record{"github": records}
	d := driveWithSettings(t, f, 160, 40, graphics{}, options{}, boardSettings())
	enableInbox(d)
	d.press("enter", "w")
	for i := 0; i < 8; i++ {
		d.press("down")
	}
	if d.m.side.YOffset() == 0 {
		t.Fatal("long checklist could not scroll independently of a short description")
	}
	before := d.m.side.YOffset()
	d.send(tea.WindowSizeMsg{Width: 160, Height: 40})
	if d.m.side.YOffset() != before {
		t.Fatal("redrawing reset the checklist scroll position")
	}
}
