package cli

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi/kitty"
	"workfile/internal/workfile"
)

func inboxScreen(d *inboxDriver) string {
	m := d.m
	tracker := trackerModel{e: m.e, t: m.theme(), inbox: m, width: m.width, height: m.height, now: m.now}
	tracker.view = len(tracker.views()) - 1
	return tracker.render()
}

func TestInboxAgeSwitchesToDaysAt24Hours(t *testing.T) {
	for age, want := range map[time.Duration]string{
		23*time.Hour + 59*time.Minute: "23h",
		24 * time.Hour:                "1d",
		45 * time.Hour:                "1d",
		48 * time.Hour:                "2d",
	} {
		if got := shortAge(loadNow.Add(-age), loadNow); got != want {
			t.Errorf("age %s displayed as %q, want %q", age, got, want)
		}
	}
}

func TestInboxViewWidths(t *testing.T) {
	for _, color := range []bool{false, true} {
		for _, width := range []int{40, 60, 120} {
			d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
			d.m.e.view.color = color
			d.send(tea.WindowSizeMsg{Width: width, Height: 30})
			for _, screen := range []string{inboxScreen(d), func() string {
				d.press("a")
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
	for _, want := range []string{"✗", "!", "·", "Address Alice's review on #84", "Read Alice's comment on #90", "Charlie approved #95"} {
		if !strings.Contains(screen, want) {
			t.Errorf("plain screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestInboxViewSymbolsAndColours(t *testing.T) {
	th := newTheme(true, "")
	for kind, want := range map[workfile.RowKind]struct{ symbol, icon, sgr string }{
		workfile.RowChangesRequested: {"✗", "󰅖", "38;5;1m"},
		workfile.RowChecksFailed:     {"✗", "󰅖", "38;5;1m"},
		workfile.RowNeedsWork:        {"✗", "󰅖", "38;5;1m"},
		workfile.RowOutOfPolicy:      {"‼", "󰀦", "38;5;1m"},
		workfile.RowReviewRequested:  {"…", "󰆉", "38;5;3m"},
		workfile.RowComments:         {"!", "󰍩", "38;5;3m"},
		workfile.RowReady:            {"✓", "󰄬", "38;5;2m"},
		workfile.RowCannotCheck:      {"!", "󰋼", "38;5;3m"},
		workfile.RowInfo:             {"·", "·", "2"},
	} {
		if got := inboxSymbol(kind); got != want.symbol {
			t.Errorf("%s symbol %q, want %q", kind, got, want.symbol)
		}
		painted := paintInboxSignal(th, kind)
		if !strings.Contains(painted, want.icon) || !strings.Contains(painted, want.sgr) {
			t.Errorf("%s painted %q, want SGR %s", kind, painted, want.sgr)
		}
	}
	if painted := paintInboxSignal(th, workfile.RowOutOfPolicy); !strings.Contains(painted, "1;") && !strings.Contains(painted, ";1") && !strings.Contains(painted, "[1m") {
		t.Errorf("out of policy is bold: %q", painted)
	}
}

func TestInboxViewCleansProviderText(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	data := inboxFixtureData()
	data.work[0].Listing.Title = "Fix\x1b[31m\nlogin"
	data.events[0].Text = "line one\x1b]8;;evil\x07\nline two"
	d.send(inboxLoadedMsg(data))
	d.selectID("APP-1")
	d.press("enter")
	screen := inboxScreen(d)
	if strings.Contains(screen, "\x1b[31m") || strings.Contains(screen, "\x1b]8;;evil") || strings.Contains(screen, "\x07") {
		t.Fatalf("provider text injected escapes:\n%q", screen)
	}
}

func TestInboxEmptyState(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.press("e", "e", "e")
	if screen := inboxScreen(d); !strings.Contains(screen, "No alerts.") {
		t.Fatalf("missing empty state:\n%s", screen)
	}
}

func TestInboxFooterPerRow(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
	d.selectID("APP-1")
	if screen := inboxScreen(d); !strings.Contains(screen, "a agent") || !strings.Contains(screen, "o open PR") {
		t.Fatalf("action row footer:\n%s", screen)
	}
	d.selectID("APP-4")
	if screen := inboxScreen(d); !strings.Contains(screen, "e resolve") {
		t.Fatalf("comments footer:\n%s", screen)
	}
	d.selectID("APP-5")
	if screen := inboxScreen(d); strings.Contains(screen, "a agent") {
		t.Fatalf("information footer offers an agent:\n%s", screen)
	}
}

func TestInboxShowsTitlesAndActionsWithoutExpanding(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	for _, width := range []int{40, 60, 120} {
		d.send(tea.WindowSizeMsg{Width: width, Height: 30})
		before := inboxScreen(d)
		if !strings.Contains(before, "Fix login") || !strings.Contains(before, "Cache") {
			t.Fatalf("width %d: titles should be visible before selection:\n%s", width, before)
		}
		d.selectID("APP-1")
		after := inboxScreen(d)
		if strings.Contains(after, "enter details") || strings.Count(before, "\n") != strings.Count(after, "\n") {
			t.Fatalf("width %d: moving should keep the same queue layout:\n%s", width, after)
		}
	}
}

func TestInboxNarrowFooterKeepsActions(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
	d.selectID("APP-1")
	d.send(tea.WindowSizeMsg{Width: 50, Height: 30})
	lines := strings.Split(inboxScreen(d), "\n")
	footerLines := len(d.m.footer(d.m.theme(), 50))
	footer := strings.Join(lines[len(lines)-footerLines:], "\n")
	for _, want := range []string{"s scope", "t open ticket", "e resolve", "a agent"} {
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
	load := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
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
		for _, want := range []string{row.Ticket, row.PR, "Review", "esc inbox"} {
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
	if m.ticket != nil || m.selected().ID != row.ID || d.saves != saves {
		t.Fatal("closing the ticket should restore the queue selection without clearing it")
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
	load := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	for _, cmd := range load().(tea.BatchMsg) {
		send := m.Update(cmd())
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

func TestInboxOpenPRUsesCommentThreadWhenAvailable(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	data := inboxFixtureData()
	thread := data.work[1].PRs["github"][0].URL + "#issuecomment-10"
	data.events[1].URL = thread
	d.m.st.Events = nil
	d.send(inboxLoadedMsg(data))
	d.selectID("APP-4")
	var opened string
	d.m.open = func(url string) error { opened = url; return nil }
	d.press("o")
	if opened != thread || d.m.selected().ID != "APP-4" {
		t.Fatalf("opening the thread should leave the row selected, got %q", opened)
	}
}

func TestInboxScopeReusesTrackerPicker(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{DefaultScope: workfile.InboxScopeAttention})
	for _, width := range []int{40, 60} {
		tracker := trackerModel{e: d.m.e, t: d.m.theme(), inbox: d.m, width: width, height: 24, now: d.m.now}
		tracker.view = len(tracker.views()) - 1
		d.m.scope = workfile.InboxScopeAttention
		d.m.rebuild()
		tracker.openInboxScope()
		if got := tracker.projectPicker.Index(); got != 1 {
			t.Fatalf("picker should select the current scope, got %d", got)
		}
		screen := tracker.render()
		for _, want := range []string{"Choose Inbox scope", "All signals", "Needs attention"} {
			if !strings.Contains(screen, want) {
				t.Fatalf("scope lacks %q:\n%s", want, screen)
			}
		}
		for _, line := range strings.Split(screen, "\n") {
			if lipgloss.Width(line) > width || strings.Contains(line, "\x1b") {
				t.Fatalf("unexpected plain scope line: %q", line)
			}
		}
		tracker.projectPicker.Select(0)
		tracker.projectKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if tracker.pickingPicker() || len(d.m.rows) != 3 {
			t.Fatalf("selecting all should reveal the update, got %v", d.ids())
		}
	}
}
