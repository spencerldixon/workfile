package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func detailFixture(t *testing.T) (*workfile.Workspace, workfile.Ticket) {
	t.Helper()
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	ticket := fixtureTicket("APP-42", "review", "Let customers change account settings.")
	ticket.Title = "Let customers manage their account settings"
	ticket.Facts["assignee"] = "Bob"
	ticket.Facts["labels"] = []workfile.Actor{{Value: "needs-more-information"}}
	ticket.Records = map[string][]workfile.Record{"github": {
		{ID: "web#18", Repository: "example-team/web", URL: "https://github.com/example-team/web/pull/18", Facts: workfile.Facts{"checks": "success", "state": "open", "labels": []string{"high-risk"}, "approvals": []workfile.Actor{{ID: "example-bob", Value: "example-bob"}}}},
		{ID: "api#31", Repository: "example-team/api", URL: "https://github.com/example-team/api/pull/31", Facts: workfile.Facts{"checks": "success", "state": "merged", "labels": []string{"low-risk"}, "approvals": []workfile.Actor{{ID: "example-alice", Value: "example-alice"}}}},
	}}
	return w, ticket
}
func renderDetails(w *workfile.Workspace, ticket workfile.Ticket, width int, color bool) string {
	var out bytes.Buffer
	view{out: &out, width: width, color: color}.details(w, w.Assess(ticket))
	return out.String()
}
func complexFixture(t *testing.T) (*workfile.Workspace, workfile.Ticket) {
	t.Helper()
	_, ticket := detailFixture(t)
	w, err := workfile.Load("../../example/branching")
	if err != nil {
		t.Fatal(err)
	}
	ticket.Title = "Ship account exports"
	ticket.Facts["description"] = strings.Repeat("x", 120)
	ticket.Facts["labels"] = []workfile.Actor{}
	ticket.Facts["headings"] = []string{"Acceptance Criteria", "Threat Model", "Rework"}
	return w, ticket
}

func TestDetailsGroupsFixesAndNamesWhatEachUnblocks(t *testing.T) {
	w, ticket := complexFixture(t)
	out := renderDetails(w, ticket, 110, false)
	for _, want := range []string{"Assigned to Bob", "PULL REQUESTS", "example-team/web", "TRANSITIONS", "Next steps", "Get at least 1 approval from Alice", "release-waiver", "Move APP-42 to security.", "Return APP-42 to doing.", "✓", "✗"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Index(out, "PULL REQUESTS") > strings.Index(out, "TRANSITIONS") {
		t.Fatal("transitions must be last")
	}
	_, steps, _ := strings.Cut(out, "Next steps")
	unblocks := func(fix string) string {
		_, rest, ok := strings.Cut(steps, fix)
		if !ok {
			t.Fatalf("missing fix %q:\n%s", fix, steps)
		}
		_, line, _ := strings.Cut(rest, "unblocks")
		line, _, _ = strings.Cut(line, "\n")
		return line
	}
	if got := unblocks("release-waiver"); !strings.Contains(got, "→ release") || strings.Contains(got, "→ qa") {
		t.Fatalf("the waiver only unblocks release: %q", got)
	}
	if got := unblocks("Get at least 1 approval from Alice"); !strings.Contains(got, "→ qa") || !strings.Contains(got, "→ release") || strings.Contains(got, "→ security") {
		t.Fatalf("code review unblocks qa and release: %q", got)
	}
	if strings.Contains(out, "not required for this transition") || strings.Contains(out, "gate blocked") || strings.Contains(out, "●") {
		t.Fatal("old legend, count or dot returned")
	}
	t.Log("\n" + out)
}
func TestDetailsColoursLinksAndUnderlinedCurrentStage(t *testing.T) {
	w, ticket := complexFixture(t)
	out := renderDetails(w, ticket, 110, true)
	for _, want := range []string{"\x1b[1;4;" + green + "mreview", "\x1b[1;" + red + "m→ qa", "\x1b[1;" + green + "m→ security", "\x1b]8;;" + ticket.URL, "\x1b]8;;https://github.com/example-team/web/pull/18"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing terminal styling/link %q", want)
		}
	}
	if strings.Contains(out, "38;5;214") || strings.Contains(out, "Current stage:") || strings.Contains(out, "[ review ]") {
		t.Fatal("old state styling returned")
	}
	ticket.Facts["headings"] = []string{"Acceptance Criteria", "Rework"}
	out = renderDetails(w, ticket, 110, true)
	if !strings.Contains(out, "\x1b[1;4;"+red+"mreview") || !strings.Contains(out, "\x1b[1;"+green+"m↩ doing") {
		t.Fatal("blocked forward moves must not block a valid return")
	}
}

var terminalEscapes = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;;[^\x1b]*\x1b\\\\")

func TestDetailsFitNarrowAndWideTerminalsWithoutLosingRoutes(t *testing.T) {
	w, ticket := complexFixture(t)
	ticket.Title = "Account settings 日本語 with deliberatelylongwordthatneedstowrap"
	for _, width := range []int{20, 40, 70, 100, 140} {
		for _, color := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%v", width, color), func(t *testing.T) {
				out := renderDetails(w, ticket, width, color)
				if !color && strings.Contains(out, "\x1b") {
					t.Fatal("plain output has terminal escapes")
				}
				for _, line := range strings.Split(terminalEscapes.ReplaceAllString(out, ""), "\n") {
					if runewidth.StringWidth(line) > width {
						t.Fatalf("line exceeds %d columns: %q", width, line)
					}
				}
				if !strings.Contains(out, "release") || !strings.Contains(out, "security") {
					t.Fatal("a route disappeared")
				}
			})
		}
	}
}
func TestUnassessedPRsNeverClaimGatePassOrMergeReadiness(t *testing.T) {
	w, ticket := detailFixture(t)
	ticket.State = "doing"
	ticket.Facts["labels"] = []workfile.Actor{}
	ticket.Facts["description"] = strings.Repeat("x", 120)
	out := renderDetails(w, ticket, 100, false)
	_, prs, _ := strings.Cut(out, "PULL REQUESTS")
	prs, _, _ = strings.Cut(prs, "TRANSITIONS")
	if strings.Contains(prs, "Gates pass") || !strings.Contains(prs, "Not assessed") {
		t.Fatal("unassessed PR claims a pass")
	}
	if !strings.Contains(out, "Move APP-42 to review.") {
		t.Fatal("missing concrete next step")
	}
}
func TestOverviewExplainsEveryUnmetRequirementWithoutFlags(t *testing.T) {
	_, ticket := detailFixture(t)
	code, out, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{ticket}}, "status")
	if code != 1 {
		t.Fatal(code)
	}
	for _, reason := range []string{"description is", "must not include needs-more-information", "from Alice"} {
		if !strings.Contains(out, reason) {
			t.Errorf("hidden reason: %s", reason)
		}
	}
	if strings.Contains(out, "--why") {
		t.Fatal("reasoning hidden behind a flag")
	}
}
func TestStepsDeduplicateTicketFixesWithoutCombiningDifferentPRs(t *testing.T) {
	checks := []workfile.Check{{Gate: "approval", Reasons: []workfile.Reason{
		{Action: "Add an approval label.", Provider: "github", Record: "api#1", OnTicket: true},
		{Action: "Add an approval label.", Provider: "github", Record: "web#1", OnTicket: true},
		{Action: "Get an approval.", Provider: "github", Record: "api#1", URL: "https://github.com/example/api/pull/1"},
		{Action: "Get an approval.", Provider: "other", Record: "api#1", URL: "https://github.com/other/api/pull/1"},
	}}}
	a := workfile.Assessment{Ticket: workfile.Ticket{Key: "APP-42"}}
	rows := (view{width: 140}).groupedSteps(a, []stepSource{{label: "→ a", checks: append(checks, checks...)}, {label: "→ b", checks: checks}}, true)
	count := 0
	for _, r := range rows {
		if len(r) > 0 && regexp.MustCompile(`^\s+\d+\. $`).MatchString(r[0].text) {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("got %d steps, want one ticket fix and two PR fixes", count)
	}
}
func TestPipelineOnlyDrawsConfiguredEdgesAndTerminatesCycles(t *testing.T) {
	w, ticket := complexFixture(t)
	var out bytes.Buffer
	v := view{out: &out, width: 40}
	a := w.Assess(ticket)
	for _, r := range v.pipeline(w.Policy, a) {
		v.line(v.render(r))
	}
	text := out.String()
	if !strings.Contains(text, "intake") || !strings.Contains(text, "triage") {
		t.Errorf("missing forward states:\n%s", text)
	}
	if !strings.Contains(text, "↩") && !strings.Contains(text, "↑") {
		t.Fatalf("configured returns disappeared:\n%s", text)
	}
	if strings.Contains(text, "done → cancelled") {
		t.Fatal("states order invented an edge")
	}
}
func TestTerminalPreview(t *testing.T) {
	if os.Getenv("WF_PREVIEW") != "1" {
		t.Skip("set WF_PREVIEW=1 for a terminal fixture")
	}
	w, ticket := complexFixture(t)
	newView(os.Stdout).details(w, w.Assess(ticket))
}

func TestTicketViewListsEachPROnceAndNeverRepeatsStates(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	pr := func(id string, n int) workfile.Record {
		return workfile.Record{ID: id, Repository: "example-team/api", URL: fmt.Sprintf("https://github.com/example-team/api/pull/%d", n),
			Facts: workfile.Facts{"checks": "success", "state": "open", "author": "x", "review_requests": []string{}, "approvals": []workfile.Actor{}, "labels": []string{}}}
	}
	tk := fixtureTicket("APP-42", "review", "short")
	tk.Records = map[string][]workfile.Record{"github": {pr("api#31", 31), pr("web#18", 18)}}
	f := &fakeSource{tickets: []workfile.Ticket{tk}}
	for _, dir := range []string{"../../example", "../../example/branching"} {
		var out, errOut bytes.Buffer
		run(context.Background(), []string{"status", "APP-42", "--dir", dir}, &out, &errOut, "t", func(*workfile.Workspace) source { return f })
		text := out.String()
		steps := text[strings.Index(text, "Next steps"):]
		for _, heading := range []string{"APP-42", "example-team/api #31", "example-team/api #18"} {
			if strings.Count(steps, "\n  ▌    "+heading+"\n") != 1 {
				t.Fatalf("%s: %q must head its fixes exactly once\n%s", dir, heading, steps)
			}
		}
		pipeline := text[:strings.Index(text, "PULL REQUESTS")]
		if strings.Count(pipeline, "doing") != 1 {
			t.Fatalf("%s: pipeline must show each state once\n%s", dir, pipeline)
		}
	}
}
