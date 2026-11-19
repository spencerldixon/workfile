package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

func multiplePRSource() *fakeSource {
	f := inboxSource(1)
	first := f.records["APP-1"]["github"][0]
	first.Title = "Improve search"
	second := first
	second.ID, second.URL, second.Title = "api#2", "https://example.com/api/pull/2", "Add search endpoint"
	second.Facts = workfile.Facts{"state": "merged"}
	f.records["APP-1"] = map[string][]workfile.Record{"github": {first, second}}
	return f
}

func TestTrackerPRPickerOpensOnlyChosenPR(t *testing.T) {
	f := multiplePRSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	reads := len(f.policyReads)
	d.press("p")
	if !d.m.pickingPR || len(d.opened) != 0 {
		t.Fatal("multiple PRs should show a picker, not open a browser")
	}
	d.wants("Choose a pull request", "web#1", "api#2", "Fix failing checks", "Open all 2 PRs")
	d.press("down", "enter")
	if d.m.pickingPR || !slices.Equal(d.opened, []string{"https://example.com/api/pull/2"}) {
		t.Fatalf("wrong selection: %v", d.opened)
	}
	if len(f.policyReads) != reads || f.listingReads != 1 {
		t.Fatal("PR picker fetched provider data")
	}
}

func TestTrackerPRPickerOpenAllDeduplicatesAndReportsPartialFailure(t *testing.T) {
	f := multiplePRSource()
	// Duplicate evidence must not open the same browser destination twice.
	f.records["APP-1"]["other"] = []workfile.Record{f.records["APP-1"]["github"][0]}
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	d.m.open = func(target string) error {
		d.opened = append(d.opened, target)
		if strings.Contains(target, "/web/") {
			return errors.New("browser failed")
		}
		return nil
	}
	d.press("p", "down", "down", "enter")
	if len(d.opened) != 2 || d.m.pickingPR {
		t.Fatalf("Open all did not try each unique PR: %v", d.opened)
	}
	d.wants("Opened 1 of 2 PRs; 1 could not be opened.")
}

func TestTrackerPRPickerSearchCancelAndStaleResults(t *testing.T) {
	d := driveWithSettings(t, multiplePRSource(), 100, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	current := d.m.current
	d.press("p", "down", "esc")
	if d.m.pickingPR || len(d.opened) != 0 || d.m.current != current {
		t.Fatal("cancel changed the ticket or opened a browser")
	}
	d.press("p", "/", "e", "n", "d", "p", "o", "i", "n", "t", "enter")
	if !slices.Equal(d.opened, []string{"https://example.com/api/pull/2"}) {
		t.Fatalf("PR title search ignored: %v", d.opened)
	}
	d.press("s")
	index := d.m.projectPicker.Index()
	d.send(trackerProjectMsg{generation: d.m.projectGeneration - 1, msg: tea.KeyPressMsg{Code: tea.KeyDown}})
	if d.m.projectPicker.Index() != index {
		t.Fatal("late PR search changed scope selection")
	}
}

func TestTrackerPRPickerWorksInListAndTicketPreview(t *testing.T) {
	for _, preview := range []bool{false, true} {
		d := driveWithSettings(t, multiplePRSource(), 100, 30, graphics{}, options{}, workfile.TrackerSettings{})
		enableInbox(d)
		if preview {
			d.press("enter")
		}
		d.press("p")
		if !d.m.pickingPR {
			t.Fatal("PR picker missing outside board view")
		}
		d.wants("Choose a pull request")
		d.press("esc")
		if d.m.onTicket != preview {
			t.Fatal("cancel lost preview focus")
		}
	}
}

func TestTrackerPRPickerRejectsUnsafeLinksAndKeepsSinglePRDirect(t *testing.T) {
	for _, target := range []string{"file:///tmp/unsafe", "javascript:alert(1)", "https://example.com/\x1b[31m", "https://user:secret@example.com/pr/2"} {
		f := multiplePRSource()
		f.records["APP-1"]["github"][1].URL = target
		d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
		enableInbox(d)
		d.press("p")
		if d.m.pickingPR || !slices.Equal(d.opened, []string{"https://example.com/web/pull/1"}) {
			t.Fatalf("unsafe URL reached picker/browser: %v", d.opened)
		}
	}
}

func TestTrackerPRPickerFitsSmallPlainTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		f := multiplePRSource()
		f.records["APP-1"]["github"][1].Title = strings.Repeat("界search ", 15) + "\x1b[31m"
		d := driveWithSettings(t, f, width, 24, graphics{}, options{}, boardSettings())
		enableInbox(d)
		d.press("p", "down")
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("PR text injected terminal escapes")
		}
		d.width, d.height = 40, 16
		d.send(tea.WindowSizeMsg{Width: 40, Height: 16})
		d.press("/", "z", "z", "z")
		d.wants("No matching PRs.")
	}
}
