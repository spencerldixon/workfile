package cli

import (
	"os"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func TestTrackerColumnHeadingsFollowTheRows(t *testing.T) {
	for _, width := range []int{40, 60, 100} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			d := driveTracker(t, trackerSource(), width, 30)
			lines := strings.Split(d.screen(), "\n")
			heading, row := lines[d.m.topRows()-1], lines[d.m.topRows()]
			for _, pair := range [][2]string{{"Key", "APP-1"}, {"Title", "Fix redirect"}, {"Status", "To Do"}, {"Assignee", "Alice"}} {
				at := strings.Index(heading, pair[0])
				if at < 0 {
					if pair[0] == "Key" || pair[0] == "Title" || strings.Contains(row, pair[1]) {
						t.Fatalf("heading and row disagree:\n%s\n%s", heading, row)
					}
					continue
				}
				got := strings.Index(row, pair[1])
				if got < 0 || ansi.StringWidth(row[:got]) != ansi.StringWidth(heading[:at]) {
					t.Fatalf("%s is not aligned with its value:\n%s\n%s", pair[0], heading, row)
				}
			}
		})
	}
}

func TestTrackerHelpFollowsListAndReaderFocus(t *testing.T) {
	for _, width := range []int{40, 60, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			f := trackerSource()
			detail := f.details["APP-1"]
			detail.Body = para(strings.Repeat("A long description. ", 200))
			f.details["APP-1"] = detail
			d := driveTracker(t, f, width, 30)
			d.wants("↑↓ move")
			d.press("enter", "down", "down")
			d.wants("↑↓ scroll")
			d.refuses("Ticket · reading", "tab to list", "enter to read")
			d.press("tab")
			d.wants("↑↓ move", "APP-2")
			d.refuses("Ticket · reading")
		})
	}
}

func TestTrackerPolishedLayoutWithoutColourOrBackground(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		d := driveTracker(t, trackerSource(), width, 40)
		d.wants("▸ All", "▸ APP-1")
		if !strings.Contains(d.pane(), "Description") {
			t.Fatal("ticket has no description heading")
		}
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatalf("plain screen contains escapes: %q", d.screen())
		}
		// With colour but no terminal background reply, do not invent one.
		d.m.t = newTheme(true, "")
		d.m.help = d.m.t.help()
		d.send(tea.WindowSizeMsg{Width: width, Height: 40})
		if strings.Contains(d.screen(), "\x1b[48;") {
			t.Fatalf("background used without a terminal reply: %q", d.screen())
		}
	}
}

func TestTrackerDocsMatchScreen(t *testing.T) {
	d := driveWithSettings(t, trackerSource(), 100, 34, graphics{}, options{}, workfile.TrackerSettings{Columns: []string{"key", "title", "status", "assignee"}})
	syncClock(d)
	lines := strings.Split(d.screen(), "\n")[:d.m.topRows()+d.m.listRows()+1]
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	sample := strings.Join(lines, "\n")
	for _, path := range []string{"../../README.md", "../../docs/usage.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), sample) {
			t.Fatalf("%s is missing the current sample:\n%s", path, sample)
		}
	}
}

func TestTrackerNoticeFitsAndCannotInjectEscapes(t *testing.T) {
	for _, width := range []int{40, 60} {
		d := driveTracker(t, trackerSource(), width, 24)
		d.m.notice = strings.Repeat("Cannot open browser ", 10) + "\x1b[2J"
		d.check()
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("an escape reached the notice")
		}
	}
}

func TestTrackerColumnWidthsUseCellsAndBoundLongKeys(t *testing.T) {
	for _, key := range []string{"例-1", strings.Repeat("LONG", 30)} {
		f := trackerSource()
		f.listing[0].Key = key
		for _, width := range []int{40, 60} {
			d := driveTracker(t, f, width, 30)
			row := d.m.row(ticketItem{f.listing[0]}, true, nil)
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("row is %d cells in %d: %s", got, width, row)
			}
			if !strings.Contains(row, "Fix") {
				t.Fatalf("key crowded out the title: %s", row)
			}
		}
	}
}
