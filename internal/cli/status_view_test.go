package cli

import (
	"io"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func statusFixture(t *testing.T, color bool, bg string, who map[string]string) *statusModel {
	t.Helper()
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	var all []workfile.Assessment
	for _, tk := range boardFixture().tickets {
		all = append(all, w.Assess(tk))
	}
	s := statusScreen{screen: screen{v: view{out: io.Discard, width: 80, color: color}, t: newTheme(color, bg)}, w: w, all: all}
	return newStatusModel(s, who)
}

func sendStatus(m *statusModel, msgs ...tea.Msg) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

func checkStatusWidths(t *testing.T, m *statusModel, width int) {
	t.Helper()
	out := m.render()
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line too wide at %d columns (%d): %q\n%s", width, w, line, ansi.Strip(out))
		}
	}
}

func TestStatusScreenFitsAndMoves(t *testing.T) {
	bob := map[string]string{"jira": "example-bob-account-id", "github": "example-bob"}
	for _, color := range []bool{true, false} {
		for _, width := range []int{40, 60, 100} {
			m := statusFixture(t, color, "48;5;236", bob)
			sendStatus(m, tea.WindowSizeMsg{Width: width, Height: 30})
			checkStatusWidths(t, m, width)
			keys := []tea.Msg{press(tea.KeyDown), press(tea.KeyEnter), press(tea.KeyDown), press(tea.KeyPgDown), press(tea.KeyEscape), press('m'), press(tea.KeyDown), press('m'), press(tea.KeyUp)}
			for _, k := range keys {
				sendStatus(m, k)
				checkStatusWidths(t, m, width)
			}
		}
	}
}

func TestStatusScreenListsByWhatToDoWithTheTicketBeside(t *testing.T) {
	m := statusFixture(t, false, "", nil)
	sendStatus(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.render()
	last := -1
	for _, want := range []string{"Ready to move", "APP-61", "Link at least 1 PR", "APP-57", "Out of policy", "APP-70"} {
		at := strings.Index(out, want)
		if at < 0 || at < last {
			t.Fatalf("%q missing or out of order\n%s", want, out)
		}
		last = at
	}
	if !strings.Contains(out, "WHERE IT IS") || !strings.Contains(out, "› ✓ APP-61") {
		t.Fatalf("the first ticket is chosen and shown beside the list\n%s", out)
	}
	if strings.Contains(out, "only mine") {
		t.Fatalf("without an identity there is no m key\n%s", out)
	}
	sendStatus(m, press(tea.KeyDown))
	if out = m.render(); !strings.Contains(out, "› ✗ APP-57") {
		t.Fatalf("down chooses the next ticket\n%s", out)
	}
	if _, cmd := m.Update(press('q')); cmd == nil {
		t.Fatal("q quits")
	}
}

func TestStatusScreenSwitchesToYourWork(t *testing.T) {
	bob := map[string]string{"jira": "example-bob-account-id", "github": "example-bob"}
	m := statusFixture(t, false, "", bob)
	sendStatus(m, tea.WindowSizeMsg{Width: 120, Height: 40}, press('m'))
	out := m.render()
	if !strings.Contains(out, "only yours") || !strings.Contains(out, "APP-57") || strings.Contains(out, "APP-61") || !strings.Contains(out, "m everyone") {
		t.Fatalf("m shows only Bob's work\n%s", out)
	}
	sendStatus(m, press('m'))
	if out = m.render(); !strings.Contains(out, "APP-61") {
		t.Fatalf("m again shows everyone's work\n%s", out)
	}
}

func TestStatusScreenNarrowOpensOnePaneAtATime(t *testing.T) {
	m := statusFixture(t, false, "", nil)
	sendStatus(m, tea.WindowSizeMsg{Width: 50, Height: 30})
	if out := m.render(); strings.Contains(out, "WHERE IT IS") {
		t.Fatalf("a narrow screen starts with the list\n%s", out)
	}
	sendStatus(m, press(tea.KeyEnter))
	if out := m.render(); !strings.Contains(out, "WHERE IT IS") || !strings.Contains(out, "esc back") {
		t.Fatalf("enter opens the ticket\n%s", out)
	}
	sendStatus(m, press(tea.KeyEscape))
	if out := m.render(); strings.Contains(out, "WHERE IT IS") {
		t.Fatalf("esc returns to the list\n%s", out)
	}
}

func TestStatusPrintedIsPlainAndFits(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 100} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, boardFixture(), "status")
		if strings.Contains(out, "\x1b") {
			t.Fatalf("plain output has escapes: %q", out)
		}
		if width == 100 && !strings.Contains(out, "https://example-team.atlassian.net/browse/APP-57") {
			t.Fatalf("plain output writes out the web addresses\n%s", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("line too wide at %d columns: %q", width, line)
			}
		}
	}
}
