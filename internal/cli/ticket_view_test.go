package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func ticketScreen(t *testing.T, width int, tk workfile.Ticket) string {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", fmt.Sprint(width))
	f := &fakeSource{tickets: []workfile.Ticket{tk}}
	code, out, errOut := invoke(t, f, "status", tk.Key)
	if errOut != "" || code > 1 {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	return out
}

func TestTicketScreenShowsWhereItIsAndWhatNeedsAttention(t *testing.T) {
	out := ticketScreen(t, 80, fixtureTicket("APP-42", "todo", "short"))
	for _, want := range []string{"WHERE IT IS", "◉", "todo", "doing", "NEEDS ATTENTION", "Expand the description", "PULL REQUESTS", "NEXT MOVES"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTicketScreenSaysWhenNothingNeedsAttention(t *testing.T) {
	out := ticketScreen(t, 80, fixtureTicket("APP-42", "todo", strings.Repeat("x", 120)))
	if !strings.Contains(out, "Nothing needs attention.") || !strings.Contains(out, "Ready to move to") {
		t.Fatalf("a refined ticket should be ready:\n%s", out)
	}
}

func TestTicketScreenExplainsTicketsOutsideTheBoardSearch(t *testing.T) {
	tk := fixtureTicket("APP-42", "todo", strings.Repeat("x", 120))
	tk.OutOfScope = true
	tk.Facts["type"] = "Sub-task"
	out := ticketScreen(t, 80, tk)
	flat := strings.Join(strings.Fields(out), " ")
	if !strings.Contains(flat, "Sub-task") || !strings.Contains(flat, "shown because you asked") {
		t.Fatalf("an out-of-scope ticket must say why it is shown:\n%s", out)
	}
	if strings.Contains(ticketScreen(t, 80, fixtureTicket("APP-43", "todo", strings.Repeat("x", 120))), "because you asked") {
		t.Fatal("an in-scope ticket must not carry the note")
	}
}

func TestTicketScreenIsPlainAndFitsNarrowTerminals(t *testing.T) {
	for _, width := range []int{40, 60, 100} {
		out := ticketScreen(t, width, fixtureTicket("APP-42", "review", "short"))
		if strings.Contains(out, "\x1b") {
			t.Fatalf("colour codes with NO_COLOR at %d columns: %q", width, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if rowWidth(row{plain(line)}) > width {
				t.Fatalf("line wider than %d columns: %q", width, line)
			}
		}
	}
}

func TestForwardStagesIsOnePathThroughTheCurrentStage(t *testing.T) {
	w, err := workfile.Load("../../example/branching")
	if err != nil {
		t.Fatal(err)
	}
	for current, want := range map[string]string{
		"review":    "intake triage discovery ready doing review qa release done",
		"security":  "intake triage discovery ready doing review security release done",
		"blocked":   "intake triage discovery ready doing review qa release done",
		"cancelled": "intake triage discovery ready doing review qa release done",
	} {
		if got := strings.Join(forwardStages(w.Policy, current), " "); got != want {
			t.Errorf("%s: got %q, want %q", current, got, want)
		}
	}
}

func TestTicketScreenPanelsFitWithColourAndBackground(t *testing.T) {
	w, err := workfile.Load("../../example/branching")
	if err != nil {
		t.Fatal(err)
	}
	a := w.Assess(fixtureTicket("APP-42", "review", "short"))
	for _, width := range []int{40, 60} {
		v := view{out: &strings.Builder{}, color: true, links: true, width: width, tint: &tintCache{}}
		v.tint.once.Do(func() { v.tint.code = "48;5;236" })
		out := newTicketModel(v, w, a).render()
		if !strings.Contains(out, "\x1b[48;5;236m") {
			t.Fatalf("no panel background at %d columns", width)
		}
		for _, line := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("line %d wide at %d columns: %q", got, width, line)
			}
		}
	}
}

func TestPipedTicketScreenWritesOutWebAddresses(t *testing.T) {
	tk := fixtureTicket("APP-42", "review", "short")
	tk.Records = map[string][]workfile.Record{"github": {{ID: "api#18", URL: "https://github.com/example-team/api/pull/18",
		Facts: workfile.Facts{"checks": "success", "state": "open", "author": "x", "review_requests": []string{}, "approvals": []workfile.Actor{}, "labels": []string{}}}}}
	out := ticketScreen(t, 60, tk)
	for _, want := range []string{tk.URL, "https://github.com/example-team/api/pull/18"} {
		if strings.Count(out, want) != 1 {
			t.Errorf("want %q once in:\n%s", want, out)
		}
	}
}

func TestStepperLinkSaysWhetherTheNextMoveIsOpen(t *testing.T) {
	w, err := workfile.Load("../../example/destination-requires")
	if err != nil {
		t.Fatal(err)
	}
	for description, want := range map[string]string{strings.Repeat("x", 120): "◉━", "short": "◉╌"} {
		a := w.Assess(fixtureTicket("APP-42", "todo", description))
		out := newTicketModel(view{width: 60, tint: &tintCache{}}, w, a).render()
		if !strings.Contains(out, want) {
			t.Errorf("want %q for a %d-character description:\n%s", want, len(description), out)
		}
	}
}
