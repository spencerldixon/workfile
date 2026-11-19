package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"workfile/internal/workfile"
)

func previewHeading(text string, level int) workfile.Block {
	return workfile.Block{Kind: workfile.BlockHeading, Level: level, Spans: []workfile.Span{{Text: text}}}
}

func TestTicketHeadingImagesRespectDocumentLevels(t *testing.T) {
	previous := 1000
	for level := 1; level <= 6; level++ {
		img, cols, rows, ok := headingImage("Various headings", level, testGraphics, 100)
		if !ok || cols >= previous || cols > 100 {
			t.Fatalf("H%d did not get a distinct, bounded size: %d columns", level, cols)
		}
		previous = cols
		expectedRows := 1
		if level <= 3 {
			expectedRows = 2
		}
		if rows != expectedRows || img.Bounds().Dy() != rows*testGraphics.cellH {
			t.Fatalf("H%d uses wrong height: %d rows", level, rows)
		}
	}
	g := testGraphics
	g.hasInk = false
	if _, _, _, ok := headingImage("Heading", 1, g, 100); ok {
		t.Fatal("heading invented an ink colour")
	}
	if _, _, _, ok := headingImage("ログイン", 1, testGraphics, 100); ok {
		t.Fatal("unsupported glyphs should use text")
	}
}

func TestTicketHeadingRequestsMatchNestedAndQuotedWidths(t *testing.T) {
	p := ticketPreview{width: 60, showComments: true}
	quoted := previewHeading("Quoted heading", 2)
	quoted.Quoted = true
	detail := workfile.Detail{Body: []workfile.Block{previewHeading("Body heading", 1), previewHeading("Body heading", 1), quoted}, Comments: []workfile.Comment{{Replies: []workfile.Comment{{Body: []workfile.Block{previewHeading("Reply heading", 3)}}}}}}
	requests := p.headingRequests(detail)
	if len(requests) != 3 || requests[0].width != 56 || requests[1].width != 54 || requests[2].width != 49 {
		t.Fatalf("requests do not match preview layout: %+v", requests)
	}
	detail.Body = nil
	for i := 0; i < maxPreviewHeadings+10; i++ {
		detail.Body = append(detail.Body, previewHeading(fmt.Sprintf("Heading %d", i), 1))
	}
	if len(p.headingRequests(detail)) != maxPreviewHeadings {
		t.Fatal("image work was not bounded")
	}
}

func TestTrackerTicketHeadingsShareGraphicsAcrossBoardAndList(t *testing.T) {
	for _, board := range []bool{false, true} {
		f := boardSource()
		f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{previewHeading("Problem", 1), previewHeading("Approach", 2), previewHeading("Validation", 3)}}
		settings := workfile.TrackerSettings{}
		if board {
			settings = boardSettings()
		}
		d := driveWithSettings(t, f, 100, 80, testGraphics, options{}, settings)
		if board {
			if len(d.m.headings) != 0 {
				t.Fatal("board generated hidden document images")
			}
			d.press("enter")
		}
		if len(d.m.headings) != 3 || d.m.titles["APP-1"] == nil {
			t.Fatalf("title or heading graphics missing for board=%v: %d headings", board, len(d.m.headings))
		}
		if !strings.ContainsRune(d.pane(), kitty.Placeholder) || strings.Contains(d.pane(), "Approach") {
			t.Fatal("images did not replace heading text")
		}
		sent := len(d.raw)
		d.send(tea.WindowSizeMsg{Width: 100, Height: 80})
		if len(d.raw) != sent {
			t.Fatal("same-size headings were sent more than once")
		}
		d.width = 60
		d.send(tea.WindowSizeMsg{Width: 60, Height: 80})
		if len(d.m.headings) != 6 {
			t.Fatal("heading images did not follow the new text width")
		}
		d.width = 21
		d.send(tea.WindowSizeMsg{Width: 21, Height: 80})
		if !strings.Contains(d.pane(), "Approach") {
			t.Fatal("narrow preview lost its text fallback")
		}
		if len(f.imageReads) != 0 {
			t.Fatal("local heading rasterisation made provider image requests")
		}
	}
}

func TestTrackerTicketHeadingFallbackKeepsText(t *testing.T) {
	for _, g := range []graphics{{}, {on: true, cellW: 8, cellH: 16}} {
		f := trackerSource()
		f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{previewHeading("Readable heading", 2)}}
		d := driveWith(t, f, 60, 80, g, options{})
		if len(d.m.headings) != 0 || !strings.Contains(d.pane(), "Readable heading") {
			t.Fatal("graphics fallback lost heading text")
		}
		if strings.ContainsRune(d.pane(), kitty.Placeholder) {
			t.Fatal("fallback drew image placeholders")
		}
	}
}

func TestTrackerTicketHeadingsRecycleAndFreeImages(t *testing.T) {
	f := trackerSource()
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: []workfile.Block{previewHeading("First heading", 1)}}
	d := graphicTickets(t, f, 100, 80)
	var key ticketHeading
	var oldID int
	for heading, picture := range d.m.headings {
		key, oldID = heading, picture.id
	}
	d.m.nextID = oldID
	d.m.allocate()
	if d.m.headings[key] != nil || d.m.headingTried[key] {
		t.Fatal("recycled image left a stale heading")
	}
	d.run(d.m.loadPreviewHeadings())
	if d.m.headings[key] == nil {
		t.Fatal("evicted heading could not be drawn again")
	}
	if got, want := strings.Count(d.m.forgetAll(), "a=d"), len(d.m.images)+len(d.m.titles)+len(d.m.headings); got != want {
		t.Fatalf("forgot %d of %d terminal images", got, want)
	}
	for _, line := range strings.Split(d.pane(), "\n") {
		if ansi.StringWidth(line) > 100 {
			t.Fatal("heading overflowed the preview")
		}
	}
}
