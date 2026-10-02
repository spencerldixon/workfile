package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

type fakeSource struct {
	tickets      []workfile.Ticket
	limit        int
	keys, tested []string
	testErrors   map[string]error
}

func (f *fakeSource) Tickets(_ context.Context, keys []string, limit int) ([]workfile.Ticket, error) {
	f.limit = limit
	f.keys = keys
	result := slices.Clone(f.tickets)
	if len(keys) > 0 {
		result = slices.DeleteFunc(result, func(t workfile.Ticket) bool { return !slices.Contains(keys, t.Key) })
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (f *fakeSource) Records(_ context.Context, tickets []workfile.Ticket) ([]workfile.Ticket, []string, error) {
	return tickets, nil, nil
}
func (f *fakeSource) Identity(context.Context, string) (string, error) { return "example-bob", nil }
func (f *fakeSource) Test(_ context.Context, name string) (string, error) {
	f.tested = append(f.tested, name)
	return "connected", f.testErrors[name]
}

func fixtureTicket(key, state, description string) workfile.Ticket {
	return workfile.Ticket{Key: key, Title: "A clear account settings page", State: state, Summary: "In progress · assigned to Bob", URL: "https://example-team.atlassian.net/browse/" + key,
		Facts: workfile.Facts{"description": description, "type": "Bug", "labels": []workfile.Actor{}},
	}
}

func invoke(t *testing.T, f *fakeSource, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if !slices.Contains(args, "--dir") {
		args = append(args, "--dir", "../../example")
	}
	code := run(context.Background(), args, &out, &errOut, "test", func(*workfile.Workspace) source { return f })
	return code, out.String(), errOut.String()
}

func TestOnlyThreeCommandsAndNoJSON(t *testing.T) {
	for _, args := range [][]string{{"validate"}, {"providers"}, {"status", "--json"}, {"status", "--why"}, {"status", "--limit", "0"}, {"status", "--me", "--user", "bob"}, {"test", "--all"}, {"status", "../secret"}} {
		code, _, err := invoke(t, &fakeSource{}, args...)
		if code != 2 || err == "" {
			t.Fatalf("%v: %d %q", args, code, err)
		}
	}
	for _, args := range [][]string{nil, {"--help"}, {"status", "--help"}, {"--version"}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, &out, io.Discard, "v1.2.3"); code != 0 {
			t.Fatal(code)
		}
		if out.Len() == 0 {
			t.Fatal("empty help/version")
		}
	}
}

func TestStatusIncludesCompletedTicketsAndHealthOnlyViolations(t *testing.T) {
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", "short"), fixtureTicket("APP-2", "done", "short")}}
	code, out, _ := invoke(t, f, "status")
	if code != 1 || !strings.Contains(out, "APP-1") || !strings.Contains(out, "APP-2") || !strings.Contains(out, "needs work") || !strings.Contains(out, "OUT OF POLICY") {
		t.Fatalf("%d\n%s", code, out)
	}
	code, out, _ = invoke(t, f, "health", "--stage", "done")
	if code != 1 || strings.Contains(out, "APP-1") || !strings.Contains(out, "APP-2") {
		t.Fatalf("%d\n%s", code, out)
	}
	f.tickets = f.tickets[:1]
	code, out, _ = invoke(t, f, "health")
	if code != 0 || !strings.Contains(out, "in policy") || strings.Contains(out, "GATES") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestHealthSummaryIsInPipelineOrderWithPercentages(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	f := &fakeSource{tickets: []workfile.Ticket{
		fixtureTicket("APP-1", "todo", "short"), fixtureTicket("APP-2", "done", "short"),
		fixtureTicket("APP-3", "done", strings.Repeat("x", 120)), fixtureTicket("APP-4", "todo", strings.Repeat("x", 120)),
	}}
	for i := range 30 {
		f.tickets = append(f.tickets, fixtureTicket(fmt.Sprintf("APP-%d", i+10), "todo", strings.Repeat("x", 120)))
	}
	code, out, _ := invoke(t, f, "health")
	if code != 1 || f.limit != 0 {
		t.Fatalf("code %d, fetch limit %d (health checks everything by default)\n%s", code, f.limit, out)
	}
	for _, want := range []string{"SUMMARY", "34 tickets checked", "GATES", "STAGES", "MOST COMMON REASONS", "%", "wf health --gate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "APP-2") {
		t.Fatalf("summary should not list tickets\n%s", out)
	}
	stages := out[strings.Index(out, "STAGES"):]
	last := -1
	for _, name := range []string{"todo", "doing", "review", "done"} {
		at := strings.Index(stages, name)
		if at < last {
			t.Fatalf("stages out of pipeline order\n%s", out)
		}
		last = at
	}
	_, out, _ = invoke(t, f, "health", "--limit", "5")
	if f.limit != 5 || !strings.Contains(out, "sampled") {
		t.Fatalf("an explicit limit must be flagged as a sample (fetch limit %d)\n%s", f.limit, out)
	}
}

func TestHealthDrillDownFilters(t *testing.T) {
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", "short"), fixtureTicket("APP-2", "done", "short")}}
	_, out, _ := invoke(t, f, "health", "--gate", "refinement")
	if !strings.Contains(out, "APP-2") || strings.Contains(out, "APP-1") || strings.Contains(out, "pull request:") {
		t.Fatalf("%s", out)
	}
	_, out, _ = invoke(t, f, "health", "--gate", "code_review", "--stage", "done")
	if strings.Contains(out, "APP-2") {
		t.Fatalf("APP-2 does not fail code_review:\n%s", out)
	}
	for _, args := range [][]string{{"health", "--gate", "nope"}, {"health", "--stage", "nope"}, {"status", "--gate", "refinement"}} {
		if code, _, err := invoke(t, f, args...); code != 2 || err == "" {
			t.Fatalf("%v: %d %q", args, code, err)
		}
	}
}

func TestNamedTicketsFiltersAndLimits(t *testing.T) {
	f := &fakeSource{}
	for i := range 30 {
		f.tickets = append(f.tickets, fixtureTicket(fmt.Sprintf("APP-%d", i+1), "todo", strings.Repeat("x", 120)))
	}
	f.tickets[29].Assignee = "example-bob-account-id"
	code, out, err := invoke(t, f, "status", "--user", "bob", "--limit", "1")
	if code != 0 || f.limit != 0 || !strings.Contains(out, "APP-30") || err != "" {
		t.Fatalf("code %d, fetch limit %d: %s %s", code, f.limit, out, err)
	}
	code, out, _ = invoke(t, f, "status", "app-30", "--limit=1")
	if code != 0 || f.limit != 0 || !strings.Contains(out, "Move APP-30 to doing.") {
		t.Fatalf("%d %s", code, out)
	}
	code, _, err = invoke(t, f, "status", "APP-404")
	if code != 2 || !strings.Contains(err, "not visible") {
		t.Fatalf("%d %q", code, err)
	}
	_, out, _ = invoke(t, f, "status", "--limit", "2")
	if f.limit != 2 || strings.Contains(out, "APP-30") {
		t.Fatalf("fetch limit %d: %s", f.limit, out)
	}
}

func TestMeMatchesPRAuthorsAndRequestedReviewers(t *testing.T) {
	t.Setenv("WORKFILE_CONFIG_FILE", t.TempDir()+"/absent.yml")
	t.Setenv("NO_COLOR", "1")
	for _, role := range []string{"author", "review_requests"} {
		t.Run(role, func(t *testing.T) {
			tk := fixtureTicket("APP-42", "review", strings.Repeat("x", 120))
			facts := workfile.Facts{"approvals": []workfile.Actor{}, "labels": []string{}, "author": "someone-else", "review_requests": []string{}}
			if role == "author" {
				facts["author"] = "example-bob"
			} else {
				facts["review_requests"] = []string{"example-bob"}
			}
			tk.Records = map[string][]workfile.Record{"github": {{ID: "api#1", People: []string{"example-bob"}, Facts: facts}}}
			f := &fakeSource{tickets: []workfile.Ticket{tk}}
			code, out, _ := invoke(t, f, "status", "--me")
			if code != 1 || !strings.Contains(out, "ALSO INVOLVES YOU") || !strings.Contains(out, "api #1") {
				t.Fatalf("%d %s", code, out)
			}
		})
	}
}

func TestConnectionsContinueAfterFailure(t *testing.T) {
	f := &fakeSource{testErrors: map[string]error{"github": errors.New("token expired")}}
	code, out, _ := invoke(t, f, "test")
	if code != 2 || !slices.Equal(f.tested, []string{"github", "jira"}) || !strings.Contains(out, "token expired") || !strings.Contains(out, "✓ jira") {
		t.Fatalf("%d %v %s", code, f.tested, out)
	}
	f.tested = nil
	code, _, _ = invoke(t, f, "test", "jira")
	if code != 0 || !slices.Equal(f.tested, []string{"jira"}) {
		t.Fatal("named connection not respected")
	}
}

func TestUnmappedTicketIsAnError(t *testing.T) {
	tk := fixtureTicket("APP-42", "", "")
	tk.Error = "Unmapped Jira status"
	code, out, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "health")
	if code != 2 || !strings.Contains(out, "CANNOT CHECK") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestPlainAndNarrowOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "40")
	tk := fixtureTicket("APP-42", "todo", "short")
	tk.Title = "A long 日本語 title with multiple words and an unbrokenlongwordhere"
	_, out, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "status")
	if strings.Contains(out, "\x1b") {
		t.Fatal("pipe contains terminal escapes")
	}
	for _, line := range strings.Split(out, "\n") {
		if runewidth.StringWidth(line) > 40 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	if strings.Contains(clean("hello\x1b\a\r\u202e"), "\x1b") {
		t.Fatal("control sequence escaped sanitisation")
	}
	var b bytes.Buffer
	v := view{out: &b, color: true, width: 80}
	if strings.Contains(v.link("ticket", "javascript:alert(1)"), "\x1b") {
		t.Fatal("unsafe hyperlink")
	}
	if !strings.Contains(v.link("ticket", "https://example.com"), "\x1b]8") {
		t.Fatal("missing terminal hyperlink")
	}
}

func boardFixture() *fakeSource {
	long := strings.Repeat("x", 120)
	doing := fixtureTicket("APP-57", "doing", long)
	doing.Title, doing.Assignee = "Add a delivery estimate", "example-bob-account-id"
	review := fixtureTicket("APP-42", "review", long)
	review.Title = "Improve account settings"
	review.Records = map[string][]workfile.Record{"github": {{ID: "api#31", Repository: "example-team/api", URL: "https://github.com/example-team/api/pull/31",
		People: []string{"example-bob"}, Facts: workfile.Facts{"state": "open", "author": "example-alice", "review_requests": []string{"example-bob"}, "approvals": []workfile.Actor{}, "labels": []string{}}}}}
	return &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-61", "todo", long), doing, review, fixtureTicket("APP-70", "done", "short")}}
}

func TestStatusBoardIsInPipelineOrder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	_, out, _ := invoke(t, boardFixture(), "status")
	last := -1
	for _, want := range []string{"SUMMARY", "PIPELINE", "TODO · 1 ticket", "DOING · 1 ticket", "REVIEW · 1 ticket", "DONE · 1 ticket"} {
		at := strings.Index(out, want)
		if at < 0 || at < last {
			t.Fatalf("%q missing or out of order\n%s", want, out)
		}
		last = at
	}
	for _, want := range []string{"25%", "OUT OF POLICY", "→ doing", "refinement: description is", "code review: api#31"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	_, out, _ = invoke(t, boardFixture(), "status", "--failing")
	if strings.Contains(out, "APP-61") {
		t.Fatalf("--failing must hide ready tickets\n%s", out)
	}
}

func TestMeBoardIsAKanbanOfAssignedWork(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WORKFILE_CONFIG_FILE", t.TempDir()+"/absent.yml")
	f := boardFixture()
	finished := fixtureTicket("APP-99", "done", strings.Repeat("x", 120))
	finished.Assignee = "example-bob-account-id"
	finished.Title = "Already shipped"
	f.tickets = append(f.tickets, finished)
	_, out, _ := invoke(t, f, "status", "--user", "bob")
	for _, want := range []string{"involving Bob", "1 ticket assigned to Bob", "✗ 1 need work", "doing 1", "DOING · 1 ticket", "APP-57", "→ review", "On the ticket", "Link at least 1 PR", "ALSO INVOLVES YOU", "example-team/api #31", "asked to review"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"APP-99", "DONE", "YOUR NEXT ACTIONS", "╭"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("%q does not belong in the lane view\n%s", unwanted, out)
		}
	}
	if strings.Index(out, "DOING") > strings.Index(out, "ALSO INVOLVES YOU") {
		t.Fatalf("assigned work comes before involvement\n%s", out)
	}
}

func TestLanesTintWhenTheTerminalAnswers(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	var out bytes.Buffer
	v := view{out: &out, color: true, width: 80}
	terminalBackground = func() (uint8, uint8, uint8, bool) { return 0, 0, 0, true }
	t.Cleanup(func() { terminalBackground = queryTerminalBackground })
	bg := v.laneBackground()
	if bg != "48;2;18;18;18" {
		t.Fatalf("a black background should tint lighter, got %q", bg)
	}
	v.lane(red, styled("DOING · 1 ticket", "1"), []row{{plain("APP-1  "), styled("✗ gate", red)}}, bg)
	text := out.String()
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "\x1b["+bg+"m") || !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("every lane line carries the background: %q", line)
		}
	}
	if !strings.Contains(text, "\x1b[31m▌\x1b[0m\x1b["+bg+"m") || !strings.Contains(text, "\x1b[0m\x1b["+bg+"m  ") {
		t.Fatalf("styled text must restore the background afterwards:\n%q", text)
	}

	terminalBackground = func() (uint8, uint8, uint8, bool) { return 0, 0, 0, false }
	if v.laneBackground() != "" {
		t.Fatal("no answer, no background")
	}
	t.Setenv("WORKFILE_BACKGROUND", "off")
	terminalBackground = func() (uint8, uint8, uint8, bool) { return 0, 0, 0, true }
	if v.laneBackground() != "" {
		t.Fatal("the setting turns the background off")
	}
	if lr, lg, lb := laneTint(255, 255, 255); lr >= 255 || lg >= 255 || lb >= 255 {
		t.Fatal("a white background should tint darker")
	}
	if r, g, b, ok := parseBackground([]byte("\x1b]11;rgb:1e1e/2020/2424\x07")); !ok || r != 30 || g != 32 || b != 36 {
		t.Fatalf("%d %d %d %v", r, g, b, ok)
	}
	if _, _, _, ok := parseBackground([]byte("nonsense")); ok {
		t.Fatal("garbage is not a colour")
	}
}

func TestGateReportShowsRulesAndStages(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	_, out, _ := invoke(t, boardFixture(), "health", "--gate", "refinement")
	for _, want := range []string{"gate refinement", "REFINEMENT", "tickets that must pass this gate fail it", "Description: at least 100 characters", "WHERE THEY ARE", "DONE · 1 ticket"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "pull request:") {
		t.Fatalf("only the named gate's reasons belong here\n%s", out)
	}
	_, out, _ = invoke(t, boardFixture(), "health", "--gate", "code_review")
	if !strings.Contains(out, "✓ All") && !strings.Contains(out, "No ticket has to pass") {
		t.Fatalf("a passing gate says so\n%s", out)
	}
}

func TestBoardsFitNarrowTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WORKFILE_CONFIG_FILE", t.TempDir()+"/absent.yml")
	for _, width := range []string{"40", "60"} {
		t.Setenv("COLUMNS", width)
		for _, args := range [][]string{{"status"}, {"status", "--user", "bob"}, {"health", "--gate", "refinement"}, {"health", "--stage", "review"}, {"health"}} {
			_, out, _ := invoke(t, boardFixture(), args...)
			for _, line := range strings.Split(out, "\n") {
				if limit, _ := strconv.Atoi(width); runewidth.StringWidth(line) > limit {
					t.Fatalf("%v at %s columns: line too wide (%d): %q", args, width, runewidth.StringWidth(line), line)
				}
			}
		}
	}
}

func TestMeGroupsFixesByPRLikeTheTicketView(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WORKFILE_CONFIG_FILE", t.TempDir()+"/absent.yml")
	pr := func(id string, n int) workfile.Record {
		return workfile.Record{ID: id, Repository: "example-team/api", URL: fmt.Sprintf("https://github.com/example-team/api/pull/%d", n), People: []string{"example-bob"},
			Facts: workfile.Facts{"state": "open", "author": "example-bob", "review_requests": []string{}, "approvals": []workfile.Actor{}, "labels": []string{}}}
	}
	tk := fixtureTicket("APP-57", "review", "short")
	tk.Title, tk.Assignee = "Improve account settings", "example-bob-account-id"
	tk.Records = map[string][]workfile.Record{"github": {pr("api#31", 31), pr("api#18", 18)}}
	_, me, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "status", "--user", "bob")
	_, single, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "status", "APP-57")
	for _, heading := range []string{"On the ticket", "example-team/api #31", "example-team/api #18"} {
		if strings.Count(me, "\n  ▌    "+heading+"\n") != 1 {
			t.Fatalf("%q must head its fixes exactly once\n%s", heading, me)
		}
	}
	for _, fix := range []string{"Expand the description to at least 100 characters — refinement.", "Get at least 1 approval — code review."} {
		if !strings.Contains(me, fix) || !strings.Contains(single, fix) {
			t.Fatalf("both views should word %q the same\nme:\n%s\nsingle:\n%s", fix, me, single)
		}
	}
	if strings.Contains(me, "✗ refinement") || strings.Contains(me, "description is") {
		t.Fatalf("fixes replace the old per-gate requirement list\n%s", me)
	}
}
