package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func boardSettings() workfile.TrackerSettings {
	return workfile.TrackerSettings{Views: []workfile.TrackerView{
		{ID: "flow", Title: "Board", Layout: "board"},
		{ID: "tickets", Title: "List"},
	}}
}

func boardSource() *fakeSource {
	f := trackerSource()
	for i := range f.listing {
		f.listing[i].AssigneeID = "example-bob"
		f.listing[i].Origin = "example-app"
	}
	f.listing = append(f.listing, workfile.Listing{Key: "APP-5", Title: "Second doing ticket", State: "doing", AssigneeID: "example-bob", Origin: "example-web"})
	return f
}

func TestTrackerBoardArrowNavigationAndHighlight(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 140, 40, graphics{}, options{}, boardSettings())
	for _, tc := range []struct{ key, want string }{
		{"", "APP-1"}, {"right", "APP-2"}, {"down", "APP-5"}, {"down", "APP-5"},
		{"right", "APP-3"}, {"left", "APP-2"}, {"up", "APP-2"}, {"down", "APP-2"}, {"left", "APP-1"}, {"left", "APP-1"},
	} {
		if tc.key != "" {
			d.press(tc.key)
		}
		item, _ := d.m.selected()
		if item.Key != tc.want {
			t.Fatalf("%s selected %s, want %s", tc.key, item.Key, tc.want)
		}
		card := strings.Join(d.m.boardTile(item, true, 40), "\n")
		if !strings.Contains(card, "▌") {
			t.Fatalf("selection has no left border: %q", card)
		}
	}
	d.press("]")
	if d.m.shownView().board() {
		t.Fatal("could not switch to list")
	}
	d.wants("Title", "Ticket preview")
	d.refuses("All projects")
	d.press("[")
	if !d.m.shownView().board() {
		t.Fatal("could not switch to board")
	}
	d.wants("s scope")
	d.refuses("Ticket preview")
}

func TestTrackerBoardUpFocusesViewTabs(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 140, 40, graphics{}, options{}, boardSettings())
	d.press("up")
	if !d.m.tabFocus {
		t.Fatal("up from the top card should focus the tabs")
	}
	d.press("right")
	if d.m.shownView().board() {
		t.Fatal("right on the tabs should switch to the list")
	}
	d.press("left")
	if !d.m.shownView().board() {
		t.Fatal("left should return to the board")
	}
	d.press("up")
	d.press("down")
	if d.m.tabFocus {
		t.Fatal("down should return to the cards")
	}
}

func TestTrackerBoardDefaultsToMeAndFiltersNeutralOrigins(t *testing.T) {
	f := boardSource()
	f.listing[0].AssigneeID = "example-alice"
	s := boardSettings()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	if strings.Contains(listKeys(d), "APP-1") || !strings.Contains(listKeys(d), "APP-5") {
		t.Fatal("board is not mine across all origins")
	}
	if f.identities != 1 || f.listingReads != 1 {
		t.Fatal("unexpected identity or listing reads")
	}
	s.Views[0].Filter.Origins = []string{"EXAMPLE-APP"}
	d = driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, s)
	if strings.Contains(listKeys(d), "APP-5") {
		t.Fatal("origin filter was ignored")
	}
	s.Views[0].Filter.Assignee = "all"
	d = driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	if !strings.Contains(listKeys(d), "APP-1") {
		t.Fatal("explicit all assignees ignored")
	}
}

func TestTrackerBoardUsesPolicyOrderAndKeepsUnmappedTickets(t *testing.T) {
	f := boardSource()
	f.listing[0].State = ""
	d := driveWithSettings(t, f, 160, 40, graphics{}, options{}, boardSettings())
	lanes := d.m.boardLanes()
	for i, state := range d.m.e.w.Policy.States {
		if lanes[i].state != state {
			t.Fatal("provider order replaced policy order")
		}
	}
	if lanes[len(lanes)-1].state != "Unmapped" || len(lanes[len(lanes)-1].indices) != 1 {
		t.Fatal("unmapped ticket lost")
	}
	d.wants("Unmapped")
}

func TestTrackerBoardNarrowPlainResizeSearchAndPreview(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 140} {
		d := driveWithSettings(t, boardSource(), width, 24, graphics{}, options{}, boardSettings())
		d.wants("Board", "▌")
		d.press("right", "down", "right", "enter")
		if !d.m.onTicket {
			t.Fatal("enter did not open preview")
		}
		lines := strings.Split(d.screen(), "\n")
		if !strings.Contains(lines[4], "Ticket · reading") {
			t.Fatalf("board detail should start below the tabs:\n%s", strings.Join(lines[:5], "\n"))
		}
		d.press("esc")
		if d.m.onTicket {
			t.Fatal("escape did not return to board")
		}
		d.press("/", "l", "o", "g", "i", "n", "enter")
		d.press("right", "left", "esc")
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("plain board contains escapes")
		}
		d.width = 40
		d.send(tea.WindowSizeMsg{Width: 40, Height: 24})
	}
}

func TestTrackerBoardCardsShowPRsAndTheirActionsWithoutReading(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 140, 40, graphics{}, options{}, boardSettings())
	item, _ := d.m.selected()
	ticket := fixtureTicket(item.Key, "doing", "text")
	ticket.Records = map[string][]workfile.Record{"github": {
		{ID: "example/app#1", URL: "https://example.com/pr/1", Facts: workfile.Facts{"state": "open", "author": "example-bob", "mergeable": "conflicting"}},
		{ID: "example/api#2", URL: "https://example.com/pr/2", Facts: workfile.Facts{"state": "merged"}},
	}}
	assessment := d.m.e.w.Assess(ticket)
	d.m.ticketReads.byKey[item.Key] = &ticketRead{policy: &assessment}
	d.m.who = map[string]string{"github": "example-bob"}
	before := len(f.policyReads) + len(f.detailReads)
	card := strings.Join(d.m.boardCard(item, true, 60), "\n")
	for _, want := range []string{"example/app#1", "example/api#2", "Resolve merge conflicts", "https://example.com/pr/1"} {
		if !strings.Contains(card, want) {
			t.Fatalf("missing %q:\n%s", want, card)
		}
	}
	if len(f.policyReads)+len(f.detailReads) != before {
		t.Fatal("rendering fetched data")
	}
	d.m.e.tracker.Views[0].Attention = "off"
	card = strings.Join(d.m.boardCard(item, true, 60), "\n")
	if strings.Contains(card, "Resolve merge conflicts") || !strings.Contains(card, "example/app#1") {
		t.Fatal("attention off hid PRs or kept action")
	}
	d.m.e.tracker.Views[0].Attention = "icons"
	d.m.t.color = true
	card = strings.Join(d.m.boardCard(item, false, 60), "\n")
	if strings.Contains(card, "38;5;1") {
		t.Fatal("icons-only mode used attention colour")
	}
}

func TestTrackerBoardBackgroundReadsAreBoundedAndPreviewIsOnDemand(t *testing.T) {
	f := inboxSource(30)
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	if len(f.policyReads) != trackerCheckBatch || len(f.detailReads) != 0 || f.listingReads != 1 {
		t.Fatalf("unexpected reads: %d policy, %d detail", len(f.policyReads), len(f.detailReads))
	}
	d.press("down", "r")
	if len(f.detailReads) != 0 || f.listingReads != 2 || len(f.policyReads) != 24 {
		t.Fatal("board refresh must reload the listing and one bounded check batch, not rich details")
	}
	d.press("enter")
	if len(f.detailReads) != 1 {
		t.Fatal("preview did not read details")
	}
	d.press("esc", "n")
	if len(f.policyReads) != 36 || len(f.detailReads) != 1 {
		t.Fatalf("check-more or refresh exceeded budget: %d", len(f.policyReads))
	}
}

func TestTrackerBoardPlainOutputKeepsPRsActionsAndSampleCaption(t *testing.T) {
	f := inboxSource(15)
	d := driveWithSettings(t, f, 60, 24, graphics{}, options{}, boardSettings())
	var output bytes.Buffer
	e := d.m.e
	e.view.out = &output
	code, err := printTrackerBoard(context.Background(), e, f.listing, d.m.shownView(), "example-bob")
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	text := output.String()
	for _, want := range []string{"Doing 15", "web#1", "https://example.com/web/pull/1", "Fix failing checks", "Checked 12 of 15", "PRs not checked"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\x1b") || len(f.detailReads) != 0 {
		t.Fatal("plain board emitted escapes or fetched rich details")
	}
}

func TestTrackerBoardSelectionStaysVisibleInLongColumn(t *testing.T) {
	f := boardSource()
	for i := 0; i < 15; i++ {
		item := f.listing[1]
		item.Key += strings.Repeat("X", i+1)
		f.listing = append(f.listing, item)
	}
	d := driveWithSettings(t, f, 40, 16, graphics{}, options{}, boardSettings())
	d.press("right")
	for i := 0; i < 15; i++ {
		d.press("down")
	}
	item, _ := d.m.selected()
	if !strings.Contains(ansi.Strip(d.screen()), "▌") || !strings.Contains(d.screen(), item.Key) {
		t.Fatalf("selected card scrolled out:\n%s", d.screen())
	}
}
