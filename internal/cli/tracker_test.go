package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

type trackerDriver struct {
	t             *testing.T
	m             *trackerModel
	width, height int
	quit          bool
	opened        []string
	raw           []string // what went to the terminal outside the screen
}

func para(text string) []workfile.Block {
	return []workfile.Block{{Kind: workfile.BlockParagraph, Spans: []workfile.Span{{Text: text}}}}
}

func trackerSource() *fakeSource {
	f := &fakeSource{details: map[string]workfile.Detail{}}
	for _, l := range []struct{ key, title, status, state, who string }{
		{"APP-1", "Fix redirect loop on expired login", "To Do", "todo", "Alice"},
		{"APP-2", "Add single sign-on login", "In Progress", "doing", "Bob"},
		{"APP-3", "Clear account settings page", "In Review", "review", ""},
		{"APP-4", "Tidy the billing screen", "Done", "done", "Charlie"},
	} {
		id := ""
		if l.who != "" {
			id = "example-" + strings.ToLower(l.who)
		}
		f.listing = append(f.listing, workfile.Listing{Key: l.key, Title: l.title, Status: l.status, State: l.state, Assignee: l.who, AssigneeID: id, Type: "Story", URL: "https://example.atlassian.net/browse/" + l.key})
		f.tickets = append(f.tickets, fixtureTicket(l.key, "doing", "text"))
		f.details[l.key] = workfile.Detail{Key: l.key, Body: para("Body of " + l.key)}
	}
	return f
}

func driveTracker(t *testing.T, f *fakeSource, width, height int) *trackerDriver {
	t.Helper()
	return driveWith(t, f, width, height, graphics{}, options{})
}

func driveWith(t *testing.T, f *fakeSource, width, height int, g graphics, o options) *trackerDriver {
	t.Helper()
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	e := env{view: view{out: &bytes.Buffer{}, width: width, tint: &tintCache{}}, w: w, client: f, o: o, errOut: io.Discard}
	d := &trackerDriver{t: t, m: newTrackerModel(context.Background(), e), width: width, height: height}
	d.m.pause = 0
	d.m.gfx = g
	d.m.open = func(target string) error { d.opened = append(d.opened, target); return nil }
	d.send(tea.WindowSizeMsg{Width: width, Height: height})
	d.run(d.m.Init())
	return d
}

func (d *trackerDriver) send(msg tea.Msg) {
	d.t.Helper()
	_, cmd := d.m.Update(msg)
	d.run(cmd)
	d.check()
}

func (d *trackerDriver) run(cmd tea.Cmd) {
	d.t.Helper()
	if cmd == nil {
		return
	}
	// Only the messages this screen acts on. A spinner or the search box's
	// cursor schedules itself for ever, so following those would never end.
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
	case tea.QuitMsg:
		d.quit = true
	case tea.RawMsg:
		d.raw = append(d.raw, fmt.Sprint(msg.Msg))
	case ticketImageMsg, ticketTitleMsg, ticketMeMsg:
		d.send(msg)
	case ticketListMsg, ticketSettledMsg, ticketDetailMsg, ticketPolicyMsg, ticketOpenedMsg, list.FilterMatchesMsg:
		d.send(msg)
	}
}

func (d *trackerDriver) press(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		d.send(keyPress(k))
	}
}

func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func (d *trackerDriver) check() {
	d.t.Helper()
	lines := strings.Split(d.m.render(), "\n")
	if len(lines) > d.height {
		d.t.Fatalf("%d lines in %d rows:\n%s", len(lines), d.height, d.m.render())
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > d.width {
			d.t.Fatalf("%d columns in %d: %q", w, d.width, line)
		}
	}
}

func (d *trackerDriver) screen() string { return d.m.render() }

// pane is all the ticket text, including what is scrolled out of sight.
func (d *trackerDriver) pane() string {
	main, side := d.m.ticketText()
	return main + side
}

func (d *trackerDriver) wants(texts ...string) {
	d.t.Helper()
	for _, text := range texts {
		if !strings.Contains(d.screen(), text) {
			d.t.Fatalf("missing %q:\n%s", text, d.screen())
		}
	}
}

func (d *trackerDriver) refuses(texts ...string) {
	d.t.Helper()
	for _, text := range texts {
		if strings.Contains(d.screen(), text) {
			d.t.Fatalf("unexpected %q:\n%s", text, d.screen())
		}
	}
}

func TestTrackerShowJiraAndWorkfileTogether(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para("Why this matters"), CommentTotal: 2, Comments: []workfile.Comment{{Author: "Alice", Created: time.Now().Add(-48 * time.Hour), Body: para("Looks good")}}}
	d := driveTracker(t, f, 100, 60)
	d.wants("4 of 4", "APP-1", "APP-4", "Why this matters", "Alice", "Looks good", "Comments (2)", "Showing 1 of 2", "doing")
}

func TestTrackerFitEveryTerminal(t *testing.T) {
	for _, size := range [][2]int{{40, 24}, {60, 24}, {40, 10}, {60, 12}, {140, 40}} {
		f := trackerSource()
		f.listing[0].Title = strings.Repeat("A very long ticket title ", 8)
		f.listing[0].Assignee = "Someone With An Extremely Long Name"
		long := "https://example.com/" + strings.Repeat("x", 90)
		f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{
			{Kind: workfile.BlockHeading, Level: 1, Spans: []workfile.Span{{Text: strings.Repeat("Heading ", 20)}}},
			{Kind: workfile.BlockParagraph, Spans: []workfile.Span{{Text: long, Link: long}}},
			{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: strings.Repeat("y", 120), Code: true}}},
			{Kind: workfile.BlockList, Level: 3, Marker: "•", Spans: []workfile.Span{{Text: strings.Repeat("item ", 40)}}},
			{Kind: workfile.BlockTable, Header: true, Rows: [][]string{{strings.Repeat("h", 70), "b"}, {"c", "d"}}},
			{Kind: workfile.BlockRule},
			{Kind: workfile.BlockImage, Name: strings.Repeat("n", 90) + ".png"},
		}}
		d := driveTracker(t, f, size[0], size[1])
		d.press("tab", "down", "tab", "/", "a", "esc", "down")
		d.width++
		d.send(tea.WindowSizeMsg{Width: d.width, Height: size[1]})
	}
}

func TestTrackerFitWithColourAndPanels(t *testing.T) {
	for _, width := range []int{40, 60} {
		f := trackerSource()
		long := "https://example.com/" + strings.Repeat("x", 90)
		f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{
			{Kind: workfile.BlockParagraph, Spans: []workfile.Span{{Text: strings.Repeat("bold words ", 20), Bold: true}, {Text: long, Link: long}, {Text: "code", Code: true}, {Text: "slanted", Italic: true}}},
		}}
		d := driveTracker(t, f, width, 40)
		d.m.v.color, d.m.v.links = true, true
		d.m.t = newTheme(true, "48;5;236")
		d.m.help = d.m.t.help()
		d.send(tea.WindowSizeMsg{Width: width, Height: 40})
		d.m.fill(true)
		d.check()
		if !strings.Contains(d.screen(), "\x1b]8;;https://example.atlassian.net/browse/APP-1") {
			t.Fatalf("the ticket key is not a link:\n%q", d.screen())
		}
	}
}

// A tracker with no ticket types and keys that look nothing like Jira's must
// work the same, because the screen treats every field as the tracker's text.
func TestTrackerWorkWithAnyTrackersShape(t *testing.T) {
	f := &fakeSource{details: map[string]workfile.Detail{}}
	for _, key := range []string{"example-team/api#1042", "example-team/web#7"} {
		f.listing = append(f.listing, workfile.Listing{Key: key, Title: "Issue " + key, Status: "open", URL: "https://github.example.com/" + key})
		f.tickets = append(f.tickets, fixtureTicket(key, "doing", "text"))
		f.details[key] = workfile.Detail{Key: key, Body: para("Body of " + key), CommentTotal: 1, Comments: []workfile.Comment{{Author: "Alice", Body: para("No date here")}}}
	}
	for _, width := range []int{40, 60, 100} {
		d := driveTracker(t, f, width, 60)
		d.wants("example-team/api#1042", "Body of example-team/api#1042", "No date here", "Unassigned")
		d.refuses("· ·", "Jira", "Story")
		d.press("down", "o")
		if !slices.Equal(d.opened, []string{"https://github.example.com/example-team/web#7"}) {
			t.Fatal(d.opened)
		}
	}
}

func TestTrackerLettersInSearchNeverFireShortcuts(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 24)
	d.press("/", "q", "o", "r")
	if d.quit || len(d.opened) != 0 {
		t.Fatalf("a shortcut fired while searching: quit %v, opened %v", d.quit, d.opened)
	}
	d.wants("/ qor")
}

func TestTrackerFuzzySearch(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 40)
	d.press("/", "b", "i", "l", "l")
	d.wants("1 of 4", "APP-4")
	d.refuses("APP-2  ")
	d.press("enter")
	d.wants("/ bill", "Body of APP-4")
	d.press("esc")
	d.wants("4 of 4", "APP-2")
}

func TestTrackerSearchWithNoMatchesSaysSo(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 24)
	d.press("/", "z", "z", "z", "z")
	d.wants("No tickets match", "0 of 4")
}

func TestTrackerEmptyScopeSaysSo(t *testing.T) {
	f := trackerSource()
	f.listing = nil
	d := driveTracker(t, f, 80, 24)
	d.wants("No tickets in your scope")
	d.press("o", "r", "tab", "down", "/", "x")
	if len(d.opened) != 0 {
		t.Fatal(d.opened)
	}
}

func TestTrackerEmptyTicketIsCalm(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1"}
	f.listing[0].Assignee = ""
	d := driveTracker(t, f, 80, 60)
	d.wants("No description", "No comments", "Unassigned")
}

func TestTrackerReadOnlyTheTicketYouStopOn(t *testing.T) {
	f := trackerSource()
	d := driveTracker(t, f, 80, 24)
	f.detailReads, f.policyReads = nil, nil
	var pending []tea.Cmd
	for range 3 {
		_, cmd := d.m.Update(keyPress("down"))
		pending = append(pending, cmd)
	}
	for _, cmd := range pending {
		d.run(cmd)
	}
	if !slices.Equal(f.detailReads, []string{"APP-4"}) || !slices.Equal(f.policyReads, []string{"APP-4"}) {
		t.Fatalf("read %v and %v", f.detailReads, f.policyReads)
	}
}

func TestTrackerCacheUntilRefresh(t *testing.T) {
	f := trackerSource()
	d := driveTracker(t, f, 80, 24)
	d.press("down", "up", "down", "up")
	if !slices.Equal(f.detailReads, []string{"APP-1", "APP-2"}) {
		t.Fatalf("read %v", f.detailReads)
	}
	d.press("r")
	if !slices.Equal(f.detailReads, []string{"APP-1", "APP-2", "APP-1"}) {
		t.Fatalf("read %v", f.detailReads)
	}
}

func TestTrackerLateReplyNeverDrawsOverTheNewTicket(t *testing.T) {
	f := trackerSource()
	d := driveTracker(t, f, 80, 40)
	d.press("r") // leaves a fresh read of APP-1
	_, slow := d.m.Update(keyPress("r"))
	d.press("down")
	d.wants("Body of APP-2")
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para("Late text")}
	d.run(slow)
	d.refuses("Late text")
	d.wants("Body of APP-2")
}

func TestTrackerOpenInTheBrowser(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 24)
	d.press("down", "o")
	d.press("tab", "o")
	want := "https://example.atlassian.net/browse/APP-2"
	if !slices.Equal(d.opened, []string{want, want}) {
		t.Fatal(d.opened)
	}
	d.m.open = func(string) error { return errors.New("no browser") }
	d.press("o")
	d.wants("Cannot open the browser")
}

func TestTrackerFocusScrollsTheTicket(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para(strings.Repeat("words go here ", 300))}
	d := driveTracker(t, f, 60, 24)
	d.press("tab", "down", "down")
	if d.m.pane.YOffset() == 0 {
		t.Fatal("the ticket did not scroll")
	}
	if selected := d.m.list.SelectedItem().(ticketItem); selected.Key != "APP-1" {
		t.Fatalf("the list moved to %s", selected.Key)
	}
	d.press("esc", "down")
	if !strings.Contains(d.pane(), "Body of APP-2") {
		t.Fatal(d.pane())
	}
	if d.m.pane.YOffset() != 0 {
		t.Fatal("a new ticket should start at the top")
	}
}

func TestTrackerListGivesItsSpareRoomToTheTicket(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 30)
	if d.m.listRows() != 4 {
		t.Fatalf("%d rows for 4 tickets", d.m.listRows())
	}
	f := trackerSource()
	for range 80 {
		f.listing = append(f.listing, f.listing[0])
	}
	if d = driveTracker(t, f, 80, 30); d.m.listRows() != 14 {
		t.Fatalf("%d rows, want the top half", d.m.listRows())
	}
}

func colourTickets(t *testing.T, f *fakeSource, width, height int) *trackerDriver {
	t.Helper()
	d := driveTracker(t, f, width, height)
	d.m.v.color, d.m.v.links = true, true
	d.m.t = newTheme(true, "48;5;236")
	d.m.help = d.m.t.help()
	d.send(tea.WindowSizeMsg{Width: width, Height: height})
	return d
}

func TestTrackerFillTheWholeTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {200, 50}, {300, 70}} {
		d := colourTickets(t, trackerSource(), size[0], size[1])
		lines := strings.Split(d.screen(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		if w := ansi.StringWidth(lines[0]); w != size[0] {
			t.Fatalf("%v: the header is %d wide", size, w)
		}
		if w := ansi.StringWidth(lines[1]); w != size[0] {
			t.Fatalf("%v: the current row is %d wide", size, w)
		}
	}
}

func TestTrackerHighlightTheCurrentRow(t *testing.T) {
	d := colourTickets(t, trackerSource(), 100, 30)
	rows := strings.Split(d.screen(), "\n")
	if !strings.Contains(rows[1], "\x1b[7") || strings.Contains(rows[2], "\x1b[7") {
		t.Fatalf("%q\n%q", rows[1], rows[2])
	}
	d.press("down")
	rows = strings.Split(d.screen(), "\n")
	if strings.Contains(rows[1], "\x1b[7") || !strings.Contains(rows[2], "\x1b[7") {
		t.Fatalf("%q\n%q", rows[1], rows[2])
	}
	// Reading the ticket softens the bar, so the focus is plain to see.
	d.press("tab")
	rows = strings.Split(d.screen(), "\n")
	if strings.Contains(rows[2], "\x1b[7") || !strings.Contains(rows[2], "48;5;236") {
		t.Fatalf("%q", rows[2])
	}
	// Without colour the marker still says which row it is.
	plain := driveTracker(t, trackerSource(), 100, 30)
	plain.wants("▸ APP-1")
}

func TestTrackerUnderlineWhatTheSearchMatched(t *testing.T) {
	d := colourTickets(t, trackerSource(), 100, 30)
	d.press("/", "b", "i", "l", "l")
	rows := strings.Split(d.screen(), "\n")
	if !regexp.MustCompile(`\x1b\[[0-9;:]*4[;:m0-9]*`).MatchString(rows[1]) || !strings.Contains(ansi.Strip(rows[1]), "billing") {
		t.Fatalf("%q", rows[1])
	}
}

func TestTrackerUseBothColumnsOnWideTerminals(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para(strings.Repeat("A long description goes on. ", 200))}
	d := driveTracker(t, f, 160, 30)
	d.wants("A long description", "NEEDS ATTENTION", "WHERE IT IS")
	narrow := driveTracker(t, f, 90, 30)
	narrow.refuses("A long description")
}

func TestTrackerKeepBlockersInViewWhileReading(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para(strings.Repeat("A long description goes on. ", 400))}
	d := driveTracker(t, f, 160, 40)
	d.press("tab", "down", "down", "down")
	d.wants("NEEDS ATTENTION")
}

func TestTrackerShowScrollPosition(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: para(strings.Repeat("A long description goes on. ", 400))}
	d := driveTracker(t, f, 80, 24)
	d.wants("0%")
	short := driveTracker(t, trackerSource(), 160, 60)
	short.refuses("0%")
}

func TestTrackerNestComments(t *testing.T) {
	f := trackerSource()
	f.listing[0].Assignee = ""
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", CommentTotal: 3, Comments: []workfile.Comment{{
		Author: "Alice", Body: para("Top level"),
		Replies: []workfile.Comment{{Author: "Bob", Body: para("A reply"), Replies: []workfile.Comment{{Author: "Charlie", Body: para("A reply to it")}}}},
	}}}
	d := driveTracker(t, f, 100, 70)
	indent := func(text string) int {
		for _, line := range strings.Split(d.pane(), "\n") {
			if i := strings.Index(line, text); i >= 0 {
				return i
			}
		}
		t.Fatalf("missing %q:\n%s", text, d.screen())
		return 0
	}
	if !(indent("Alice") == indent("Top level") && indent("Top level") < indent("A reply") && indent("A reply") < indent("A reply to it")) {
		t.Fatalf("not nested:\n%s", d.pane())
	}
	d.wants("↳ Bob", "↳ Charlie")
	d.refuses("Showing")
}

func TestEachCommentIsABlockAndRepliesSitInside(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", CommentTotal: 3, Comments: []workfile.Comment{
		{Author: "Alice", Body: para("First"), Replies: []workfile.Comment{{Author: "Bob", Body: para("A reply")}}},
		{Author: "Charlie", Body: para("Second")},
	}}
	d := colourTickets(t, f, 100, 70)
	d.m.fill(true)
	first, reply, second := -1, -1, -1
	lines := strings.Split(d.pane(), "\n")
	for i, line := range lines {
		switch {
		case strings.Contains(line, "First"):
			first = i
		case strings.Contains(line, "A reply"):
			reply = i
		case strings.Contains(line, "Second"):
			second = i
		}
	}
	if first < 0 || !(first < reply && reply < second) {
		t.Fatalf("%d %d %d:\n%s", first, reply, second, d.pane())
	}
	// A block starts at the left edge and has a gap above it. A reply's block
	// starts further in, on the terminal's own background.
	if !strings.HasPrefix(lines[first], "\x1b[48;5;236m") {
		t.Fatalf("%q", lines[first])
	}
	if !strings.HasPrefix(lines[reply], "   \x1b[48;5;236m") {
		t.Fatalf("%q", lines[reply])
	}
	if w := ansi.StringWidth(lines[reply]); w != 100 {
		t.Fatalf("a reply block ends at %d", w)
	}
	gap := false
	for _, line := range lines[first:second] {
		gap = gap || line == ""
	}
	if !gap {
		t.Fatal("blocks should have a gap between them")
	}
}

func TestDeepRepliesKeepRoomOnNarrowTerminals(t *testing.T) {
	deep := workfile.Comment{Author: "Charlie", Body: para(strings.Repeat("words ", 30))}
	for range 8 {
		deep = workfile.Comment{Author: "Bob", Body: para("nested"), Replies: []workfile.Comment{deep}}
	}
	for _, width := range []int{21, 40, 60} {
		f := trackerSource()
		f.details["APP-1"] = workfile.Detail{Key: "APP-1", CommentTotal: 9, Comments: []workfile.Comment{deep}}
		d := driveTracker(t, f, width, 200)
		d.wants("Charlie")
	}
}

func TestEnterReadsTheTicketAndTabComesBack(t *testing.T) {
	d := driveTracker(t, trackerSource(), 80, 24)
	d.press("enter")
	if !d.m.onTicket {
		t.Fatal("enter should read the ticket")
	}
	d.wants("tab list")
	d.press("tab")
	if d.m.onTicket {
		t.Fatal("tab should come back to the list")
	}
	d.press("tab")
	if !d.m.onTicket {
		t.Fatal("tab should also read the ticket")
	}
	d.press("esc", "/", "b", "i", "l", "l", "enter")
	if d.m.onTicket {
		t.Fatal("the first enter only keeps the search")
	}
	d.wants("/ bill")
	d.press("enter")
	if !d.m.onTicket {
		t.Fatal("the next enter reads the ticket")
	}
}

func TestTrackerFollowThePipeline(t *testing.T) {
	f := trackerSource()
	byKey := func(key, state string) workfile.Listing {
		return workfile.Listing{Key: key, Title: key, State: state, Status: state}
	}
	f.listing = []workfile.Listing{byKey("APP-9", "done"), byKey("APP-8", ""), byKey("APP-7", "review"), byKey("APP-6", "todo"), byKey("APP-5", "doing"), byKey("APP-4", "todo")}
	want := []string{"APP-6", "APP-4", "APP-5", "APP-7", "APP-9", "APP-8"}
	d := driveTracker(t, f, 80, 30)
	var got []string
	for _, item := range d.m.list.Items() {
		got = append(got, item.(ticketItem).Key)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
	_, out, _ := invoke(t, f, "tracker")
	at := -1
	for _, key := range want {
		i := strings.Index(out, key)
		if i <= at {
			t.Fatalf("%s is out of order:\n%s", key, out)
		}
		at = i
	}
}

func listKeys(d *trackerDriver) string {
	var keys []string
	for _, item := range d.m.list.VisibleItems() {
		keys = append(keys, item.(ticketItem).Key)
	}
	return strings.Join(keys, " ")
}

func TestViewsRunAllThenMeThenEachState(t *testing.T) {
	f := trackerSource()
	d := driveTracker(t, f, 100, 30)
	if listKeys(d) != "APP-1 APP-2 APP-3 APP-4" {
		t.Fatalf("the default should show everything: %s", listKeys(d))
	}
	d.wants("All 4", "Assigned to me", "Todo 1", "Doing 1", "Review 1", "Done 1")
	head := strings.Split(d.screen(), "\n")[0]
	at := -1
	for _, label := range []string{"All", "Assigned to me", "Todo", "Doing", "Review", "Done"} {
		i := strings.Index(head, label)
		if i <= at {
			t.Fatalf("%s is out of order: %q", label, head)
		}
		at = i
	}
	for _, want := range []string{"APP-2", "APP-1", "APP-2", "APP-3", "APP-4", "APP-1 APP-2 APP-3 APP-4"} {
		d.press("right")
		if listKeys(d) != want {
			t.Fatalf("got %s, want %s", listKeys(d), want)
		}
	}
	d.press("left")
	if listKeys(d) != "APP-4" {
		t.Fatalf("left from All should wrap to the last state: %s", listKeys(d))
	}
	if item, _ := d.m.selected(); item.Key != "APP-4" {
		t.Fatalf("the selection should follow the view: %s", item.Key)
	}
}

func TestViewsMarkTheCurrentTabWithoutColour(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.wants("[All 4]")
	d.press("right")
	d.wants("[Assigned to me 1]")
	d.refuses("[All 4]")
}

func TestViewsWorkFromTheTicketAndKeepItsFocus(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.press("enter", "right")
	if !d.m.onTicket || listKeys(d) != "APP-2" {
		t.Fatalf("%v %s", d.m.onTicket, listKeys(d))
	}
}

func TestArrowsStayInTheSearchWhileTyping(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.press("/", "b", "right", "left")
	if d.m.view != 0 {
		t.Fatalf("the view moved to %d while typing", d.m.view)
	}
}

func TestSearchWorksInsideAView(t *testing.T) {
	f := trackerSource()
	f.listing[1].State = "review"
	d := driveTracker(t, f, 100, 30)
	d.press("left", "left", "/", "c", "l", "e", "a", "r", "enter")
	if listKeys(d) != "APP-3" {
		t.Fatal(listKeys(d))
	}
	d.wants("1 of 2")
}

func TestViewsKeepTheCurrentTicketWhenItIsStillThere(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.press("down", "right")
	if item, _ := d.m.selected(); item.Key != "APP-2" {
		t.Fatalf("moved to %s", item.Key)
	}
}

func TestMeAsksWhoYouAreOnlyWhenNeededAndOnce(t *testing.T) {
	f := trackerSource()
	d := driveTracker(t, f, 100, 30)
	if f.identities != 0 {
		t.Fatal("nobody asked who you are yet")
	}
	d.press("right", "left", "right")
	if f.identities != 1 {
		t.Fatalf("asked %d times", f.identities)
	}
}

func TestAnEmptyViewSaysSo(t *testing.T) {
	f := trackerSource()
	f.listing = f.listing[:1]
	d := driveTracker(t, f, 100, 30)
	d.press("right")
	d.wants("Nothing assigned to you")
	d.press("left", "left")
	d.wants("Nothing in Done")
}

func TestUnmappedTicketsShowOnlyInAll(t *testing.T) {
	f := trackerSource()
	f.listing[0].State = ""
	d := driveTracker(t, f, 100, 30)
	d.wants("All 4", "Todo 0")
}

func TestTrackerSayWhenTheyCannotTellWhoYouAre(t *testing.T) {
	f := trackerSource()
	f.identityErr = errors.New("provider returned HTTP 401")
	d := driveTracker(t, f, 100, 30)
	d.press("right")
	d.wants("Cannot tell who you are")
	d.press("left")
	if listKeys(d) != "APP-1 APP-2 APP-3 APP-4" {
		t.Fatal(listKeys(d))
	}
	f.identityErr = nil
	d.press("right")
	if listKeys(d) != "APP-2" {
		t.Fatalf("going back should try again: %s", listKeys(d))
	}
}

func TestAssignedToMeIsATabEvenWithOnlyMine(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.m.e.w.Settings.OnlyMine = true
	d.press("right")
	d.wants("[Assigned to me 1]")
	if listKeys(d) != "APP-2" {
		t.Fatal(listKeys(d))
	}
}

func TestAssignedToMeCanBeWhereYouStart(t *testing.T) {
	d := driveWith(t, trackerSource(), 100, 30, graphics{}, options{view: "me"})
	if listKeys(d) != "APP-2" {
		t.Fatal(listKeys(d))
	}
	d.wants("[Assigned to me 1]")
	_, out, _ := invoke(t, trackerSource(), "tracker", "--view", "me")
	if !strings.Contains(out, "APP-2") || strings.Contains(out, "APP-1") {
		t.Fatal(out)
	}
	f := trackerSource()
	f.identityErr = errors.New("provider returned HTTP 401")
	if code, _, errOut := invoke(t, f, "tracker", "--view", "me"); code != 2 || !strings.Contains(errOut, "401") {
		t.Fatalf("%d %q", code, errOut)
	}
	d = driveWith(t, f, 100, 30, graphics{}, options{view: "me"})
	if !d.quit || d.m.fatal == nil {
		t.Fatal("without knowing who you are, this view cannot show the right tickets")
	}
}

func TestUnassignedTicketsAppearInAllAndTheirState(t *testing.T) {
	d := driveTracker(t, trackerSource(), 100, 30)
	d.wants("Unassigned")
	d.press("left", "left")
	if listKeys(d) != "APP-3" {
		t.Fatal(listKeys(d))
	}
	d.wants("Unassigned")
}

func TestViewOptionPicksWhereYouStart(t *testing.T) {
	d := driveWith(t, trackerSource(), 100, 30, graphics{}, options{view: "review"})
	if listKeys(d) != "APP-3" {
		t.Fatal(listKeys(d))
	}
	_, out, _ := invoke(t, trackerSource(), "tracker", "--view", "todo")
	if !strings.Contains(out, "APP-1") || strings.Contains(out, "APP-2") {
		t.Fatal(out)
	}
	for _, view := range []string{"all", "me", "doing", "Done"} {
		if code, _, errOut := invoke(t, trackerSource(), "tracker", "--view", view); code != 0 {
			t.Errorf("%s: %s", view, errOut)
		}
	}
	code, _, errOut := invoke(t, trackerSource(), "tracker", "--view", "sideways")
	if code != 2 || !strings.Contains(errOut, "all, me, todo, doing, review, done") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestTrackerRejectsScopeOption(t *testing.T) {
	f := trackerSource()
	code, _, errOut := invoke(t, f, "tracker", "--scope", "project = EXAMPLE")
	if code != 2 || !strings.Contains(errOut, "unknown option --scope") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestTrackerRunInARealProgram(t *testing.T) {
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := env{view: view{out: io.Discard, width: 80, tint: &tintCache{}}, w: w, client: trackerSource(), errOut: io.Discard}
	m := newTrackerModel(ctx, e)
	m.pause = 0
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24))
	go func() {
		for _, k := range []string{"down", "/", "b", "enter", "tab", "down", "esc", "q"} {
			time.Sleep(20 * time.Millisecond)
			p.Send(keyPress(k))
		}
	}()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if !m.loaded {
		t.Fatal("the list never arrived")
	}
}

func TestTrackerShowFailedReadsInWords(t *testing.T) {
	f := trackerSource()
	f.detailErr = errors.New("provider returned HTTP 403")
	d := driveTracker(t, f, 80, 40)
	d.wants("Cannot read this ticket", "HTTP 403", "doing")

	f = trackerSource()
	f.policyErr = errors.New("provider returned HTTP 500")
	d = driveTracker(t, f, 80, 40)
	d.wants("Cannot check the policy", "HTTP 500", "Body of APP-1")
}

func TestTrackerListFailureEndsTheScreenWithTheReason(t *testing.T) {
	f := trackerSource()
	f.listingErr = errors.New("cannot reach provider")
	d := driveTracker(t, f, 80, 24)
	if !d.quit || d.m.fatal == nil {
		t.Fatalf("quit %v, fatal %v", d.quit, d.m.fatal)
	}
}

func TestTrackerTextCannotInjectEscapes(t *testing.T) {
	f := trackerSource()
	f.listing[0].Title = "Title \x1b]52;c;ZXZpbA==\x07 end"
	f.listing[0].Assignee = "Alice\x1b[2J"
	evil := "\x1b]0;owned\x07"
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{
		{Kind: workfile.BlockParagraph, Spans: []workfile.Span{{Text: "body" + evil, Link: "https://example.com/" + evil}}},
		{Kind: workfile.BlockImage, Name: "file" + evil + ".png"},
		{Kind: workfile.BlockTable, Rows: [][]string{{"cell" + evil}}},
	}, CommentTotal: 1, Comments: []workfile.Comment{{Author: "Bob" + evil, Body: para("reply" + evil)}}}
	d := driveTracker(t, f, 100, 60)
	if strings.Contains(d.screen(), "\x1b") {
		t.Fatalf("an escape reached the screen: %q", d.screen())
	}
	d.wants("body", "reply")
}

func TestTrackerPlainOutputHasTheSameFacts(t *testing.T) {
	code, out, errOut := invoke(t, trackerSource(), "tracker")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, text := range []string{"APP-1", "Fix redirect loop", "In Review", "Alice", "https://example.atlassian.net/browse/APP-1", "Unassigned"} {
		if !strings.Contains(out, text) {
			t.Fatalf("missing %q:\n%s", text, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("escape in plain output: %q", out)
	}
	f := trackerSource()
	f.listing = nil
	if _, out, _ = invoke(t, f, "tracker"); !strings.Contains(out, "No tickets in your scope") {
		t.Fatal(out)
	}
	f.listingErr = errors.New("cannot reach provider")
	if code, _, errOut = invoke(t, f, "tracker"); code != 2 || !strings.Contains(errOut, "cannot reach provider") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestTrackerTakesNoArguments(t *testing.T) {
	for _, args := range [][]string{{"tracker", "APP-1"}, {"tracker", "--json"}, {"tracker", "--me"}, {"tracker", "--limit", "5"}, {"tracker", "--hide-done"}, {"tracker", "--scope", "x"}, {"status", "--scope", "x"}, {"status", "--view", "done"}} {
		if _, err := parse(args); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestAgoIsFriendly(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		created time.Time
		want    string
	}{
		{now.Add(-4 * time.Hour), "today"},
		{now.Add(-28 * time.Hour), "yesterday"},
		{now.Add(-76 * time.Hour), "3 days ago"},
		{time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC), "2 Jan 2026"},
		{time.Time{}, ""},
	} {
		if got := ago(c.created, now); got != c.want {
			t.Errorf("%v: got %q, want %q", c.created, got, c.want)
		}
	}
}
