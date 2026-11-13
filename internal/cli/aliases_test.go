package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestStatusModeTable(t *testing.T) {
	for _, c := range []struct {
		name string
		o    options
		want statusMode
	}{
		{"nothing", options{}, modeRollup},
		{"failing only", options{failing: true}, modeRollup},
		{"keys", options{keys: []string{"APP-1"}}, modeTickets},
		{"keys beat everything", options{keys: []string{"APP-1"}, me: true, gate: "g"}, modeTickets},
		{"me", options{me: true}, modeMine},
		{"user", options{user: "bob"}, modeMine},
		{"gate", options{gate: "code_review"}, modeListing},
		{"stage", options{stage: "done"}, modeListing},
		{"gate with me", options{gate: "code_review", me: true}, modeListing},
	} {
		if got := c.o.statusMode(); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAliasesRunTheirTargetAndNoteOnStderrOnly(t *testing.T) {
	tk := fixtureTicket("APP-1", "todo", "short")
	for _, c := range []struct {
		args []string
		note string
	}{
		{[]string{"health", "--json"}, "wf: health is now status. The old name will stop working in a later release.\n"},
		{[]string{"tickets", "--view", "todo"}, "wf: tickets is now tracker. The old name will stop working in a later release.\n"},
		{[]string{"test", "jira"}, "wf: test is now setup --test. The old name will stop working in a later release.\n"},
		{[]string{"prove", "--json"}, "wf: prove is now lint. The old name will stop working in a later release.\n"},
	} {
		f := &fakeSource{tickets: []workfile.Ticket{tk}}
		code, out, errOut := invoke(t, f, c.args...)
		if errOut != c.note {
			t.Errorf("%v: stderr %q", c.args, errOut)
		}
		if strings.Contains(out, "is now") {
			t.Errorf("%v: note leaked into stdout", c.args)
		}
		if slicesContains(c.args, "--json") && !json.Valid([]byte(out)) {
			t.Errorf("%v: stdout is not valid JSON: %d %q", c.args, code, out)
		}
	}
	f := &fakeSource{}
	if _, _, _ = invoke(t, f, "test", "jira"); len(f.tested) != 1 || f.tested[0] != "jira" {
		t.Errorf("test alias did not run setup --test: %v", f.tested)
	}
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestSetupTestAllPassIsExitZero(t *testing.T) {
	f := &fakeSource{}
	code, out, _ := invoke(t, f, "setup", "--test")
	if code != 0 || len(f.tested) != 2 || !strings.Contains(out, "✓ jira") {
		t.Fatalf("%d %v %s", code, f.tested, out)
	}
	if code, _, errOut := invoke(t, f, "setup", "jira"); code != 2 || !strings.Contains(errOut, "--test") {
		t.Fatalf("names without --test: %d %q", code, errOut)
	}
	if code, _, _ := invoke(t, f, "setup", "--test", "--dir", t.TempDir()); code != 2 {
		t.Fatalf("no workspace: %d", code)
	}
}

func TestHelpEndsWithTheNextStepAndNamesFiveCommands(t *testing.T) {
	run := func(dir string) string {
		var out bytes.Buffer
		if code := run(context.Background(), []string{"--dir", dir}, &out, &bytes.Buffer{}, "t", nil); code != 0 {
			t.Fatal(code)
		}
		return strings.TrimRight(out.String(), "\n")
	}
	if out := run(t.TempDir()); !strings.HasSuffix(out, "No .workfile/ here. Start with: wf setup") {
		t.Fatalf("no workspace:\n%s", out)
	}
	if out := run("../../example/branching"); !strings.HasSuffix(out, "Try: wf status --me") || !strings.Contains(out[strings.LastIndex(out, "\n"):], "Using ") {
		t.Fatalf("workspace:\n%s", out)
	}
	text := helpText(".")
	commands := 0
	for _, line := range strings.Split(strings.SplitN(text, "Options for status", 2)[0], "\n") {
		if strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			commands++
		}
	}
	if commands != 5 {
		t.Fatalf("help names %d commands", commands)
	}
	if strings.Contains(text, "—") {
		t.Fatal("em dash in help")
	}
}
