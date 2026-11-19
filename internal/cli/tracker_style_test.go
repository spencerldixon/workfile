package cli

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func TestTrackerSelectiveHighlightsKeepChromeUnfilled(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 40)
	e := d.m.e
	e.view.color = true
	e.view.tint.once.Do(func() { e.view.tint.code = "48;5;236" })
	d.m = newTrackerModel(context.Background(), e)
	d.m.ticketReads.pause = 0
	d.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	d.run(d.m.Init())
	if d.m.t.bg != "" {
		t.Fatal("tracker retained the panel background")
	}
	highlighted := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m`)
	if tab := d.m.tab(d.m.shownView(), true); !highlighted.MatchString(tab) {
		t.Fatalf("active tab is not highlighted: %q", tab)
	}
	background := regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?(?:4[0-8]|10[0-7])(?:;[0-9;]*)?m`)
	metadataTag := regexp.MustCompile(`[^\n]*`)
	for _, key := range []string{"down", "tab", "down", "tab", "right"} {
		d.press(key)
		if background.MatchString(d.m.header()) || background.MatchString(d.m.masthead()) {
			t.Fatal("panel background reached tracker chrome")
		}
		body := metadataTag.ReplaceAllString(d.m.pane.View(), "")
		if highlighted.MatchString(body) || highlighted.MatchString(d.m.side.View()) {
			t.Fatal("content panes must not inherit title highlights")
		}
	}
}

func TestTrackerTabPaddingAndBothSectionTitleHighlights(t *testing.T) {
	d := colourTickets(t, trackerSource(), 160, 40)
	lines := strings.Split(d.screen(), "\n")
	if lines[1] != "" || lines[3] != "" || !strings.Contains(ansi.Strip(lines[2]), "All") {
		t.Fatal("tabs need a blank line above and below")
	}
	heading := d.m.previewHeading()
	highlighted := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m`)
	if len(highlighted.FindAllString(heading, -1)) != 2 || !strings.Contains(heading, "Ticket preview") || !strings.Contains(heading, "Workfile") {
		t.Fatalf("both section titles must be highlighted: %q", heading)
	}
	d.press("tab", "down")
	d.wants("Ticket · reading")
}

func TestTrackerStatusSignalsOnlyUseKnownAssessments(t *testing.T) {
	f := trackerSource()
	d := colourTickets(t, f, 100, 40)
	item := ticketItem{workfile.Listing{Key: "APP-99", Title: "Improve search", State: "review", Status: "In Review"}}
	failed := []workfile.Check{{Outcome: "fail"}}
	for _, tc := range []struct {
		name, want string
		read       *ticketRead
	}{
		{"unread", "→ In Review", nil},
		{"pending", "→ In Review", &ticketRead{}},
		{"ready", "✓ In Review", &ticketRead{policy: &workfile.Assessment{Routes: []workfile.RouteAssessment{{To: "done"}}}}},
		{"blocked", "✗ In Review", &ticketRead{policy: &workfile.Assessment{Routes: []workfile.RouteAssessment{{To: "done", Checks: failed}}}}},
		{"out of policy", "‼ In Review", &ticketRead{policy: &workfile.Assessment{Earlier: failed}}},
		{"cannot check", "! In Review", &ticketRead{policyErr: errors.New("unavailable")}},
		{"unknown state", "! In Review", &ticketRead{policy: &workfile.Assessment{Ticket: workfile.Ticket{Error: "unknown state"}}}},
		{"done", "· In Review", &ticketRead{policy: &workfile.Assessment{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d.m.ticketReads.byKey[item.Key] = tc.read
			raw := d.m.row(item, false, nil)
			row := ansi.Strip(raw)
			if !strings.Contains(row, tc.want) {
				t.Fatalf("wanted %q, got %s", tc.want, row)
			}
			if strings.HasPrefix(tc.want, "✓") && !strings.Contains(raw, "38;5;2m") {
				t.Fatal("ready signal is not terminal green")
			}
			if strings.ContainsAny(string([]rune(tc.want)[0]), "✗‼!") && !strings.Contains(raw, "38;5;1m") {
				t.Fatal("problem signal is not terminal red")
			}
		})
	}
	if f.listingReads != 1 || len(f.policyReads) != 1 {
		t.Fatal("drawing status signals fetched more provider data")
	}
}

func TestTrackerSectionsUseOneGapAndSentenceHeadings(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 60)
	text := d.pane()
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	if strings.Contains(strings.Join(lines, "\n"), "\n\n\n") {
		t.Fatal("section padding adds more than one empty line")
	}
	for _, want := range []string{"Where it is", "Next action", "Ticket actions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing sentence-case heading %q", want)
		}
	}
}

func TestTrackerTabsKeepTheirWidthAcrossViewsAndReads(t *testing.T) {
	d := colourTickets(t, trackerSource(), 160, 40)
	d.press("right", "left") // learns who "me" is, which adds that tab's count once
	width := func() int { return ansi.StringWidth(d.m.tabs(1000)) }
	start := width()
	for _, key := range []string{"right", "right", "down", "down", "left", "left"} {
		d.press(key)
		if got := width(); got != start {
			t.Fatalf("tabs moved after %q: %d columns, was %d", key, got, start)
		}
	}
}

func TestTrackerFocusedTabHasContinuousHighlight(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	d.m.t = newTheme(true, "")
	d.press("up")
	tab := d.m.tab(d.m.shownView(), true)
	highlight := regexp.MustCompile(`^\x1b\[1;4;7m Board [0-9]+ \x1b\[m$`)
	if !highlight.MatchString(tab) {
		t.Fatalf("focused tab needs a single highlight across the label and count: %q", tab)
	}
}
