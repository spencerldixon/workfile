package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"workfile/internal/automation"
	"workfile/internal/workfile"
)

// automationWorkspace copies the example workspace into a temporary folder,
// adds files under .workfile/, and points personal and state folders at a
// temporary home. With a remote it is also a git repository.
func automationWorkspace(t *testing.T, remote string, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("WORKFILE_CONFIG_FILE", "")
	root := t.TempDir()
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = files[name] + withoutName(data)
	}
	for name, content := range files {
		path := filepath.Join(root, ".workfile", name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if remote != "" {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not installed")
		}
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s", args, out)
			}
		}
	}
	return root
}

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
		{"automations"}, {"automations", "init"}, {"automations", "trust"}, {"automations", "--all"}, {"automations", "--log", "x"},
		{"watch"}, {"watch", "--once", "--dry-run"}, {"watch", "--interval", "5m"},
	} {
		if _, err := parse(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"check", "APP-1"}, {"check", "--to", "done"}, {"check", "APP-1", "APP-2", "--to", "done"}, {"check", "APP-1", "--to", "done", "--me"},
		{"automations", "delete"}, {"automations", "trust", "--all"}, {"automations", "--me"},
		{"watch", "--interval", "30s"}, {"watch", "APP-1"}, {"watch", "--json"},
		{"status", "--once"}, {"health", "--to", "done"},
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

const bobFile = `automations:
  - id: note-review
    on: {enters: review}
    run: [sh, -c, 'echo "$WORKFILE_TICKET $0" >> "$1"', "{stage}", OUT]
    mode: auto
`

func TestAutomationsListTrustAndInit(t *testing.T) {
	root := automationWorkspace(t, "git@github.com:example-team/web.git", map[string]string{"policy.yml": "name: example-web\n", "automations/bob.yml": bobFile})
	f := &fakeSource{}
	t.Setenv("COLUMNS", "40")
	code, out, _ := invoke(t, f, "automations", "--dir", root)
	if code != 1 || !strings.Contains(out, "have not approved") || !strings.Contains(out, "for Bob") {
		t.Fatalf("%d %s", code, out)
	}
	assertWidth(t, out, 40)
	assertPlain(t, out)

	if code, out, errOut := invoke(t, f, "automations", "trust", "--dir", root); code != 0 || !strings.Contains(out, "APPROVED") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	t.Setenv("COLUMNS", "60")
	code, out, _ = invoke(t, f, "automations", "--dir", root)
	if code != 0 || strings.Contains(out, "NEEDS ATTENTION") || !strings.Contains(out, "when it enters review") {
		t.Fatalf("approved: %d %s", code, out)
	}
	assertWidth(t, out, 60)

	if code, out, errOut := invoke(t, f, "automations", "init", "--dir", root); code != 0 || !strings.Contains(out, "github.com/example-team/web") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	if code, _, errOut := invoke(t, f, "automations", "init", "--dir", root); code != 2 || !strings.Contains(errOut, "already exists") {
		t.Fatalf("init twice: %d %s", code, errOut)
	}
	code, out, _ = invoke(t, f, "automations", "--dir", root)
	if code != 0 || !strings.Contains(out, "PRIVATE") || !strings.Contains(out, "ready-for-review") {
		t.Fatalf("the starting file loads and links: %d %s", code, out)
	}

	code, out, _ = invoke(t, f, "automations", "--all", "--dir", root)
	if code != 0 || !strings.Contains(out, "Bob") || strings.Contains(out, "ready-for-review") {
		t.Fatalf("--all shows shared files only: %s", out)
	}
}

func TestAutomationsInitNeedsANameAndARemote(t *testing.T) {
	root := automationWorkspace(t, "", map[string]string{})
	if code, _, errOut := invoke(t, &fakeSource{}, "automations", "init", "--dir", root); code != 2 || !strings.Contains(errOut, "add name:") {
		t.Fatalf("%d %s", code, errOut)
	}
	root = automationWorkspace(t, "", map[string]string{"policy.yml": "name: example-web\n"})
	if code, _, errOut := invoke(t, &fakeSource{}, "automations", "init", "--dir", root); code != 2 || !strings.Contains(errOut, "git") {
		t.Fatalf("%d %s", code, errOut)
	}
}

func TestPrivateFileFromAnotherRepositoryDoesNotRun(t *testing.T) {
	root := automationWorkspace(t, "https://github.com/example-other/web.git", map[string]string{"policy.yml": "name: example-web\n"})
	path := workfile.PrivateAutomationsPath("example-web")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("repos: [github.com/example-team/web]\nautomations:\n  - id: x\n    on: {enters: review}\n    run: [echo]\n    mode: auto\n"), 0600)
	code, out, _ := invoke(t, &fakeSource{}, "automations", "--dir", root)
	if code != 1 || !strings.Contains(strings.Join(strings.Fields(out), " "), "add github.com/example-other/web to repos") {
		t.Fatalf("%d %s", code, out)
	}
}

func bobsTicket(state string) workfile.Ticket {
	t := fixtureTicket("APP-7", state, strings.Repeat("a", 120))
	t.Title = "Explain notification preferences"
	t.Assignee = "example-bob"
	return t
}

func TestWatchOnceFiresOnlyOnChange(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	out := filepath.Join(t.TempDir(), "ran")
	root := automationWorkspace(t, "", map[string]string{"automations/bob.yml": strings.Replace(bobFile, "OUT", out, 1)})
	f := &fakeSource{tickets: []workfile.Ticket{bobsTicket("doing")}}
	invoke(t, f, "automations", "trust", "--dir", root)

	code, first, _ := invoke(t, f, "watch", "--once", "--dir", root)
	if code != 0 || !strings.Contains(first, "starting point") || strings.Contains(first, "fired") {
		t.Fatalf("the first check fires nothing: %d %s", code, first)
	}
	f.tickets = []workfile.Ticket{bobsTicket("review")}
	code, second, _ := invoke(t, f, "watch", "--once", "--dir", root)
	if code != 0 || !strings.Contains(second, "1 automation fired") || !strings.Contains(second, "note-review finished") {
		t.Fatalf("%d %s", code, second)
	}
	assertPlain(t, second)
	if data, _ := os.ReadFile(out); string(data) != "APP-7 review\n" {
		t.Fatalf("ran with %q", data)
	}
	_, third, _ := invoke(t, f, "watch", "--once", "--dir", root)
	if strings.Contains(third, "fired") {
		t.Fatalf("each change fires once: %s", third)
	}
	if code, log, _ := invoke(t, f, "automations", "--log", "note-review", "--dir", root); code != 0 || !strings.Contains(log, "finished") {
		t.Fatalf("log: %d %s", code, log)
	}
}

func TestWatchDryRunRunsNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ran")
	root := automationWorkspace(t, "", map[string]string{"automations/bob.yml": strings.Replace(bobFile, "OUT", out, 1)})
	f := &fakeSource{tickets: []workfile.Ticket{bobsTicket("doing")}}
	invoke(t, f, "automations", "trust", "--dir", root)
	invoke(t, f, "watch", "--once", "--dry-run", "--dir", root)
	f.tickets = []workfile.Ticket{bobsTicket("review")}
	_, output, _ := invoke(t, f, "watch", "--once", "--dry-run", "--dir", root)
	if !strings.Contains(output, "dry run: would") {
		t.Fatal(output)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a dry run ran a command")
	}
}

func TestWatchWithoutAutomationsSaysHowToStart(t *testing.T) {
	root := automationWorkspace(t, "", map[string]string{})
	code, out, _ := invoke(t, &fakeSource{}, "watch", "--once", "--dir", root)
	if code != 0 || !strings.Contains(out, "GET STARTED") {
		t.Fatalf("%d %s", code, out)
	}
}

const askFile = `automations:
  - id: ask-me
    on: {enters: review}
    run: [echo, "{ticket}", "{title}"]
    mode: ask
`

func TestWatchScreenAsksAndFits(t *testing.T) {
	root := automationWorkspace(t, "", map[string]string{"automations/bob.yml": askFile})
	f := &fakeSource{tickets: []workfile.Ticket{bobsTicket("review")}}
	invoke(t, f, "automations", "trust", "--dir", root)
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := loadAutomations(ctx, w, f, true)
	if err != nil {
		t.Fatal(err)
	}
	x := &watcher{w: w, client: f, s: s}
	e := automation.Event{Automation: "ask-me", Mode: workfile.ModeAsk, Trigger: "enters review", At: time.Now(), Ticket: "APP-7", Title: "Explain notification preferences in a title long enough to wrap", Stage: "review"}
	for _, width := range []int{40, 60} {
		m := newWatchModel(ctx, newWatchView(newView(&bytes.Buffer{}), x, 2*time.Minute, true))
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m.Update(checkDoneMsg(checkResult{at: time.Now(), events: []automation.Event{e}, run: []automation.Event{e}}))
		view := m.render()
		if !strings.Contains(view, "Run ask-me?") || !strings.Contains(view, "echo APP-7") {
			t.Fatalf("the question shows the exact command:\n%s", view)
		}
		assertWidth(t, view, width)
		assertPlain(t, view)
		for _, block := range m.printed {
			assertWidth(t, block, width)
		}
		m.key("n")
		if len(m.questions) != 0 || !strings.Contains(m.printed[len(m.printed)-1], "ask-me skipped") {
			t.Fatalf("n skips: %v", m.printed)
		}
		if view := m.render(); !strings.Contains(view, "Next check in 2m") {
			t.Fatalf("status: %s", view)
		}
		if cmd := m.key("q"); cmd == nil {
			t.Fatal("q stops watching")
		}
	}
	st, _ := s.store.State()
	if last, ok := st.Last("ask-me"); !ok || last.Outcome != automation.Declined {
		t.Fatalf("a declined question is recorded: %+v", last)
	}
}

func TestQuestionsWithoutATerminalWait(t *testing.T) {
	root := automationWorkspace(t, "", map[string]string{"automations/bob.yml": askFile})
	f := &fakeSource{tickets: []workfile.Ticket{bobsTicket("doing")}}
	invoke(t, f, "automations", "trust", "--dir", root)
	invoke(t, f, "watch", "--once", "--dir", root)
	f.tickets = []workfile.Ticket{bobsTicket("review")}
	_, out, _ := invoke(t, f, "watch", "--once", "--dir", root)
	if !strings.Contains(out, "waiting for your answer") || !strings.Contains(out, "Run wf watch in a terminal") {
		t.Fatal(out)
	}
}

// withoutName drops the example's name, so each test chooses its own.
func withoutName(data []byte) string {
	return regexp.MustCompile(`(?m)^name:.*\n`).ReplaceAllString(string(data), "")
}
