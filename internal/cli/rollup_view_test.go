package cli

import (
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func healthFixture(t *testing.T, color bool, bg string) *healthModel {
	t.Helper()
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	var all []workfile.Assessment
	for _, tk := range boardFixture().tickets {
		all = append(all, w.Assess(tk))
	}
	h := healthScreen{screen: screen{v: view{out: io.Discard, width: 80, color: color}, t: newTheme(color, bg)}, w: w, r: w.Rollup(all), all: all}
	return newHealthModel(h)
}

func send(m *healthModel, msgs ...tea.Msg) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

func press(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func checkWidths(t *testing.T, m *healthModel, width int) {
	t.Helper()
	out := m.render()
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line too wide at %d columns (%d): %q\n%s", width, w, line, ansi.Strip(out))
		}
	}
}

func TestHealthScreenFitsAndMoves(t *testing.T) {
	for _, color := range []bool{true, false} {
		for _, width := range []int{40, 60, 100} {
			m := healthFixture(t, color, "48;5;236")
			send(m, tea.WindowSizeMsg{Width: width, Height: 30})
			checkWidths(t, m, width)
			keys := []tea.Msg{press(tea.KeyEnter), press(tea.KeyDown), press(tea.KeyEnter), press(tea.KeyEnter), press(tea.KeyDown), press(tea.KeyEscape), press(tea.KeyEscape)}
			for _, k := range keys {
				send(m, k)
				checkWidths(t, m, width)
			}
			for range len(m.items) {
				send(m, press(tea.KeyDown), press(tea.KeyEnter), press(tea.KeyEnter))
				checkWidths(t, m, width)
				send(m, press(tea.KeyEscape), press(tea.KeyEscape))
			}
		}
	}
}

func TestHealthScreenShowsRulesThenTickets(t *testing.T) {
	m := healthFixture(t, false, "")
	send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	out := m.render()
	for _, want := range []string{"1 of 4 tickets are out of policy (25%).", "Gates", "Stages", "Refinement", "todo → doing", "Description: at least 100 characters", "↑↓ choose", "enter tickets"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "APP-70") {
		t.Fatalf("tickets are listed only after enter\n%s", out)
	}
	send(m, press(tea.KeyEnter))
	out = m.render()
	if !strings.Contains(out, "APP-70") || !strings.Contains(out, "esc back") {
		t.Fatalf("enter lists the failing tickets under their rule\n%s", out)
	}
	send(m, press(tea.KeyEscape), press(tea.KeyDown))
	if out = m.render(); !strings.Contains(out, "Pull request") || strings.Contains(out, "APP-70") {
		t.Fatalf("esc goes back and down picks the next gate\n%s", out)
	}
	send(m, press('q'))
	if _, cmd := m.Update(press('q')); cmd == nil {
		t.Fatal("q quits")
	}
}

func TestHealthScreenNarrowOpensOnePaneAtATime(t *testing.T) {
	m := healthFixture(t, false, "")
	send(m, tea.WindowSizeMsg{Width: 40, Height: 30})
	if out := m.render(); strings.Contains(out, "Rules") || !strings.Contains(out, "› ") {
		t.Fatalf("a narrow screen starts with the list, the choice marked without a background\n%s", out)
	}
	send(m, press(tea.KeyEnter))
	if out := m.render(); !strings.Contains(out, "Rules") || !strings.Contains(out, "esc back") {
		t.Fatalf("enter opens the panel\n%s", out)
	}
	send(m, press(tea.KeyEscape))
	if out := m.render(); strings.Contains(out, "Rules") {
		t.Fatalf("esc returns to the list\n%s", out)
	}
}

func TestHealthPrintedIsPlainAndListsRules(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	_, out, _ := invoke(t, boardFixture(), "status")
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output has escapes: %q", out)
	}
	for _, want := range []string{"1 of 4 tickets are out of policy (25%).", "Gates", "failing of tickets that must pass", "Description: at least 100 characters", "Stages", "Next  wf status --gate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
	for _, icon := range []string{"✗ Gates", "✓ Gates", "✗ Stages", "SUMMARY"} {
		if strings.Contains(out, icon) {
			t.Fatalf("panel titles carry no icon (%q)\n%s", icon, out)
		}
	}
}
