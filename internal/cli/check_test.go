package cli

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// assertWidth fails when any line is wider than the screen.
func assertWidth(t *testing.T, output string, width int) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if w := runewidth.StringWidth(line); w > width {
			t.Fatalf("line is %d columns, wider than %d:\n%s", w, width, line)
		}
	}
}

func assertPlain(t *testing.T, output string) {
	t.Helper()
	if strings.Contains(output, "\x1b") {
		t.Fatalf("plain output has escape codes: %q", output)
	}
}

func TestParseNewCommands(t *testing.T) {
	for _, args := range [][]string{
		{"check", "APP-1", "--to", "done"}, {"check", "42", "--to", "done", "--json"},
	} {
		if _, err := parse(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"check", "APP-1"}, {"check", "--to", "done"}, {"check", "APP-1", "APP-2", "--to", "done"}, {"check", "APP-1", "--to", "done", "--me"},
		{"watch", "--once"}, {"status", "--once"}, {"status", "--dry-run"},
		{"status", "--to", "done"},
	} {
		if _, err := parse(args); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestCheckAnswersFromThePolicy(t *testing.T) {
	ready := fixtureTicket("APP-1", "todo", strings.Repeat("a", 120))
	short := fixtureTicket("APP-2", "todo", "too short")
	f := &fakeSource{tickets: []workfile.Ticket{ready, short}}

	code, out, _ := invoke(t, f, "check", "APP-1", "--to", "doing")
	if code != 0 || !strings.Contains(out, "CAN MOVE TO DOING") {
		t.Fatalf("%d %s", code, out)
	}
	assertPlain(t, out)

	code, out, _ = invoke(t, f, "check", "APP-2", "--to", "doing", "--json")
	var report checkReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if code != 1 || report.Allowed || report.ExitCode != 1 || len(report.Unmet) == 0 || report.From != "todo" {
		t.Fatalf("%d %+v", code, report)
	}

	code, out, _ = invoke(t, f, "check", "APP-1", "--to", "done")
	if code != 1 || !strings.Contains(out, "no move from todo to done") || !strings.Contains(out, "It can move to doing") {
		t.Fatalf("%d %s", code, out)
	}
	if code, _, errOut := invoke(t, f, "check", "APP-1", "--to", "nowhere"); code != 2 || !strings.Contains(errOut, "not a stage") {
		t.Fatalf("%d %s", code, errOut)
	}
	if code, _, errOut := invoke(t, f, "check", "APP-9", "--to", "doing"); code != 2 || !strings.Contains(errOut, "not visible") {
		t.Fatalf("%d %s", code, errOut)
	}
}

func TestCheckFitsNarrowTerminals(t *testing.T) {
	f := &fakeSource{tickets: []workfile.Ticket{fixtureTicket("APP-2", "todo", "too short")}}
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", strconv.Itoa(width))
		_, out, _ := invoke(t, f, "check", "APP-2", "--to", "doing")
		if !strings.Contains(out, "CANNOT MOVE TO DOING YET") {
			t.Fatal(out)
		}
		assertWidth(t, out, width)
	}
}
