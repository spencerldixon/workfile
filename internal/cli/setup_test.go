package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupFake struct{ peopleErr bool }

func (setupFake) Identity(context.Context, string) (string, error) { return "example-user", nil }
func (setupFake) Test(context.Context, string) (string, error)     { return "connected", nil }
func (setupFake) SetupProjects(context.Context) ([]provider.SetupProject, error) {
	return []provider.SetupProject{{Key: "APP", Name: "Example project"}, {Key: "WEB", Name: "Example web"}}, nil
}
func (setupFake) SetupRepos(context.Context) ([]provider.SetupRepo, error) {
	return []provider.SetupRepo{{Name: "api"}, {Name: "web"}, {Name: "worker"}}, nil
}
func (setupFake) SetupStatuses(context.Context, string) ([]string, error) {
	return []string{"To Do", "In Progress", "Done"}, nil
}
func (f setupFake) SetupPeople(context.Context, string) ([]provider.SetupPerson, error) {
	if f.peopleErr {
		return nil, errors.New("permission denied")
	}
	return []provider.SetupPerson{{AccountID: "example-alice", DisplayName: "Alice", Active: true}}, nil
}

type setupTwoPeopleFake struct{ setupFake }

func (setupTwoPeopleFake) SetupPeople(context.Context, string) ([]provider.SetupPerson, error) {
	return []provider.SetupPerson{{AccountID: "example-alice", DisplayName: "Alice", Active: true}, {AccountID: "example-bob", DisplayName: "Bob", Active: true}}, nil
}

type setupRetryFake struct {
	setupFake
	calls int
}

func (f *setupRetryFake) Identity(context.Context, string) (string, error) {
	f.calls++
	if f.calls == 1 {
		return "", errors.New("provider returned HTTP 401; check the token")
	}
	return "example-user", nil
}
func TestSetupFlags(t *testing.T) {
	for _, args := range [][]string{{"setup", "--json"}, {"setup", "--me"}, {"setup", "APP-1"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := parse([]string{"setup", "--dir", "/tmp/example"}); err != nil {
		t.Fatal(err)
	}
}
func TestSetupStatesResolveCollisions(t *testing.T) {
	states := setupStates([]string{"In Review", "In-Review", "Done"}, []int{0, 1, 2})
	if strings.Join(states, " ") != "in_review in_review_2 done" {
		t.Fatalf("unexpected aliases: %v", states)
	}
}

// driver feeds messages to the model and plays back the results of its
// commands, so a whole setup can run without a terminal. Commands that only
// wait (blinks, ticks) are abandoned after a moment.
type driver struct {
	t     *testing.T
	m     *setupModel
	width int
	quit  bool

	pending []chan tea.Msg // commands still running, such as a save
}

func newDriver(t *testing.T, width int, client setupSource) (*driver, string) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WORKFILE_BACKGROUND", "off")
	root := t.TempDir()
	var out bytes.Buffer
	m := newSetupModel(context.Background(), view{out: &out, width: width}, setupDeps{
		dir:         root,
		credentials: filepath.Join(root, "private", "credentials"),
		open:        func(string) error { return errors.New("no browser") },
		connect:     func(*workfile.Workspace, map[string]string) setupSource { return client },
	})
	d := &driver{t: t, m: m, width: width}
	d.send(tea.WindowSizeMsg{Width: width, Height: 40})
	m.Init()
	return d, root
}

func (d *driver) send(msg tea.Msg) {
	d.t.Helper()
	_, cmd := d.m.Update(msg)
	d.run(cmd)
	d.checkView()
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		d.handle(msg)
	case <-time.After(30 * time.Millisecond):
		d.pending = append(d.pending, done)
	}
}

func (d *driver) handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
	case tea.QuitMsg:
		d.quit = true
	case spinner.TickMsg, animTickMsg:
	case testedMsg, reposMsg, projectsMsg, statusesMsg, peopleMsg, savedMsg, openedMsg:
		d.send(msg)
	default:
		// The filter answers with a message the list package keeps private.
		if strings.Contains(fmt.Sprintf("%T", msg), "FilterMatchesMsg") {
			d.send(msg)
		}
	}
}

// settle waits for slow commands while the model says it is working.
func (d *driver) settle() {
	deadline := time.Now().Add(5 * time.Second)
	for d.m.loading && time.Now().Before(deadline) {
		pending := d.pending
		d.pending = nil
		for _, ch := range pending {
			select {
			case msg := <-ch:
				d.handle(msg)
			default:
				d.pending = append(d.pending, ch)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// checkView fails if any line is wider than the terminal or leaks escapes.
func (d *driver) checkView() {
	d.t.Helper()
	view := d.m.render()
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > d.width {
			d.t.Fatalf("line is %d columns, terminal is %d: %q\n%s", w, d.width, line, view)
		}
	}
	if strings.ContainsAny(view, "\x1b\r") {
		d.t.Fatalf("escapes in plain output: %q", view)
	}
}

func (d *driver) press(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		switch k {
		case "enter":
			d.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			d.send(tea.KeyPressMsg{Code: tea.KeyEscape})
		case "down":
			d.send(tea.KeyPressMsg{Code: tea.KeyDown})
		case "shift+down":
			d.send(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
		case "space":
			d.send(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		case "ctrl+c":
			d.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		default:
			d.send(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		}
	}
}

// typeText replaces whatever is in the input.
func (d *driver) typeText(s string) {
	d.t.Helper()
	d.m.input.SetValue("")
	d.send(tea.PasteMsg{Content: s})
}

func (d *driver) view() string { return d.m.render() }

func (d *driver) want(s string) {
	d.t.Helper()
	d.settle()
	// Compare ignoring line breaks and runs of spaces: text wraps by width.
	if !strings.Contains(strings.Join(strings.Fields(d.view()), " "), strings.Join(strings.Fields(s), " ")) {
		d.t.Fatalf("expected %q on screen:\n%s", s, d.view())
	}
}

// toJira walks from the welcome screen to the Jira token screen.
func (d *driver) toJiraToken() {
	d.t.Helper()
	d.press("enter", "enter")
	d.typeText("example-team.atlassian.net")
	d.press("enter")
	d.typeText("alice@example.com")
	d.press("enter")
}

func (d *driver) toGitHubChoice() {
	d.t.Helper()
	d.toJiraToken()
	d.typeText("example-token")
	d.press("enter")
}

// finish goes from the review screen to the end.
func (d *driver) finish() {
	d.t.Helper()
	d.want("Ready to save")
	d.press("enter")
	d.want("Your workfile is ready")
}

func (d *driver) skipGitHub() {
	d.t.Helper()
	d.toGitHubChoice()
	d.want("Connect GitHub too?")
	d.press("n", "enter")
}

func (d *driver) toStatuses() {
	d.t.Helper()
	d.skipGitHub()
	d.want("Which projects does your team work in?")
	d.press("space", "enter")
	d.want("Which statuses should Workfile track?")
}

func TestSetupFlowCreatesWorkspaceAndPrivateCredentials(t *testing.T) {
	for _, width := range []int{40, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			d, root := newDriver(t, width, setupFake{})
			d.toStatuses()
			d.press("enter")
			d.finish()
			w, err := workfile.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(w.Policy.States) != 3 || len(w.Policy.Gates) != 0 || len(w.People) != 1 || len(w.Providers) != 1 {
				t.Fatalf("unexpected workspace: %+v", w)
			}
			c := workfile.Credentials{Path: d.m.deps.credentials}
			token, err := c.Get(w.Providers["jira"].Auth["token"])
			if err != nil || token != "example-token" {
				t.Fatalf("token not saved privately: %q %v", token, err)
			}
			data, err := os.ReadFile(filepath.Join(root, ".workfile", "providers.yml"))
			if err != nil || strings.Contains(string(data), "example-token") {
				t.Fatal("token written into the workspace")
			}
			if !d.m.saved {
				t.Fatal("not marked saved")
			}
		})
	}
}

func TestSetupTokenShowsStarsAndNeverTheToken(t *testing.T) {
	for _, width := range []int{40, 60} {
		d, _ := newDriver(t, width, setupFake{})
		d.toJiraToken()
		d.want("Nothing pasted yet")
		d.typeText("example-token")
		d.want("*************")
		d.want("13 characters pasted")
		if strings.Contains(d.view(), "example-token") {
			t.Fatal("token printed")
		}
	}
}

func TestSetupTokenRejectsQuotesAndSpaces(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.toJiraToken()
	d.press("enter")
	d.want("Paste your token to continue.")
	for _, bad := range []string{`"example-token"`, "example token"} {
		d.typeText(bad)
		d.press("enter")
		d.want("Paste only the token")
	}
	if d.m.step != stepJiraToken {
		t.Fatal("moved on with a bad token")
	}
}

func TestSetupInvalidEntriesStayOnTheScreen(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.press("enter", "enter")
	d.typeText("https://example.com")
	d.press("enter")
	d.want("Enter a Jira Cloud hostname")
	d.typeText("example-team.atlassian.net")
	d.press("enter")
	d.typeText("not-an-email")
	d.press("enter")
	d.want("Enter an email address")
}

func TestSetupBackAndCancel(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.press("enter", "enter")
	d.typeText("example-team.atlassian.net")
	d.press("enter")
	d.want("Your Jira email")
	d.press("esc")
	d.want("Your Jira site")
	if d.m.input.Value() != "example-team.atlassian.net" {
		t.Fatal("answer lost going back")
	}
	d.press("ctrl+c")
	if !d.quit || d.m.saved {
		t.Fatal("ctrl+c did not cancel")
	}
}

func TestSetupExistingWorkspaceIsNotOverwritten(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{})
	if err := os.Mkdir(filepath.Join(root, ".workfile"), 0755); err != nil {
		t.Fatal(err)
	}
	d.press("enter", "enter")
	d.want("already exists")
	if d.m.step != stepFolder {
		t.Fatal("accepted a folder that already has a workfile")
	}
	other := t.TempDir()
	d.typeText(other)
	d.press("enter")
	d.want("Your Jira site")
}

func TestSetupGitHubIsConnectedWhenChosen(t *testing.T) {
	for _, width := range []int{40, 60} {
		d, root := newDriver(t, width, setupFake{})
		d.toGitHubChoice()
		d.press("y", "enter")
		d.want("Your GitHub organisation")
		d.typeText("https://github.com/example-team")
		d.press("enter")
		d.want("not a GitHub URL")
		d.typeText("example-team")
		d.press("enter")
		d.want("Create a GitHub token")
		d.want("repo and read:org")
		d.typeText("example-github-token")
		d.press("enter")
		d.want("Which repositories does your team ship from?")
		d.press("space", "enter")
		d.want("Which projects does your team work in?")
		d.press("space", "enter", "enter")
		d.finish()
		w, err := workfile.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		c := workfile.Credentials{Path: d.m.deps.credentials}
		got, err := c.Get(w.Providers["github"].Auth["token"])
		if len(w.Providers) != 2 || w.Providers["github"].Org != "example-team" || err != nil || got != "example-github-token" {
			t.Fatalf("GitHub not configured: %+v %v", w.Providers, err)
		}
	}
}

func TestSetupGitHubTokenPageIsWrittenOutWhenPlain(t *testing.T) {
	d, _ := newDriver(t, 40, setupFake{})
	d.toGitHubChoice()
	d.press("y", "enter")
	d.typeText("example-team")
	d.press("enter")
	flat := strings.Join(strings.Fields(d.view()), "")
	if !strings.Contains(flat, githubTokenPage) {
		t.Fatalf("address not written out:\n%s", d.view())
	}
}

func TestSetupSkippingGitHubDropsItsProvider(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{})
	d.toGitHubChoice()
	d.press("y", "enter")
	d.typeText("example-team")
	d.press("enter")
	d.typeText("example-github-token")
	d.press("enter")
	d.want("Which repositories does your team ship from?")
	d.press("esc", "esc", "esc")
	d.want("Connect GitHub too?")
	d.press("n", "enter")
	d.want("Which projects does your team work in?")
	d.press("space", "enter", "enter")
	d.finish()
	w, err := workfile.Load(root)
	if err != nil || len(w.Providers) != 1 {
		t.Fatalf("GitHub kept after choosing no: %+v %v", w, err)
	}
}

func TestSetupStatusesCanBeUntickedAndReordered(t *testing.T) {
	d, root := newDriver(t, 60, setupTwoPeopleFake{})
	d.toStatuses()
	d.want("[✓] To Do")
	d.press("down", "space")
	d.want("[ ] In Progress")
	d.want("Pipeline  To Do → Done")
	d.press("enter")
	d.finish()
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(w.Policy.States, " ") != "to_do done" || len(w.People) != 2 {
		t.Fatalf("unexpected workspace: %+v", w)
	}
	if j := w.Providers["jira"]; len(j.Projects) != 1 || j.Projects[0] != "APP" || j.KeepDoneFor != "14d" || j.Scope != "" || strings.Join(j.SkipStatuses, ",") != "In Progress" {
		t.Fatalf("unexpected scope: %+v", j)
	}
	policy, err := os.ReadFile(filepath.Join(root, ".workfile", "policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.Split(strings.TrimSpace(string(policy)), "\n\n")
	for i, key := range []string{"tracker:", "states:", "transitions:", "gates:"} {
		if len(sections) != 4 || !strings.HasPrefix(sections[i], key) {
			t.Fatalf("policy sections wrong: %s", policy)
		}
	}
	people, err := os.ReadFile(filepath.Join(root, ".workfile", "people.yml"))
	if err != nil || !strings.HasPrefix(string(people), "alice:\n") || !strings.Contains(string(people), "\n\nbob:\n") {
		t.Fatalf("people not spaced: %s %v", people, err)
	}

	d, root = newDriver(t, 60, setupFake{})
	d.toStatuses()
	d.press("shift+down")
	d.want("Pipeline  In Progress → To Do → Done")
	d.press("enter")
	d.finish()
	w, err = workfile.Load(root)
	if err != nil || strings.Join(w.Policy.States, " ") != "in_progress to_do done" {
		t.Fatalf("reorder not saved: %+v %v", w, err)
	}
}

func TestSetupNeedsAtLeastOneStatus(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.toStatuses()
	d.press("a", "enter")
	d.want("Keep at least one status.")
	if d.m.step != stepStatuses {
		t.Fatal("moved on with no statuses")
	}
}

func TestSetupPeopleFailureStillSaves(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{peopleErr: true})
	d.toStatuses()
	d.press("enter")
	d.want("Could not import people")
	d.finish()
	w, err := workfile.Load(root)
	if err != nil || len(w.People) != 0 {
		t.Fatalf("unexpected workspace: %+v %v", w, err)
	}
}

func TestSetupConnectionFailureReturnsToTheFieldWithAMessage(t *testing.T) {
	fake := &setupRetryFake{}
	d, root := newDriver(t, 60, fake)
	d.toGitHubChoice()
	d.want("Could not connect to Jira")
	d.want("HTTP 401")
	if d.m.step != stepSite || d.m.input.Value() != "example-team.atlassian.net" {
		t.Fatalf("not back on the site field: step %d", d.m.step)
	}
	d.press("enter", "enter")
	d.typeText("example-token-2")
	d.press("enter")
	d.want("Connect GitHub too?")
	d.press("n", "enter", "space", "enter", "enter")
	d.finish()
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	c := workfile.Credentials{Path: d.m.deps.credentials}
	got, err := c.Get(w.Providers["jira"].Auth["token"])
	if err != nil || got != "example-token-2" || fake.calls != 2 {
		t.Fatalf("replacement token not saved: %q %v", got, err)
	}
}

func TestSetupCredentialFailureRemovesWorkspace(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{})
	if err := os.MkdirAll(d.m.deps.credentials, 0700); err != nil {
		t.Fatal(err)
	}
	d.toStatuses()
	d.press("enter")
	d.want("Ready to save")
	d.press("enter")
	d.want("Could not save")
	if _, err := os.Stat(filepath.Join(root, ".workfile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed save left a partial workspace")
	}
	if d.m.saved {
		t.Fatal("marked saved after failing")
	}
}

func TestSetupLeavingBeforeTheEndWritesNothing(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{})
	d.toStatuses()
	d.press("enter")
	d.want("Ready to save")
	d.press("ctrl+c")
	if _, err := os.Stat(filepath.Join(root, ".workfile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote files before the final confirm")
	}
}

func TestSetupNoProjectsIsAnError(t *testing.T) {
	d, _ := newDriver(t, 60, setupNoProjects{})
	d.skipGitHub()
	if d.m.fatal == nil || !d.quit {
		t.Fatal("expected a fatal error")
	}
}

type setupNoProjects struct{ setupFake }

func (setupNoProjects) SetupProjects(context.Context) ([]provider.SetupProject, error) {
	return nil, nil
}

func TestSetupStaleAnswersAreIgnored(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.toJiraToken()
	d.typeText("example-token")
	d.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	d.m.Update(testedMsg{id: d.m.loadID - 1})
	if d.m.step == stepAskGitHub {
		t.Fatal("answer for a cancelled request moved the screen")
	}
}

func TestSetupAnimationFramesFit(t *testing.T) {
	th := newTheme(false, "")
	var lines int
	for f := 0; f <= animLast+3; f++ {
		frame := th.animFrame(f)
		n := strings.Count(frame, "\n")
		if f > 0 && n != lines {
			t.Fatalf("frame %d changes height", f)
		}
		lines = n
		for _, line := range strings.Split(frame, "\n") {
			if lipgloss.Width(line) > 24 {
				t.Fatalf("frame %d is too wide: %q", f, line)
			}
		}
	}
	last := th.animFrame(animLast)
	if !strings.Contains(last, "w o r k f i l e") || strings.Contains(th.animFrame(0), "w o r k") {
		t.Fatal("wordmark should appear only at the end")
	}
}

func TestSetupPlainSkipsAnimation(t *testing.T) {
	d, _ := newDriver(t, 40, setupFake{})
	if d.m.animate || d.m.frame != animLast {
		t.Fatal("plain output should start on the finished frame")
	}
	if cmd := d.m.Init(); cmd != nil {
		t.Fatal("plain output should not tick")
	}
	d.want("w o r k f i l e")
}

func TestSetupAnimationSkipsOnAnyKey(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.m.animate, d.m.frame = true, 3
	d.press("x")
	if d.m.frame != animLast || d.m.step != stepWelcome {
		t.Fatal("a key should skip to the end without moving on")
	}
	d.press("enter")
	if d.m.step != stepFolder {
		t.Fatal("enter should begin after the animation")
	}
}

func TestSetupColourPanelKeepsItsBackground(t *testing.T) {
	th := newTheme(true, "48;2;30;30;30")
	panel := th.panel(th.good("ok")+" then "+th.bold("more")+"\nshort", 30)
	for _, line := range strings.Split(panel, "\n") {
		if lipgloss.Width(line) != 30 {
			t.Fatalf("panel line is %d wide: %q", lipgloss.Width(line), line)
		}
		if !strings.HasPrefix(line, "\x1b[48;2;30;30;30m") {
			t.Fatalf("line lost its background: %q", line)
		}
	}
	if strings.Contains(panel, "▌") {
		t.Fatal("panels have no edge")
	}
	if !strings.Contains(panel, "\x1b[0m\x1b[48;2;30;30;30m") {
		t.Fatal("background not restored after styled text")
	}
}

func TestSetupTokenValidation(t *testing.T) {
	for _, bad := range []string{"", " ", "a b", "a\nb", `"x"`, "x'y"} {
		if _, err := setupToken(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if got, err := setupToken("  example-token  "); err != nil || got != "example-token" {
		t.Fatal("rejected a good token")
	}
}

func TestSetupTypingKeysFillsTheInput(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.press("enter", "enter")
	d.m.input.SetValue("")
	d.press("a", "b", "c")
	if d.m.input.Value() != "abc" {
		t.Fatalf("typed keys lost: %q", d.m.input.Value())
	}
}

func TestSetupColourUsesOnlyTheTerminalsOwnColours(t *testing.T) {
	var out bytes.Buffer
	m := newSetupModel(context.Background(), view{out: &out, width: 60, color: true}, setupDeps{
		dir: t.TempDir(), background: "48;2;30;30;30",
		open:    func(string) error { return nil },
		connect: func(*workfile.Workspace, map[string]string) setupSource { return setupFake{} },
	})
	m.Init()
	var screens []string
	for _, step := range []setupStep{stepWelcome, stepFolder, stepAskGitHub, stepReview} {
		m.step = step
		m.statuses, m.items = []string{"To Do"}, []stageItem{{0, true}}
		screens = append(screens, m.render())
	}
	all := strings.Join(screens, "")
	if !strings.Contains(all, "\x1b[") {
		t.Fatal("expected colour")
	}
	for _, bad := range []string{"38;2;", "▌"} {
		if strings.Contains(all, bad) {
			t.Fatalf("found %q: only the 16 ANSI colours and a panel background are allowed", bad)
		}
	}
	for _, m := range regexp.MustCompile(`38;5;(\d+)`).FindAllStringSubmatch(all, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 15 {
			t.Fatalf("colour %d is outside the 16 ANSI colours", n)
		}
	}
}
