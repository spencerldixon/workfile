package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"workfile/internal/snapshot"
	"workfile/internal/workfile"
)

var longText = strings.Repeat("x", 120)

// twoRuns reads the board once, changes it, and reads it again with --since.
func twoRuns(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	f := &fakeSource{tickets: []workfile.Ticket{
		fixtureTicket("APP-1", "todo", longText),
		fixtureTicket("APP-2", "todo", longText),
		fixtureTicket("APP-3", "todo", "short"),
	}}
	if code, out, errOut := invoke(t, f, "status"); code > 1 || errOut != "" || strings.Contains(out, "Since") {
		t.Fatalf("first run: %d %s %s", code, out, errOut)
	}
	f.tickets[0].State = "doing"                 // APP-1 moves
	f.tickets[1].Facts["description"] = "short"  // APP-2 becomes blocked
	f.tickets[2].Facts["description"] = longText // APP-3 becomes ready
	return invoke(t, f, append([]string{"status"}, args...)...)
}

func TestSinceLastShowsMovedBlockedAndReady(t *testing.T) {
	code, out, errOut := twoRuns(t, "--since", "last")
	if code > 1 || errOut != "" {
		t.Fatalf("%d %s", code, errOut)
	}
	for _, want := range []string{"Since your last run, just now", "APP-1", "todo → doing", "APP-2", "✗ blocked: refinement", "APP-3", "✓ ready for doing", "· back on track"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "Since your last run") > strings.Index(out, "workfile") && strings.Contains(out, "workfile") {
		t.Errorf("the panel comes first:\n%s", out)
	}
	if !strings.Contains(out, "https://example-team.atlassian.net/browse/APP-1") {
		t.Errorf("plain output writes the address out:\n%s", out)
	}
}

func TestSinceDoesNotChangeTheExitCode(t *testing.T) {
	with, _, _ := twoRuns(t, "--since", "last")
	without, _, _ := twoRuns(t)
	if with != without {
		t.Fatalf("%d with --since, %d without", with, without)
	}
}

func TestSinceWithNoEarlierRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", longText)}}
	code, out, errOut := invoke(t, f, "status", "--since", "last")
	if code > 1 || errOut != "" || !strings.Contains(out, "Nothing to compare yet. Run wf status again later to see what changed.") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	// The run that found nothing still saved a snapshot to compare with.
	_, out, _ = invoke(t, f, "status", "--since", "last")
	if !strings.Contains(out, "Nothing changed.") {
		t.Fatalf("%s", out)
	}
}

func TestSinceSaysWhenThereIsOnlyAnOlderSnapshot(t *testing.T) {
	_, out, _ := twoRuns(t, "--since", "1d")
	if !strings.Contains(out, "(the oldest on this computer)") {
		t.Fatalf("%s", out)
	}
}

func TestSinceIsCheckedWhenOptionsAreRead(t *testing.T) {
	for _, bad := range []string{"0h", "8d", "soon", "169h"} {
		code, _, errOut := invoke(t, &fakeSource{}, "status", "--since", bad)
		if code != 2 || !strings.Contains(errOut, "--since") {
			t.Errorf("%s: %d %q", bad, code, errOut)
		}
	}
	code, _, errOut := invoke(t, &fakeSource{}, "lint", "--since", "last")
	if code != 2 || errOut == "" {
		t.Fatalf("lint takes no --since: %d %q", code, errOut)
	}
}

func TestSinceJSONAddsChangesAndSince(t *testing.T) {
	code, out, errOut := twoRuns(t, "--since", "last", "--json")
	if code > 1 || errOut != "" {
		t.Fatalf("%d %s", code, errOut)
	}
	var report struct {
		SchemaVersion int `json:"schema_version"`
		Since         string
		Changes       []snapshot.Change
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err, out)
	}
	if report.SchemaVersion != 1 || len(report.Changes) < 3 {
		t.Fatalf("%+v", report)
	}
	if _, err := time.Parse(time.RFC3339, report.Since); err != nil {
		t.Fatal(err)
	}
	_, out, _ = invoke(t, &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", longText)}}, "status", "--json")
	if strings.Contains(out, `"changes"`) || strings.Contains(out, `"since"`) {
		t.Fatalf("no --since, no changes:\n%s", out)
	}
}

func TestEveryStatusRunSavesASnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", longText)}}
	invoke(t, f, "status", "--json")
	files, _ := filepath.Glob(filepath.Join(dir, "workfile", "example-web", "snapshots", "*.json"))
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
}

func TestFailedSaveWarnsAndKeepsTheExitCode(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker) // a file where a folder is needed
	t.Setenv("NO_COLOR", "1")
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-1", "todo", longText)}}
	code, _, errOut := invoke(t, f, "status")
	if code > 1 || !strings.Contains(errOut, "wf: could not save a snapshot: ") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func sampleComparison() (comparison, []workfile.Assessment) {
	taken := time.Date(2026, 11, 13, 6, 0, 0, 0, time.Local)
	c := comparison{found: true, since: snapshot.Since{Last: true}, taken: taken, now: taken.Add(3 * time.Hour), changes: []snapshot.Change{
		{Key: "APP-1", Kind: snapshot.Moved, From: "doing", To: "review"},
		{Key: "APP-1", Kind: snapshot.BecameReady, To: "done"},
		{Key: "APP-1", Kind: snapshot.BackOnTrack},
		{Key: "APP-22", Kind: snapshot.NewlyBlocked, Gates: []string{"code_review", "pull_request", "a_gate_with_a_very_long_name"}},
		{Key: "APP-3", Kind: snapshot.OutOfPolicy, Gates: []string{"code_review"}},
		{Key: "APP-4", Kind: snapshot.Appeared},
	}}
	var all []workfile.Assessment
	for _, key := range []string{"APP-1", "APP-22", "APP-3", "APP-4"} {
		all = append(all, workfile.Assessment{Ticket: workfile.Ticket{Key: key, URL: "https://example-team.atlassian.net/browse/" + key}})
	}
	return c, all
}

func TestSincePanelWordsEachChange(t *testing.T) {
	c, all := sampleComparison()
	out := newSinceModel(view{width: 80, tint: &tintCache{}}, c, all).render()
	for _, want := range []string{"Since your last run, 3 hours ago", "doing → review", "✓ ready for done", "· back on track", "✗ blocked: code review", "‼ out of policy: code review", "new"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSincePanelIsPlainAndFitsNarrowTerminals(t *testing.T) {
	c, all := sampleComparison()
	for _, width := range []int{40, 60} {
		out := newSinceModel(view{width: width, tint: &tintCache{}}, c, all).render()
		if strings.Contains(out, "\x1b") {
			t.Fatalf("escape codes in plain output: %q", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("line %d wide at %d columns: %q", got, width, line)
			}
		}
	}
}

func TestSincePanelFitsWithColourAndBackground(t *testing.T) {
	c, all := sampleComparison()
	for _, width := range []int{40, 60} {
		v := view{out: &strings.Builder{}, color: true, links: true, width: width, tint: &tintCache{}}
		v.tint.once.Do(func() { v.tint.code = "48;5;236" })
		out := newSinceModel(v, c, all).render()
		if !strings.Contains(out, "\x1b[48;5;236m") || !strings.Contains(out, "\x1b]8;;https://example-team.atlassian.net/browse/APP-1") {
			t.Fatalf("want a background and linked keys at %d columns: %q", width, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("line %d wide at %d columns: %q", got, width, line)
			}
		}
	}
}

func TestSincePanelNeverPrintsEscapesFromProviderText(t *testing.T) {
	c, all := sampleComparison()
	c.changes = []snapshot.Change{{Key: "APP-1", Kind: snapshot.NewlyBlocked, Gates: []string{"bad\x1b[31mgate"}}}
	out := newSinceModel(view{width: 80, tint: &tintCache{}}, c, all).render()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("%q", out)
	}
}

func TestSinceWhen(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		c    comparison
		want string
	}{
		{comparison{since: snapshot.Since{Last: true}, taken: now.Add(-3 * time.Hour)}, "your last run, 3 hours ago"},
		{comparison{since: snapshot.Since{Last: true}, taken: now.Add(-time.Minute)}, "your last run, 1 minute ago"},
		{comparison{since: snapshot.Since{Ago: 24 * time.Hour}, taken: time.Date(2026, 11, 12, 9, 10, 0, 0, time.Local)}, "yesterday at 09:10"},
		{comparison{since: snapshot.Since{Ago: 12 * time.Hour}, taken: time.Date(2026, 11, 13, 7, 0, 0, 0, time.Local)}, "today at 07:00"},
		{comparison{since: snapshot.Since{Ago: 72 * time.Hour}, taken: time.Date(2026, 11, 10, 9, 10, 0, 0, time.Local), oldest: true}, "Tue 10 Nov at 09:10 (the oldest on this computer)"},
	} {
		tc.c.now = now
		if got := tc.c.when(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestHelpDescribesSince(t *testing.T) {
	if !strings.Contains(helpText("."), "  --since last|12h|1d         What changed since an earlier run on this computer\n") {
		t.Fatal("help does not describe --since")
	}
}
