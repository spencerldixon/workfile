package cli

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func readerFixture(f *fakeSource) *fakeSource {
	heading := func(text string) workfile.Block {
		return workfile.Block{Kind: workfile.BlockHeading, Level: 2, Spans: []workfile.Span{{Text: text}}}
	}
	body := []workfile.Block{heading("Summary")}
	body = append(body, para(strings.Repeat("A long first section. ", 100))...)
	body = append(body, heading("Technical"))
	body = append(body, para("Read all region names together.")...)
	body = append(body, para(strings.Repeat("More implementation detail. ", 100))...)
	f.details["APP-1"] = workfile.Detail{
		Key: "APP-1", Body: body,
		Comments: []workfile.Comment{{Author: "Bob", Body: para("Keep the ordering the same.")}},
	}
	return f
}

func TestTrackerReaderFitsPlainAndScrollsToComments(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		d := driveTracker(t, readerFixture(trackerSource()), width, 26)
		d.press("enter")
		d.refuses("Ticket · reading", "tab to list")
		d.wants("Story", "To Do", "Alice", "w workfile")
		d.press("down", "down")
		d.refuses("Ticket preview", "APP-2", "\x1b", "0%", "On this ticket")
		offset := d.m.pane.YOffset()
		if offset == 0 {
			t.Fatal("the ticket did not scroll")
		}
		d.press("w")
		d.refuses("Workfile · reading")
		d.wants("Next action")
		d.press("w")
		if d.m.pane.YOffset() != offset {
			t.Fatal("returning from Workfile lost the ticket position")
		}
		for !d.m.pane.AtBottom() {
			before := d.m.pane.YOffset()
			d.press("f")
			if d.m.pane.YOffset() == before {
				t.Fatal("page down did not move toward the comments")
			}
		}
		d.wants("────", "Comments (1)", "Keep the ordering the same.")
		d.refuses("\x1b")
		d.check()
		d.press("esc")
		d.wants("APP-2", "↑↓ move")
	}
}

func TestTrackerBoardReaderPinsMetadataAndKeepsWorkfileInView(t *testing.T) {
	d := driveWithSettings(t, readerFixture(boardSource()), 140, 30, graphics{}, options{}, boardSettings())
	d.m.t = newTheme(true, "")
	d.m.v.color, d.m.v.links = true, true
	d.press("enter")
	d.wants("\uf02d Story", "To Do", "\uf007 Alice", "Next action")
	header := strings.Join(d.m.readerHeader, "\n")
	first, _, _ := strings.Cut(d.screen(), "\n")
	if !strings.HasPrefix(ansi.Strip(first), ticketIndent+"Board") ||
		!strings.Contains(ansi.Strip(header), "\n"+ticketIndent+d.m.current) {
		t.Fatal("first view and ticket key should share the same inset")
	}
	d.press("down", "down", "down")
	if header != strings.Join(d.m.readerHeader, "\n") || d.m.side.YOffset() != 0 {
		t.Fatal("scrolling the ticket moved the identity or Workfile")
	}
	d.wants("Next action")
	d.refuses("On this ticket", "g jump")
	d.check()
	d.press("esc")
	d.wants("Board", "▌")
}

func TestTrackerReaderStatusUsesListColoursInRoundedTags(t *testing.T) {
	d := colourTickets(t, trackerSource(), 140, 30)
	d.m.overview = false
	d.m.tagPalette = ticketTagPalette{0: 0.01, 15: 0.99, 1: 0.08, 2: 0.44}
	d.press("enter")
	for _, tc := range []struct {
		icon   string
		colour lipgloss.ANSIColor
		ink    lipgloss.ANSIColor
		checks []workfile.Check
	}{
		{"󰄬", 2, 0, nil},
		{"󰅖", 1, 15, []workfile.Check{{Outcome: "fail"}}},
	} {
		read := d.m.ticketReads.byKey[d.m.current]
		read.policy = &workfile.Assessment{Routes: []workfile.RouteAssessment{{To: "doing", Checks: tc.checks}}}
		d.m.fill(false)
		header := strings.Join(d.m.readerHeader, "\n")
		status := d.m.t.roundedTag(tc.icon+" To Do", tc.colour, tc.ink)
		if !strings.Contains(header, status) {
			t.Fatalf("expected rounded tags with the matching status colour and contrasting text: %q", header)
		}
	}
}

func TestTrackerListPreviewAndReaderShareTheTicketHeading(t *testing.T) {
	d := colourTickets(t, trackerSource(), 140, 30)
	d.wants("\uf02d Story", "\uf007 Alice")
	preview, _, _ := d.m.ticketContent()
	d.press("enter")
	reader, _, _ := d.m.ticketContent()
	if !slices.Equal(preview.lines, reader.lines) {
		t.Fatalf("preview and reader rendered different ticket headings:\n%q\n%q", preview.lines, reader.lines)
	}
}
