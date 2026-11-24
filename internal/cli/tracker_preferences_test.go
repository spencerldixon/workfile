package cli

import (
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

func customTrackerSettings() workfile.TrackerSettings {
	return workfile.TrackerSettings{
		Views: []workfile.TrackerView{
			{ID: "my-work", Title: "My active work", Filter: workfile.TrackerFilter{Assignee: "me", States: []string{"doing", "review"}}},
			{ID: "unassigned", Title: "Unassigned", Filter: workfile.TrackerFilter{Assignee: "unassigned"}},
		},
	}
}

func TestTrackerConfiguredViewsReuseTheListing(t *testing.T) {
	f := trackerSource()
	s := customTrackerSettings()
	s.DefaultView = "unassigned"
	d := driveWithSettings(t, f, 140, 40, graphics{}, options{}, s)
	views := d.m.views()
	if len(views) != 2 || views[0].label() != "My active work" || views[1].label() != "Unassigned" {
		t.Fatalf("configured tabs changed: %+v", views)
	}
	if f.identities != 0 {
		t.Fatal("custom views asked for identity before it was needed")
	}
	if got := listKeys(d); got != "APP-3" || f.identities != 0 {
		t.Fatalf("unassigned: %s, identity reads %d", got, f.identities)
	}
	d.press("left")
	if got := listKeys(d); got != "APP-2" || f.identities != 1 {
		t.Fatalf("my active work: %s, identity reads %d", got, f.identities)
	}
	d.press("right", "left", "/", "l", "o", "g", "i", "n", "enter")
	if got := listKeys(d); got != "APP-2" || f.identities != 1 || f.listingReads != 1 || f.scope != "" {
		t.Fatalf("views fetched more scope: %s, identities %d, listings %d, scope %q", got, f.identities, f.listingReads, f.scope)
	}
}

func TestTrackerCustomStartingViewAndCLIOverride(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{{"", "APP-2"}, {"my-work", "APP-2"}, {"unassigned", "APP-3"}} {
		s := customTrackerSettings()
		s.DefaultView = "my-work"
		d := driveWithSettings(t, trackerSource(), 100, 30, graphics{}, options{view: tc.flag}, s)
		if got := listKeys(d); got != tc.want {
			t.Fatalf("--view %q: %s, want %s", tc.flag, got, tc.want)
		}
	}
	s := customTrackerSettings().WithDefaults()
	for _, flag := range []string{"all", "me", "review"} {
		if _, err := pickView(options{view: flag}, branchingTrackerPolicy(), s); err == nil {
			t.Fatalf("--view %s selected an unconfigured tab", flag)
		}
	}
}

func TestTrackerCustomViewFiltersAreCombinedAndOrderedByPolicy(t *testing.T) {
	s := customTrackerSettings()
	s.DefaultView = "my-work"
	f := trackerSource()
	f.listing[0].AssigneeID = "example-bob" // mine, but outside configured stages
	f.listing[2].AssigneeID = "example-bob"
	f.listing = []workfile.Listing{f.listing[2], f.listing[0], f.listing[1], f.listing[3]}
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	if got := listKeys(d); got != "APP-2 APP-3" {
		t.Fatalf("combined filter or pipeline order is wrong: %s", got)
	}
	f.listing[0].AssigneeID = ""
	f.listing[0].Assignee = "Alice"
	view := ticketView{custom: &s.Views[1]}
	if view.keeps(f.listing[0], "") {
		t.Fatal("a named assignee without an id is not unassigned")
	}
}

func TestTrackerConfiguredColumnsReorderAndFit(t *testing.T) {
	for _, width := range []int{40, 60, 120} {
		for _, columns := range [][]string{{"title", "key"}, {"assignee", "title", "status", "key"}, {"key", "status", "title"}} {
			s := workfile.TrackerSettings{Columns: columns}
			d := driveWithSettings(t, trackerSource(), width, 30, graphics{}, options{}, s)
			labels := strings.Split(d.screen(), "\n")[d.m.topRows()-1]
			row := d.m.row(ticketItem{trackerSource().listing[0]}, true, nil)
			last := -1
			for _, column := range d.m.rowColumns() {
				label := map[string]string{"key": "Key", "title": "Title", "assignee": "Assignee", "status": "Status"}[column.name]
				value := map[string]string{"key": "APP-1", "title": "Fix", "assignee": "Alice", "status": "To Do"}[column.name]
				at, valueAt := strings.Index(labels, label), strings.Index(row, value)
				if at <= last || valueAt < 0 || ansi.StringWidth(labels[:at]) != ansi.StringWidth(row[:valueAt]) {
					t.Fatalf("columns do not align at %d:\n%s\n%s", width, labels, row)
				}
				last = at
			}
			if !slices.Equal(d.m.e.tracker.Columns, columns) {
				t.Fatal("narrow rendering mutated configured column order")
			}
			d.press("down", "enter", "down")
		}
	}
}

func TestTrackerHiddenCommentsDoNotReadTheirImages(t *testing.T) {
	show := false
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para("Visible description"), CommentTotal: 1, Comments: []workfile.Comment{{Author: "Alice", Body: imageBlocks("comment-image")}}}
	d := driveWithSettings(t, f, 120, 50, testGraphics, options{}, workfile.TrackerSettings{Preview: workfile.TrackerPreview{Comments: &show}})
	if text := d.pane(); strings.Contains(text, "Comments") || strings.Contains(text, "comment-image") || !strings.Contains(text, "Visible description") {
		t.Fatalf("hidden comments leaked into the preview:\n%s", text)
	}
	if len(f.imageReads) != 0 {
		t.Fatalf("hidden comment images were fetched: %v", f.imageReads)
	}
}

func TestTrackerLongCustomTitlesFitWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			s := customTrackerSettings()
			s.DefaultView = "my-work"
			s.Views[0].Title = strings.Repeat("My personal active work ", 6)
			d := driveWithSettings(t, trackerSource(), width, 24, graphics{}, options{}, s)
			d.press("/", "l", "enter", "tab", "down", "tab", "right")
			if strings.Contains(d.screen(), "\x1b") {
				t.Fatal("an escape reached the plain screen")
			}
		})
	}
}

func TestTrackerCommandReadsSharedAndPrivateConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "personal"))
	t.Setenv("WORKFILE_CONFIG_FILE", filepath.Join(root, "global.yml"))
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(root, ".workfile", name), string(data))
	}
	write(workfile.PersonalPath("config.yml"), "tracker:\n  default_view: me\n")
	write(filepath.Join(root, ".workfile", "config.yml"), `tracker:
  default_view: review-queue
  views:
    - id: review-queue
      title: Needs review
      filter:
        states: [review]
    - id: all-work
      title: Everything
    - id: my-work
      title: My work
      filter:
        assignee: me
        states: [doing, review]
`)
	private := workfile.WorkspaceSettingsPath("example-web")
	write(private, "tracker:\n  default_view: my-work\n")
	for _, tc := range []struct{ flag, want string }{{"", "APP-2"}, {"all-work", "APP-1 APP-2 APP-3 APP-4"}, {"review-queue", "APP-3"}} {
		args := []string{"tracker", "--dir", root}
		if tc.flag != "" {
			args = append(args, "--view", tc.flag)
		}
		f := trackerSource()
		code, out, errOut := invoke(t, f, args...)
		if code != 0 || errOut != "" {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
		for _, key := range []string{"APP-1", "APP-2", "APP-3", "APP-4"} {
			if strings.Contains(out, key) != strings.Contains(tc.want, key) {
				t.Fatalf("%v: wrong tickets:\n%s", args, out)
			}
		}
		if f.listingReads != 1 || len(f.policyReads) != 0 || len(f.detailReads) != 0 {
			t.Fatal("plain configuration caused extra provider reads")
		}
	}
	if code, _, errOut := invoke(t, trackerSource(), "tracker", "--dir", root, "--view", "all"); code != 2 || !strings.Contains(errOut, "choose inbox, review-queue, all-work, my-work") {
		t.Fatalf("unconfigured built-in tab was selectable: %d %s", code, errOut)
	}
	write(private, "tracker:\n  default_view: missing\n")
	f := trackerSource()
	code, _, errOut := invoke(t, f, "tracker", "--dir", root)
	if code != 2 || !strings.Contains(errOut, "default_view") || f.listingReads != 0 {
		t.Fatalf("invalid settings were not rejected before reading tickets: %d %s", code, errOut)
	}
	// Presentation settings do not alter lint's policy checks.
	if code, _, errOut := invoke(t, trackerSource(), "policy", "--lint", "--dir", root); code == 2 {
		t.Fatalf("tracker preferences broke lint: %s", errOut)
	}
}

func TestTrackerCustomViewKeepsFocusOnResize(t *testing.T) {
	s := customTrackerSettings()
	s.DefaultView = "my-work"
	d := driveWithSettings(t, trackerSource(), 100, 30, graphics{}, options{}, s)
	d.press("enter")
	for _, width := range []int{40, 60} {
		d.width = width
		d.send(tea.WindowSizeMsg{Width: width, Height: 30})
		if !d.m.onTicket || d.m.shownView().custom.ID != "my-work" || listKeys(d) != "APP-2" {
			t.Fatal("resize changed the custom view or focus")
		}
	}
}
