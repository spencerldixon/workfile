package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func TestTrackerBoardCompactCardsKeepAttentionVisible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	d := driveWithSettings(t, boardSource(), 100, 24, graphics{}, options{}, boardSettings())
	item, _ := d.m.selected()
	ticket := fixtureTicket(item.Key, "doing", strings.Repeat("Description. ", 20))
	ticket.Records = map[string][]workfile.Record{"github": {}}
	for i := 1; i <= 5; i++ {
		record := workfile.Record{ID: fmt.Sprintf("example/app#%d", i), URL: fmt.Sprintf("https://example.com/pr/%d", i), Facts: workfile.Facts{"state": "merged"}}
		if i == 5 {
			record.Facts = workfile.Facts{"state": "open", "author": "example-bob", "mergeable": "conflicting", "checks": "success"}
		}
		ticket.Records["github"] = append(ticket.Records["github"], record)
	}
	d.m.who = map[string]string{"github": "example-bob", "jira": "example-bob"}
	assessment := d.m.e.w.Assess(ticket)
	d.m.ticketReads.byKey[item.Key] = &ticketRead{policy: &assessment}
	before := len(d.m.e.client.(*fakeSource).policyReads)
	for _, tc := range []struct {
		width  int
		hidden string
	}{{24, "+4"}, {36, "+3"}, {60, "+2"}} {
		width := tc.width
		lines := d.m.boardTile(item, true, width)
		// The action may wrap to a second row rather than lose its words.
		if len(lines) > 10 {
			t.Fatalf("card is %d lines tall", len(lines))
		}
		card := strings.Join(lines, "\n")
		if head := strings.TrimSpace(ansi.Strip(lines[1])); strings.ContainsAny(head, "✗✓‼!·") {
			t.Fatalf("key row repeats the action icon: %q", head)
		}
		for _, want := range []string{"▌", "[Conflict app#5]", tc.hidden} {
			if !strings.Contains(card, want) {
				t.Fatalf("missing %q:\n%s", want, card)
			}
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("overflow: %q", line)
			}
		}
		if strings.Contains(card, "https://") || strings.Contains(card, "example/app#") {
			t.Fatal("compact card leaked long URLs or owner prefixes")
		}
	}
	if len(d.m.e.client.(*fakeSource).policyReads) != before {
		t.Fatal("drawing read provider data")
	}
}

func TestTrackerBoardPRTagsShowCachedStatus(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	records := []workfile.Record{
		{ID: "app#42", URL: "https://example.com/app/pull/42", Facts: workfile.Facts{"state": "open", "checks": "pending"}},
		{ID: "api#18", Facts: workfile.Facts{"state": "open", "review_requests": []string{"example-alice"}}},
		{ID: "web#73", Facts: workfile.Facts{"state": "merged"}},
		{ID: "app#91", Facts: workfile.Facts{"state": "open", "checks": "failure"}, Unavailable: map[string]string{"checks": "Unavailable"}},
		{ID: "api#20", Facts: workfile.Facts{"state": "open", "checks": "failure"}},
		{ID: "web#22", Facts: workfile.Facts{"state": "open", "checks": "success"}},
		{ID: "web#24", Facts: workfile.Facts{"state": "open", "checks": "success", "review_requests": []string{"example-bob"}}},
	}
	actions := []workfile.WorkAction{{State: workfile.NeedsMe, Target: "web#24", Fact: "review"}}
	d.m.t = newTheme(false, "")
	plain := "[CI pending app#42] [Review pending api#18] [Merged web#73] [Open app#91] [CI failed api#20] [CI passed web#22] [Action needed web#24]"
	if got := d.m.boardPRTags(records, actions, 150); got != plain {
		t.Fatalf("plain tags = %q, want %q", got, plain)
	}
	d.m.t, d.m.v.color = newTheme(true, ""), true
	d.m.e.tracker.Views[0].Attention = "color"
	got := d.m.boardPRTags(records, actions, 150)
	for _, want := range []string{"38;5;3m", "48;5;3m󰔛 ", "48;5;3m󰆉 api#18", "48;5;2m web#73", "48;5;8m app#91", "48;5;1m󰅖 api#20", "48;5;2m󰄬 web#22", "48;5;1m! web#24", "", records[0].URL} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in tags %q", want, got)
		}
	}
	d.m.e.tracker.Views[0].Attention = "icons"
	if got := d.m.boardPRTags(records, actions, 150); !strings.Contains(got, "48;5;8m󰆉 api#18") || strings.Contains(got, "48;5;3m") {
		t.Fatalf("icons mode lost icons or added colour: %q", got)
	}
	d.m.e.tracker.Views[0].Attention = "off"
	if got := ansi.Strip(d.m.boardPRTags(records, actions, 150)); got != "app#42 api#18 web#73 app#91 api#20 web#22 web#24" {
		t.Fatalf("off mode should show PR IDs: %q", got)
	}
}

func TestTrackerBoardCardSpacingAndTagsLast(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	item, _ := d.m.selected()
	ticket := fixtureTicket(item.Key, "doing", "short")
	ticket.Records = map[string][]workfile.Record{"github": {
		{ID: "example/app#1", Facts: workfile.Facts{"state": "open", "author": "example-bob", "checks": "failure"}},
		{ID: "example/api#2", Facts: workfile.Facts{"state": "merged"}},
	}}
	assessment := d.m.e.w.Assess(ticket)
	d.m.ticketReads.byKey[item.Key] = &ticketRead{policy: &assessment}
	d.m.who = map[string]string{"github": "example-bob"}
	lines := d.m.boardTile(item, false, 36)
	if strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[len(lines)-2]) != "" || lines[len(lines)-1] != "" {
		t.Fatalf("missing card padding or external gap: %q", lines)
	}
	card := strings.Join(lines, "\n")
	if strings.Contains(card, ">") || strings.ContainsAny(card, "▌│┃") {
		t.Fatal("unselected cards must remain borderless")
	}
	action, spacer, tags := lines[len(lines)-5], lines[len(lines)-4], lines[len(lines)-3]
	if !strings.Contains(action, "Fix failing checks on app#1") || strings.TrimSpace(spacer) != "" ||
		!strings.Contains(tags, "app#1") || !strings.Contains(tags, "api#2") {
		t.Fatalf("expected the action and a blank line above the tags:\n%s", card)
	}
	ticket.Records = nil
	assessment = d.m.e.w.Assess(ticket)
	d.m.ticketReads.byKey[item.Key] = &ticketRead{policy: &assessment}
	card = strings.Join(d.m.boardTile(item, false, 36), "\n")
	if strings.Contains(card, "No linked PRs") {
		t.Fatal("an absent PR is not useful card content")
	}
}

func TestTrackerBoardSelectedBorderOnlyMarksCurrentCard(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 140} {
		d := driveWithSettings(t, boardSource(), width, 40, graphics{}, options{}, boardSettings())
		for _, move := range []string{"", "right", "down", "left"} {
			if move != "" {
				d.press(move)
			}
			item, _ := d.m.selected()
			selected := d.m.boardTile(item, true, 30)
			ordinary := d.m.boardTile(item, false, 30)
			for _, line := range selected[:len(selected)-1] {
				if !strings.HasPrefix(line, "▌") || ansi.StringWidth(line) != 30 {
					t.Fatalf("missing full-height, width-safe border: %q", line)
				}
			}
			if selected[len(selected)-1] != "" {
				t.Fatal("border leaked into the gap between cards")
			}
			if strings.Contains(strings.Join(ordinary, "\n"), "▌") {
				t.Fatal("unselected card kept a border")
			}
			if strings.Contains(strings.Join(selected, "\n"), "selected") {
				t.Fatal("border should replace the selected label")
			}
			column := -1
			borderRows := 0
			for _, line := range strings.Split(d.screen(), "\n") {
				if strings.Count(line, "▌") > 1 {
					t.Fatal("multiple cards are bordered")
				}
				at := strings.Index(line, "▌")
				if at < 0 {
					continue
				}
				cell := ansi.StringWidth(line[:at])
				if column >= 0 && cell != column {
					t.Fatal("border moved between columns within one frame")
				}
				column, borderRows = cell, borderRows+1
			}
			if borderRows != len(selected)-1 {
				t.Fatalf("expected %d border rows, got %d", len(selected)-1, borderRows)
			}
		}
	}
}

func TestTrackerBoardTicketKeysRemainBoldAfterStatusIcon(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	d.m.t.color = true
	item, _ := d.m.selected()
	for _, selected := range []bool{false, true} {
		card := strings.Join(d.m.boardTile(item, selected, 40), "\n")
		if !strings.Contains(card, "\x1b[1m"+item.Key) {
			t.Fatalf("ticket key inherited the icon reset instead of bold: %q", card)
		}
	}
}

func TestTrackerBoardStateTitles(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"blocked", "Blocked"}, {"in_progress", "In Progress"}, {"ready-for-review", "Ready For Review"}, {"QA", "QA"}, {"étude", "Étude"},
	} {
		if got := boardStateTitle(tc.state); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestTrackerBoardCompactPRLabelKeepsItsNumber(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 40, 24, graphics{}, options{}, boardSettings())
	item, _ := d.m.selected()
	ticket := fixtureTicket(item.Key, "doing", "short")
	ticket.Records = map[string][]workfile.Record{"github": {
		{ID: "example/example-long-api-service#9876", URL: "https://example.com/app/pull/9876",
			Facts: workfile.Facts{"state": "open", "author": "example-bob", "checks": "failure"}},
	}}
	assessment := d.m.e.w.Assess(ticket)
	d.m.ticketReads.byKey[item.Key] = &ticketRead{policy: &assessment}
	d.m.who = map[string]string{"github": "example-bob"}
	d.m.t, d.m.v.color = newTheme(true, ""), true
	card := strings.Join(d.m.boardTile(item, true, 24), "\n")
	plain := ansi.Strip(card)
	if !strings.Contains(plain, "󰅖 #9876") || !strings.Contains(plain, "Fix failing checks") ||
		strings.Count(plain, "#9876") != 1 || strings.Contains(plain, "✗") {
		t.Fatalf("expected a short tag and an action with no duplicate status or PR:\n%s", plain)
	}
}

func TestTrackerBoardDesignHasAlignedCountsAndQuietChrome(t *testing.T) {
	f := inboxSource(4)
	d := driveWithSettings(t, f, 134, 24, graphics{}, options{}, boardSettings())
	d.m.e.w.Policy.States = []string{"doing", "review", "qa", "release"}
	d.m.e.w.Policy.Transitions = map[string]workfile.Transition{"doing": {To: []string{"review"}}, "review": {To: []string{"qa"}}, "qa": {To: []string{"release"}}, "release": {End: true}}
	d.m.all[1].State = "review"
	d.m.all[2].State = "review"
	d.m.all[3].State = "qa"
	d.run(d.m.showTickets())
	enableInbox(d)
	screen := d.screen()
	lines := strings.Split(screen, "\n")
	if !strings.Contains(lines[0], "workfile") || !strings.Contains(lines[2], "Board") {
		t.Fatal("app and views should share one header")
	}
	if strings.Contains(screen, "Stages ") || strings.Contains(screen, "selected ·") || strings.Contains(screen, "Needs you\n") {
		t.Fatal("old instructional chrome remains")
	}
	d.wants("s scope", "Synced", "next", "r refresh", "web#1")
	if strings.Contains(strings.ToLower(screen), "nothing here") {
		t.Fatal("empty columns should be blank")
	}
	// Wider 30-column lanes, three-column gutters and two-column margins.
	for i, want := range []string{"Doing", "Review", "Qa", "Release"} {
		start := 2 + i*33
		lane := lines[4][start : start+30]
		if !strings.HasPrefix(lane, want) || lane[29] < '0' || lane[29] > '9' {
			t.Fatalf("misaligned lane header: %q", lane)
		}
	}
	if strings.TrimSpace(lines[5]) != "" {
		t.Fatal("state headings need a blank row before the ticket cards")
	}
	for _, line := range lines[5 : len(lines)-1] {
		if strings.TrimSpace(ansi.Cut(line, 101, 131)) != "" {
			t.Fatal("the empty Release column must have no body content")
		}
	}
	if strings.Contains(screen, "https://") {
		t.Fatal("board should show compact PR links, not long addresses")
	}
	if !strings.Contains(screen, "Fix failing checks") {
		t.Fatal("short actions should fit in full, below the PR when necessary")
	}
}

func TestTrackerBoardSelectionTintsWholeCardFromOneTerminalReply(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("WORKFILE_BACKGROUND", "")
	old := terminalBackground
	calls := 0
	terminalBackground = func() (uint8, uint8, uint8, bool) { calls++; return 20, 22, 26, true }
	defer func() { terminalBackground = old }()
	d := driveWithSettings(t, boardSource(), 60, 24, graphics{}, options{}, boardSettings())
	e := d.m.e
	e.view.color = true
	e.view.tint = &tintCache{}
	d.m = newTrackerModel(context.Background(), e)
	d.send(tea.WindowSizeMsg{Width: 60, Height: 24})
	d.run(d.m.Init())
	item, _ := d.m.selected()
	selected := d.m.boardTile(item, true, 36)
	ordinary := d.m.boardTile(item, false, 36)
	if d.m.boardTheme.bg == "" || d.m.boardTheme.bg == d.m.boardSelectedTheme.bg {
		t.Fatal("selection needs a distinct terminal-derived background")
	}
	for _, line := range selected[:len(selected)-1] {
		if !strings.HasPrefix(line, "\x1b["+d.m.boardSelectedTheme.bg+"m▌") {
			t.Fatalf("border cell left a gap in the selected background: %q", line)
		}
	}
	if strings.Join(selected, "") == strings.Join(ordinary, "") {
		t.Fatal("selection is invisible")
	}
	d.press("right", "left")
	d.check()
	if calls != 1 {
		t.Fatalf("asked terminal %d times", calls)
	}
}

func TestTrackerBoardShowsSearchInHeader(t *testing.T) {
	d := driveWithSettings(t, inboxSource(3), 60, 24, graphics{}, options{}, boardSettings())
	enableInbox(d)
	d.refuses("All projects", "s change")
	d.press("/", "z")
	if !strings.Contains(d.m.header(), "/ z") {
		t.Fatal("empty search hid the filter")
	}
}
