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
	if tab := d.m.tab(d.m.shownView(), true); tab != ticketIndent+d.m.t.accent("All (4)") {
		t.Fatalf("active tab should use bold accent text: %q", tab)
	}
	background := regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?(?:4[0-8]|10[0-7])(?:;[0-9;]*)?m`)
	metadataTag := regexp.MustCompile(`[^\n]*`)
	for _, key := range []string{"down", "tab", "down", "tab", "right"} {
		d.press(key)
		if background.MatchString(d.m.navigation()) || highlighted.MatchString(d.m.navigation()) {
			t.Fatal("panel background reached tracker chrome")
		}
		body := metadataTag.ReplaceAllString(d.m.pane.View(), "")
		if highlighted.MatchString(body) || highlighted.MatchString(d.m.side.View()) {
			t.Fatal("content panes must not inherit title highlights")
		}
	}
}

func TestTrackerDividersAcrossViews(t *testing.T) {
	for _, width := range []int{40, 60, 140} {
		d := driveWithSettings(t, boardSource(), width, 30, graphics{}, options{}, boardSettings())
		for _, key := range []string{"", "enter", "esc", "]", "enter", "esc"} {
			if key != "" {
				d.press(key)
			}
			lines := strings.Split(d.screen(), "\n")
			divider := strings.Repeat("─", width)
			if lines[len(lines)-3] != "" || lines[len(lines)-2] != divider {
				t.Fatalf("%d columns: expected a blank row above the footer divider", width)
			}
			if lines[1] != divider {
				t.Fatalf("%d columns: missing full-width navigation divider", width)
			}
			if !d.m.onTicket && !d.m.showingBoard() && lines[d.m.topRows()+d.m.listRows()] != divider {
				t.Fatalf("%d columns: missing divider beneath list", width)
			}
			d.refuses("Ticket preview", "enter to read", "Ticket · reading")
		}
	}
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
		{"ready", "󰄬 In Review", &ticketRead{policy: &workfile.Assessment{Routes: []workfile.RouteAssessment{{To: "done"}}}}},
		{"blocked", "󰅖 In Review", &ticketRead{policy: &workfile.Assessment{Routes: []workfile.RouteAssessment{{To: "done", Checks: failed}}}}},
		{"out of policy", "󰀦 In Review", &ticketRead{policy: &workfile.Assessment{Earlier: failed}}},
		{"cannot check", "󰋼 In Review", &ticketRead{policyErr: errors.New("unavailable")}},
		{"unknown state", "󰋼 In Review", &ticketRead{policy: &workfile.Assessment{Ticket: workfile.Ticket{Error: "unknown state"}}}},
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
	for _, want := range []string{"Next action", "Ticket actions"} {
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

func TestTrackerBoardAndListShareNavigation(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	d.m.t = newTheme(true, "")
	syncClock(d)
	for _, view := range []string{"Board", "List"} {
		if view == "Board" {
			d.press("up")
		} else {
			d.press("]")
		}
		first, _, _ := strings.Cut(d.screen(), "\n")
		if !strings.Contains(first, d.m.t.accent(view+" (5)")) ||
			!strings.HasPrefix(ansi.Strip(first), ticketIndent+"Board (5)   List (5)") ||
			!strings.Contains(first, "\uf021 Synced 11:30:00") ||
			strings.Contains(first, "\uf017 next 11:32:00") != (view == "Board") {
			t.Fatalf("unexpected %s navigation: %q", view, first)
		}
	}
}
