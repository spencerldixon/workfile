package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func fixSource() *fakeSource {
	f := trackerSource()
	f.records = map[string]map[string][]workfile.Record{"APP-1": {"github": {
		{ID: "app#1", URL: "https://example.com/app/pull/1", ChecksURL: "https://example.com/app/pull/1/checks",
			Facts: workfile.Facts{"state": "open", "author": "example-bob", "checks": "failure", "mergeable": "conflicting"}},
		{ID: "api#2", URL: "https://example.com/api/pull/2", ChecksURL: "https://example.com/api/pull/2/checks",
			Facts: workfile.Facts{"state": "open", "author": "example-bob", "checks": "failure"}},
		{ID: "web#3", URL: "https://example.com/web/pull/3", Facts: workfile.Facts{"state": "merged"}},
	}}}
	f.failedChecks = map[string][]workfile.FailedCheck{
		"app#1": {{Name: "test (ubuntu)", URL: "https://example.com/runs/1"}},
		"api#2": {{Name: "lint", URL: "https://example.com/runs/2"}, {Name: "build", URL: "https://example.com/runs/3"}},
	}
	return f
}

// fixDriver opens APP-1 as its owner, with the policy and failing jobs read.
func fixDriver(t *testing.T, width int) *trackerDriver {
	t.Helper()
	d := driveTracker(t, fixSource(), width, 60)
	d.m.who = map[string]string{"github": "example-bob", "jira": "example-alice"}
	d.m.fill(false)
	return d
}

func TestTrackerNumberKeysOpenEachFixWhereItIsDone(t *testing.T) {
	d := fixDriver(t, 100)
	fixes := d.m.fixes()
	if len(fixes) < 3 {
		t.Fatalf("expected numbered fixes, got %+v", fixes)
	}
	text := ansi.Strip(d.m.pane.View() + d.m.side.View())
	want := map[string]string{}
	for i, fix := range fixes {
		label := strings.TrimSpace(strings.Fields(fix.Text)[0])
		if !strings.Contains(text, string(rune('1'+i))+". "+label) {
			t.Fatalf("pane does not number fix %d (%s) as the keys do:\n%s", i+1, fix.Text, text)
		}
		switch {
		case fix.Target == "app#1" && fix.Fact == "checks":
			want[fix.Text+fix.Target] = "https://example.com/runs/1" // one failing job: its log
		case fix.Target == "api#2" && fix.Fact == "checks":
			want[fix.Text+fix.Target] = "https://example.com/api/pull/2/checks" // several: the checks page
		case fix.Target == "app#1":
			want[fix.Text+fix.Target] = "https://example.com/app/pull/1"
		}
	}
	for i, fix := range fixes {
		d.opened = nil
		d.press(string(rune('1' + i)))
		if len(d.opened) != 1 {
			t.Fatalf("key %d opened %v", i+1, d.opened)
		}
		if expected, ok := want[fix.Text+fix.Target]; ok && d.opened[0] != expected {
			t.Fatalf("fix %d (%s on %s) opened %s, want %s", i+1, fix.Text, fix.Target, d.opened[0], expected)
		}
	}
	if len(want) != 3 {
		t.Fatalf("fixture lost a fix: %v", want)
	}
	if !strings.Contains(ansi.Strip(d.m.footer()), "1-3 open fix") {
		t.Fatalf("help does not show the number keys: %s", d.m.footer())
	}
	d.opened = nil
	d.press("9")
	if len(d.opened) != 0 || !strings.Contains(d.m.notice, "no fix 9") {
		t.Fatalf("a missing fix must say so, not open anything: %v %q", d.opened, d.m.notice)
	}
}

func TestTrackerNamesFailingJobsOnTheFixLine(t *testing.T) {
	d := fixDriver(t, 100)
	text := ansi.Strip(d.m.pane.View() + d.m.side.View())
	for _, want := range []string{"Fix failing check · test (ubuntu)", "Fix failing checks · lint · build"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	reads := d.m.e.client.(*fakeSource).failedCheckReads
	if len(reads) != 2 || reads[0] != "github/app#1" || reads[1] != "github/api#2" {
		t.Fatalf("expected one read per failing open PR, got %v", reads)
	}
	for _, width := range []int{40, 60} {
		d := fixDriver(t, width)
		for _, line := range strings.Split(d.m.pane.View(), "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
}

func TestTrackerBoardCardsNeverReadFailingJobs(t *testing.T) {
	f := fixSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	d.m.overview = true
	d.run(d.m.pumpChecks())
	if len(f.failedCheckReads) != 0 {
		t.Fatalf("board cards read failing jobs: %v", f.failedCheckReads)
	}
	d.opened = nil
	d.press("1")
	if len(d.opened) != 0 {
		t.Fatal("number keys acted while the ticket was not open")
	}
}
