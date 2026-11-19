package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"workfile/internal/workfile"
)

func inboxScreen(d *inboxDriver) string { return d.m.View().Content }

func TestInboxViewWidths(t *testing.T) {
	for _, color := range []bool{false, true} {
		for _, width := range []int{40, 60, 120} {
			d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
			d.m.e.view.color = color
			d.send(tea.WindowSizeMsg{Width: width, Height: 30})
			for _, screen := range []string{inboxScreen(d), func() string {
				d.press("enter")
				return inboxScreen(d)
			}(), func() string { d.press("esc", "e"); return inboxScreen(d) }()} {
				for i, line := range strings.Split(screen, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Fatalf("color=%v width %d: line %d is %d wide: %q", color, width, i, w, line)
					}
				}
			}
		}
	}
}

func TestInboxViewPlain(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	screen := inboxScreen(d)
	if strings.Contains(screen, "\x1b") {
		t.Fatalf("plain screen has escapes:\n%q", screen)
	}
	for _, want := range []string{"✗", "●", "○", "2 need you · 1 update", "Needs you", "Updates", "Alice requested changes on #84", "Alice commented on #90", "Charlie approved #95"} {
		if !strings.Contains(screen, want) {
			t.Errorf("plain screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestInboxViewSymbolsAndColours(t *testing.T) {
	th := newTheme(true, "")
	for kind, want := range map[workfile.RowKind]struct{ symbol, icon, sgr string }{
		workfile.RowChangesRequested: {"✗", "󰅖", "38;5;1m"},
		workfile.RowChecksFailed:     {"✗", "󰙨", "38;5;1m"},
		workfile.RowNeedsWork:        {"✗", "󰏫", "38;5;1m"},
		workfile.RowOutOfPolicy:      {"‼", "󰀦", "38;5;1m"},
		workfile.RowReviewRequested:  {"●", "", "38;5;3m"},
		workfile.RowComments:         {"●", "󰆉", "38;5;3m"},
		workfile.RowReady:            {"✓", "󰄬", "38;5;2m"},
		workfile.RowCannotCheck:      {"!", "󰋼", "38;5;3m"},
		workfile.RowInfo:             {"○", "󰋽", "2"},
	} {
		if got := inboxSymbol(kind); got != want.symbol {
			t.Errorf("%s symbol %q, want %q", kind, got, want.symbol)
		}
		painted := paintSymbol(th, kind)
		if !strings.Contains(painted, want.icon) || !strings.Contains(painted, want.sgr) {
			t.Errorf("%s painted %q, want SGR %s", kind, painted, want.sgr)
		}
	}
	if painted := paintSymbol(th, workfile.RowOutOfPolicy); !strings.Contains(painted, "1;") && !strings.Contains(painted, ";1") && !strings.Contains(painted, "[1m") {
		t.Errorf("out of policy is bold: %q", painted)
	}
}

func TestInboxViewCleansProviderText(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	data := inboxFixtureData()
	data.work[0].Listing.Title = "Fix\x1b[31m\nlogin"
	data.events[0].Text = "line one\x1b]8;;evil\x07\nline two"
	d.send(inboxLoadedMsg(data))
	d.press("enter")
	screen := inboxScreen(d)
	if strings.Contains(screen, "\x1b[31m") || strings.Contains(screen, "\x1b]8;;evil") || strings.Contains(screen, "\x07") {
		t.Fatalf("provider text injected escapes:\n%q", screen)
	}
}

func TestInboxZeroStaticFrame(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.press("e")
	if _, cmd := d.m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"}); cmd != nil {
		t.Fatal("plain output should not schedule an animation")
	}
	screen := inboxScreen(d)
	for _, want := range []string{"✓", "Nothing needs you.", "Charlie approved #95"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("inbox zero lacks %q:\n%s", want, screen)
		}
	}
}

func TestInboxFooterPerRow(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
	if screen := inboxScreen(d); !strings.Contains(screen, "a agent") || !strings.Contains(screen, "o open #84") {
		t.Fatalf("action row footer:\n%s", screen)
	}
	d.selectID("APP-4")
	if screen := inboxScreen(d); !strings.Contains(screen, "e done") {
		t.Fatalf("comments footer:\n%s", screen)
	}
	d.selectID("APP-5")
	if screen := inboxScreen(d); strings.Contains(screen, "a agent") {
		t.Fatalf("information footer offers an agent:\n%s", screen)
	}
}

func TestInboxShowsQueueThenDetail(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	for _, width := range []int{40, 60, 120} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 30})
		if screen := inboxScreen(d); strings.Contains(screen, "Next step") || !strings.Contains(screen, "enter details") {
			t.Fatalf("width %d: inbox opens as a queue:\n%s", width, screen)
		}
		d.press("enter")
		if screen := inboxScreen(d); !strings.Contains(screen, "Next step") || !strings.Contains(screen, "esc list") {
			t.Fatalf("width %d: enter opens the preview:\n%s", width, screen)
		}
		d.press("esc")
		if strings.Contains(inboxScreen(d), "Next step") {
			t.Fatalf("width %d: esc returns to the queue", width)
		}
	}
}

func TestInboxZeroAnimatesThenStops(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.m.e.view.color = true
	d.m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	_, cmd := d.m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if cmd == nil {
		t.Fatal("reaching zero starts the moment")
	}
	if !strings.Contains(ansi.Strip(inboxScreen(d)), "· Nothing needs you.") {
		t.Fatalf("first frame:\n%s", inboxScreen(d))
	}
	_, cmd = d.m.Update(zeroTickMsg{d.m.zeroRun - 1})
	if cmd != nil || d.m.zeroFrame != 0 {
		t.Fatal("a tick from an earlier run advanced the animation")
	}
	var middle bool
	for range zeroLast {
		_, cmd = d.m.Update(zeroTickMsg{d.m.zeroRun})
		middle = middle || strings.Contains(ansi.Strip(inboxScreen(d)), "● Nothing needs you.")
	}
	if !middle || cmd != nil || !strings.Contains(ansi.Strip(inboxScreen(d)), "✓ Nothing needs you.") {
		t.Fatal("the moment ends on the tick")
	}
	d2 := driveInbox(t, workfile.InboxSettings{})
	d2.m.e.view.color = true
	d2.press("e", "e", "j")
	_, cmd = d2.m.Update(zeroTickMsg{d2.m.zeroRun})
	if cmd != nil || !strings.Contains(ansi.Strip(inboxScreen(d2)), "✓ Nothing needs you.") {
		t.Fatal("any key skips to the last frame")
	}
}

func TestInboxSyncedJustNow(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	if screen := inboxScreen(d); !strings.Contains(screen, "synced just now") || strings.Contains(screen, "now ago") {
		t.Fatalf("header wording:\n%s", screen)
	}
}

func TestInboxNarrowFooterKeepsActions(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
	d.send(tea.WindowSizeMsg{Width: 50, Height: 30})
	lines := strings.Split(inboxScreen(d), "\n")
	footerLines := len(d.m.footer(d.m.theme(), 50))
	footer := strings.Join(lines[len(lines)-footerLines:], "\n")
	for _, want := range []string{"enter details", "t ticket", "e done", "a agent"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("narrow footer lacks %q: %q", want, footer)
		}
	}
}

func TestInboxOpensTicketAndReturnsToAlert(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	m := d.m
	d.press("j", "enter")
	row := m.selected()
	pane := exampleWorkfilePane(60)
	work := &m.data.work[1]
	pane.assessment.Ticket.Key = row.Ticket
	work.Assessment, work.Next = pane.assessment, pane.actions
	f := m.e.client.(*fakeSource)
	f.details = map[string]workfile.Detail{row.Ticket: {Key: row.Ticket}}
	saves := d.saves
	_, load := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.ticket == nil || load == nil {
		t.Fatal("the ticket shortcut did not open the selected ticket")
	}
	d.send(load())
	if len(f.detailReads) != 1 || f.detailReads[0] != row.Ticket || len(f.keys) != 0 {
		t.Fatalf("expected only a detail read for %s, got detail %v and policy %v", row.Ticket, f.detailReads, f.keys)
	}
	for _, width := range []int{40, 60} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 30})
		screen := inboxScreen(d)
		for _, want := range []string{row.Ticket, row.PR, "Where it is", "esc inbox"} {
			if !strings.Contains(screen, want) {
				t.Fatalf("missing %q at %d columns:\n%s", want, width, screen)
			}
		}
		if strings.ContainsRune(screen, '\x1b') {
			t.Fatal("plain ticket view contains terminal escapes")
		}
		for _, line := range strings.Split(screen, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("line overflows at %d columns: %q", width, line)
			}
		}
	}
	d.press("pgdown")
	if m.ticket.body.YOffset() == 0 {
		t.Fatal("Page Down should scroll the ticket")
	}
	d.press("e", "esc")
	if m.ticket != nil || m.selected().ID != row.ID || !m.detail || d.saves != saves {
		t.Fatal("closing the ticket should restore the alert preview without clearing it")
	}
	d.send(load()) // A reply that arrived after closing must leave the inbox visible.
	if m.ticket != nil {
		t.Fatal("a late reply reopened the ticket")
	}
}

func TestInboxTicketUsesTheSharedKittyDiagram(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	m := d.m
	m.e.view.color, m.workflow.gfx = true, testGraphics
	d.send(tea.WindowSizeMsg{Width: 60, Height: 48})
	d.press("j", "enter")
	r := m.selected()
	datum := &m.data.work[1]
	assessment := m.e.w.Assess(fixtureTicket(r.Ticket, "doing", "short"))
	datum.Assessment = &assessment
	_, load := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	for _, cmd := range load().(tea.BatchMsg) {
		_, send := m.Update(cmd())
		if send != nil {
			if raw, ok := send().(tea.RawMsg); !ok || !strings.Contains(raw.Msg.(string), "\x1b_G") {
				t.Fatal("inbox did not transmit a workflow image")
			}
		}
	}
	if !strings.ContainsRune(inboxScreen(d), kitty.Placeholder) {
		t.Fatal("inbox did not place the workflow image")
	}
}

func TestInboxNextWordsAnAction(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.press("enter")
	screen := inboxScreen(d)
	action := strings.Index(screen, "1. Address Alice's review on #84")
	activity := strings.Index(screen, "Latest activity")
	if action < 0 || activity < action {
		t.Fatalf("next step is worded as an action:\n%s", screen)
	}
}

func TestInboxDetailScrollsToLatestActivity(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.send(tea.WindowSizeMsg{Width: 40, Height: 11})
	d.press("enter")
	if screen := inboxScreen(d); !strings.Contains(screen, "Fix login") || strings.Contains(screen, "Latest activity") {
		t.Fatalf("detail starts at the ticket on a short screen:\n%s", screen)
	}
	d.press("pgdown", "pgdown", "pgdown")
	if screen := inboxScreen(d); !strings.Contains(screen, "Latest activity") {
		t.Fatalf("page down reaches recent activity:\n%s", screen)
	}
	d.press("pgup")
	if screen := inboxScreen(d); strings.Contains(screen, "Latest activity") {
		t.Fatalf("page up scrolls back after reaching the end:\n%s", screen)
	}
}
