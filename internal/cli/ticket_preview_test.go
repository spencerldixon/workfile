package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func previewDetail() workfile.Detail {
	return workfile.Detail{Key: "APP-1", Body: para("Description text"), CommentTotal: 3, Comments: []workfile.Comment{{
		Author: "Alice", Body: para("Root comment"), Replies: []workfile.Comment{{Author: "Bob", Body: para("Reply comment"), Replies: []workfile.Comment{{Author: "Charlie", Body: para("Nested comment")}}}},
	}}}
}

func TestTicketPreviewCommentPanelsFollowNesting(t *testing.T) {
	for _, width := range []int{40, 60} {
		const bg = "\x1b[48;5;236m"
		p := ticketPreview{t: newTheme(true, ""), panelTheme: newTheme(true, "48;5;236"), width: width, showComments: true}
		detail := previewDetail()
		detail.Comments[0].Replies[0].Body = para("Reply comment with a long line that wraps within the thread")
		rendered := p.sections(p.ticketBody(&ticketRead{detail: &detail}), width)
		plain := ansi.Strip(rendered)
		for _, want := range []string{
			"    Alice", "    Root comment",
			"       󰑀 Bob", "         Reply comment",
			"          󰑀 Charlie", "            Nested comment",
		} {
			if !strings.Contains(plain, "\n"+want) {
				t.Fatalf("missing %q at width %d:\n%s", want, width, plain)
			}
		}
		for _, name := range []string{"Alice", "Bob", "Charlie"} {
			for _, line := range strings.Split(rendered, "\n") {
				if strings.Contains(line, name) && (!strings.Contains(line, bg) || ansi.StringWidth(line) != width) {
					t.Fatalf("missing full-width background for %s: %q", name, line)
				}
			}
		}
		lines := strings.Split(rendered, "\n")
		for i, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("%d-column preview overflowed: %q", width, line)
			}
			if strings.Contains(line, "Alice") && (!strings.Contains(lines[i-1], bg) || strings.TrimSpace(ansi.Strip(lines[i-1])) != "") {
				t.Fatalf("missing shaded padding above comment at width %d", width)
			}
			if strings.Contains(line, "Root comment") && (!strings.Contains(lines[i+1], bg) || strings.TrimSpace(ansi.Strip(lines[i+1])) != "") {
				t.Fatalf("missing shaded padding below comment at width %d", width)
			}
		}
	}
}

func TestTicketPreviewPlainAndColour(t *testing.T) {
	for _, color := range []bool{false, true} {
		for _, width := range []int{40, 60} {
			p := ticketPreview{t: newTheme(color, ""), width: width, showComments: true, now: time.Now()}
			detail := previewDetail()
			rendered := p.sections(p.ticketBody(&ticketRead{detail: &detail}), width)
			plain := ansi.Strip(rendered)
			icon := "↳"
			if color {
				icon = "󰑀"
			}
			for _, want := range []string{"Description text", "Alice", "Root comment", icon + " Bob", "Reply comment", icon + " Charlie", "Nested comment"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("fallback lost %q", want)
				}
			}
			if !color && strings.Contains(rendered, "\x1b") {
				t.Fatal("NO_COLOR preview contained escapes")
			}
			if strings.Contains(rendered, "48;") {
				t.Fatal("invented a background without a terminal reply")
			}
		}
	}
}

func TestTicketPreviewSortsOnlyItsCopyOfFlatComments(t *testing.T) {
	now := time.Now()
	detail := workfile.Detail{Comments: []workfile.Comment{
		{Author: "Unknown date"},
		{Author: "Oldest", Created: now.Add(-time.Hour)},
		{Author: "Newest first", Created: now},
		{Author: "Newest second", Created: now},
	}}
	p := ticketPreview{width: 60, now: now}
	rendered := p.sections(p.comments(detail), 60)
	last := -1
	for _, author := range []string{"Newest first", "Newest second", "Oldest", "Unknown date"} {
		if index := strings.Index(rendered, author); index > last {
			last = index
		} else {
			t.Fatalf("%q out of order or absent:\n%s", author, rendered)
		}
	}
	if detail.Comments[0].Author != "Unknown date" {
		t.Fatal("rendering changed the source comment order")
	}
}

func TestTrackerBoardAndListUseTheSameTicketPreview(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 140} {
		var texts []string
		for _, board := range []bool{false, true} {
			f := boardSource()
			f.details["APP-1"] = previewDetail()
			settings := workfile.TrackerSettings{}
			if board {
				settings = boardSettings()
			}
			d := driveWithSettings(t, f, width, 60, graphics{}, options{}, settings)
			if board {
				d.press("enter")
			}
			before := len(f.detailReads) + len(f.policyReads)
			texts = append(texts, d.pane())
			d.send(tea.WindowSizeMsg{Width: width, Height: 60})
			d.check()
			if len(f.detailReads)+len(f.policyReads) != before {
				t.Fatal("drawing the shared preview fetched data")
			}
		}
		if texts[0] != texts[1] {
			t.Fatalf("board and list ticket views diverged at %d columns", width)
		}
	}
}

func TestTrackerPanelBackgroundQueriesTerminalOnce(t *testing.T) {
	t.Setenv("WORKFILE_BACKGROUND", "")
	original := terminalBackground
	calls := 0
	terminalBackground = func() (uint8, uint8, uint8, bool) { calls++; return 18, 20, 22, true }
	defer func() { terminalBackground = original }()
	d := driveWithSettings(t, boardSource(), 100, 60, graphics{}, options{}, boardSettings())
	e := d.m.e
	e.view.color = true
	e.view.tint = &tintCache{}
	model := newTrackerModel(context.Background(), e)
	if model.panelTheme.bg == "" {
		t.Fatal("panel background did not come from the terminal")
	}
	model.previewRenderer()
	model.previewRenderer()
	e.tracker = workfile.TrackerSettings{}
	newTrackerModel(context.Background(), e)
	if calls != 1 {
		t.Fatalf("asked terminal %d times", calls)
	}
}

func TestTicketPreviewHiddenCommentsHaveNoHeadingRequests(t *testing.T) {
	p := ticketPreview{t: newTheme(false, ""), width: 60, showComments: false}
	detail := previewDetail()
	detail.Comments[0].Body = []workfile.Block{{Kind: workfile.BlockHeading, Level: 1, Spans: []workfile.Span{{Text: "Hidden heading"}}}}
	if requests := p.headingRequests(detail); len(requests) != 0 {
		t.Fatal("hidden comments requested image work")
	}
	rendered := p.sections(p.ticketBody(&ticketRead{detail: &detail}), 60)
	if strings.Contains(rendered, "Alice") || strings.Contains(rendered, "Comments") {
		t.Fatal("hidden comments were drawn")
	}
}
