package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

func inboxPRRecord(id, author string, facts workfile.Facts) workfile.Record {
	f := workfile.Facts{"state": "open", "author": author, "checks": "success", "review_requests": []string{}, "changes_requested": []string{}}
	for k, v := range facts {
		f[k] = v
	}
	return workfile.Record{ID: id, URL: "https://github.example.com/example-team/" + strings.Replace(id, "#", "/pull/", 1), Head: "h-" + id, Facts: f}
}

// inboxFixtureData has, oldest first: a changes-requested row (APP-1), a
// comments row (APP-4), and an information row (APP-5).
func inboxFixtureData() inboxData {
	at := loadNow
	return inboxData{
		at:          at,
		trackerRead: true,
		who:         map[string]string{"github": "example-bob", "jira": "example-bob"},
		names:       map[string]string{"example-alice": "Alice", "example-charlie": "Charlie"},
		work: []workfile.InboxWork{
			{Listing: workfile.Listing{Key: "APP-1", Title: "Fix login", State: "review", URL: "https://example-team.atlassian.net/browse/APP-1"}, Mine: true,
				PRs: map[string][]workfile.Record{"github": {inboxPRRecord("app#84", "example-bob", workfile.Facts{"changes_requested": []string{"example-alice"}})}}},
			{Listing: workfile.Listing{Key: "APP-4", Title: "Cache", State: "review"}, Mine: true,
				PRs: map[string][]workfile.Record{"github": {inboxPRRecord("app#90", "example-bob", nil)}}},
			{Listing: workfile.Listing{Key: "APP-5", Title: "Search", State: "review"}, Mine: true,
				PRs: map[string][]workfile.Record{"github": {inboxPRRecord("app#95", "example-bob", nil)}}},
		},
		events: []workfile.Activity{
			{Connection: "github", PR: "app#84", Kind: workfile.ActivityChangesRequested, WhoID: "example-alice", At: at.Add(-50 * time.Minute)},
			{Connection: "github", PR: "app#90", Kind: workfile.ActivityComment, WhoID: "example-alice", At: at.Add(-20 * time.Minute)},
			{Connection: "github", PR: "app#95", Kind: workfile.ActivityApproved, WhoID: "example-charlie", At: at.Add(-5 * time.Minute)},
		},
	}
}

type inboxDriver struct {
	t       *testing.T
	m       *inboxModel
	argv    [][]string
	startFn func([]string) error
	saves   int
	saveErr error
}

func driveInbox(t *testing.T, s workfile.InboxSettings) *inboxDriver {
	t.Helper()
	e := inboxEnv(t, inboxSourceFixture())
	if s.Poll.Duration == 0 {
		def, err := workfile.ReadInboxSettings(e.w)
		if err != nil {
			t.Fatal(err)
		}
		def.Agent = s.Agent
		for k, v := range s.Prompts {
			def.Prompts[k] = v
		}
		s = def
	}
	d := &inboxDriver{t: t}
	m := newInboxModel(context.Background(), e, s, workfile.NewInboxState())
	m.now = func() time.Time { return loadNow }
	m.start = func(_ context.Context, argv []string) error {
		d.argv = append(d.argv, argv)
		if d.startFn != nil {
			return d.startFn(argv)
		}
		return nil
	}
	m.open = func(string) error { return nil }
	m.save = func(workfile.InboxState) error { d.saves++; return d.saveErr }
	d.m = m
	d.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	d.send(inboxLoadedMsg(inboxFixtureData()))
	return d
}

// send delivers msg and runs any command it returns once, feeding agent
// results back, as the Bubble Tea loop would.
func (d *inboxDriver) send(msg tea.Msg) {
	d.t.Helper()
	cmd := d.m.Update(msg)
	if cmd == nil {
		return
	}
	if out, ok := cmd().(agentDoneMsg); ok {
		d.m.Update(out)
	}
}

func (d *inboxDriver) press(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "pgup":
			msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		cmd := d.m.Update(msg)
		if cmd == nil {
			continue
		}
		if out, ok := cmd().(agentDoneMsg); ok {
			d.m.Update(out)
		}
	}
}

func (d *inboxDriver) ids() []string {
	var ids []string
	for _, r := range d.m.rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func (d *inboxDriver) selectID(id string) {
	d.t.Helper()
	for d.m.sel > 0 {
		d.press("k")
	}
	for d.m.selected().ID != id {
		before := d.m.sel
		d.press("down")
		if d.m.sel == before {
			d.t.Fatalf("row %s not found in %v", id, d.ids())
		}
	}
}

func TestInboxFixtureRows(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	if got := d.ids(); !slices.Equal(got, []string{"APP-5", "APP-4", "APP-1"}) {
		t.Fatalf("fixture rows: %v", got)
	}
}

func TestInboxDoneHidesRow(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.selectID("APP-1")
	d.press("e")
	if slices.Contains(d.ids(), "APP-1") {
		t.Fatalf("done row still shown: %v", d.ids())
	}
	if d.m.st.Done["APP-1"] == "" || d.saves == 0 {
		t.Fatalf("done must be remembered and saved: %v saves=%d", d.m.st.Done, d.saves)
	}
	d.send(inboxLoadedMsg(inboxFixtureData()))
	if slices.Contains(d.ids(), "APP-1") {
		t.Fatal("done row came back on the next poll with nothing new")
	}
}

func TestInboxSnoozeWritesUntil(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.selectID("APP-1")
	d.press("s")
	if s := d.m.st.Snoozed["APP-1"]; !s.Until.Equal(loadNow.Add(time.Hour)) || s.Fingerprint == "" {
		t.Fatalf("snooze for the configured hour: %+v", s)
	}
	if slices.Contains(d.ids(), "APP-1") {
		t.Fatal("snoozed row still shown")
	}
}

func TestInboxAgentRunsOneArgPrompt(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "rex tab -- claude {prompt}"})
	d.selectID("APP-1")
	d.press("a")
	if len(d.argv) != 1 {
		t.Fatalf("agent not started: %v", d.argv)
	}
	argv := d.argv[0]
	if len(argv) != 5 || argv[4] != "Address Alice's review on #84 for APP-1" {
		t.Fatalf("prompt must be one argument: %q", argv)
	}
	if !slices.Contains(d.ids(), "APP-1") {
		t.Fatal("row stays after a successful launch")
	}
	if len(d.m.st.Agents) != 1 || d.m.st.Agents[0].Row != "APP-1" || d.m.st.Agents[0].Prompt != argv[4] {
		t.Fatalf("launch must be noted: %+v", d.m.st.Agents)
	}
}

func TestInboxAgentMissingPlaceholder(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}", Prompts: map[string]string{"changes_requested": "Fix {fix}"}})
	d.selectID("APP-1")
	d.press("a")
	if len(d.argv) != 0 {
		t.Fatalf("nothing may run with a missing value: %v", d.argv)
	}
	if !strings.Contains(d.m.toast, "Needs {fix}") || !slices.Contains(d.ids(), "APP-1") || len(d.m.st.Agents) != 0 {
		t.Fatalf("want a toast, the row kept and no note: %q %v", d.m.toast, d.ids())
	}
}

func TestInboxAgentNotFound(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "example-missing {prompt}"})
	d.startFn = func([]string) error { return errors.New(`exec: "example-missing": executable file not found in $PATH`) }
	d.selectID("APP-1")
	d.press("a")
	if !strings.Contains(d.m.toast, "Couldn't start agent") || !slices.Contains(d.ids(), "APP-1") || len(d.m.st.Agents) != 0 {
		t.Fatalf("a failed start keeps the row: %q %v", d.m.toast, d.ids())
	}
}

func TestInboxNoAgentHidesKey(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.selectID("APP-1")
	d.press("a")
	if len(d.argv) != 0 {
		t.Fatal("no agent configured, nothing runs")
	}
	if slices.ContainsFunc(d.m.footerKeys(), func(k [2]string) bool { return k[0] == "a" }) {
		t.Fatal("footer offers a with no agent")
	}
}

func TestInboxOpeningKeepsRowsUntilDone(t *testing.T) {
	for _, id := range []string{"APP-4", "APP-5"} {
		t.Run(id, func(t *testing.T) {
			d := driveInbox(t, workfile.InboxSettings{})
			d.selectID(id)
			var opened string
			d.m.open = func(target string) error {
				opened = target
				return nil
			}
			target := d.m.selected().PRURL
			d.press("enter", "o", "esc", "q")
			if target == "" || opened != target {
				t.Fatalf("opened %q, want %q", opened, target)
			}

			if err := workfile.WriteInboxState(d.m.e.w, d.m.st); err != nil {
				t.Fatal(err)
			}
			d.m.st = workfile.ReadInboxState(d.m.e.w)
			data := inboxFixtureData()
			data.events = nil // a new session relies on its persisted activity
			data.at = loadNow.Add(15 * 24 * time.Hour)
			d.m.now = func() time.Time { return data.at }
			d.send(inboxLoadedMsg(data))
			d.send(inboxLoadedMsg(data))
			if !slices.Contains(d.ids(), id) {
				t.Fatal("opening and restarting must keep the row")
			}

			d.selectID(id)
			d.press("e")
			d.send(inboxLoadedMsg(data))
			if slices.Contains(d.ids(), id) {
				t.Fatal("marking done must clear the row")
			}
		})
	}
}

func TestInboxStateWriteFailureToastsOnce(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	d.saveErr = errors.New("read-only file system")
	d.press("e")
	if !strings.Contains(d.m.toast, "Couldn't save inbox state") {
		t.Fatalf("want a save toast, got %q", d.m.toast)
	}
	d.m.toast = ""
	d.press("e")
	if d.m.toast == "Couldn't save inbox state" {
		t.Fatal("the save warning shows once")
	}
}

func TestInboxPollModes(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	counts := map[pollMode]int{}
	for i := 1; i <= 30; i++ {
		d.m.polls = i
		counts[d.m.pollMode()]++
	}
	if counts[pollFull] != 2 || counts[pollRefresh] != 4 || counts[pollQuick] != 24 {
		t.Fatalf("want the board every 15th poll, my tickets every 5th, else quick: %v", counts)
	}
	d.m.s.RefreshEvery = 3
	d.m.s.FullEvery = 6
	for i, want := range []pollMode{pollQuick, pollQuick, pollRefresh, pollQuick, pollQuick, pollFull} {
		d.m.polls = i + 1
		if got := d.m.pollMode(); got != want {
			t.Errorf("poll %d: got %v, want %v", i+1, got, want)
		}
	}
}

func TestInboxScrollsToSelection(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{})
	data := inboxFixtureData()
	for i := 0; i < 30; i++ {
		key := "APP-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		data.work = append(data.work, workfile.InboxWork{Listing: workfile.Listing{Key: key}, Mine: true, Err: "not visible"})
	}
	d.send(inboxLoadedMsg(data))
	d.send(tea.WindowSizeMsg{Width: 60, Height: 12})
	for i := 0; i < 25; i++ {
		d.press("down")
	}
	screen := inboxScreen(d)
	lines := strings.Split(screen, "\n")
	if len(lines) > 12 {
		t.Fatalf("screen is %d lines tall in a 12-line terminal", len(lines))
	}
	if !strings.Contains(screen, "▸ ") {
		t.Fatalf("the selected row stays in view:\n%s", screen)
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") && !strings.Contains(lines[len(lines)-1], "e resolve") {
		t.Fatalf("the footer stays on screen:\n%s", screen)
	}
}

func TestInboxAgentOfferedOnlyWhenItCanRun(t *testing.T) {
	d := driveInbox(t, workfile.InboxSettings{Agent: "claude {prompt}"})
	a := workfile.Assessment{Ticket: workfile.Ticket{Key: "APP-7"}}
	data := inboxFixtureData()
	data.work = []workfile.InboxWork{{Listing: workfile.Listing{Key: "APP-7", Title: "Spec"}, Mine: true, Assessment: &a, Next: workfile.NextActions{State: workfile.NeedsMe, Summary: "Add acceptance criteria"}}}
	data.loose = map[string][]workfile.Record{"github": {inboxPRRecord("api#7", "example-bob", nil)}}
	data.events = []workfile.Activity{{Connection: "github", PR: "api#7", Kind: workfile.ActivityComment, WhoID: "example-alice", At: loadNow.Add(-time.Minute)}}
	d.send(inboxLoadedMsg(data))
	hasA := func() bool {
		return slices.ContainsFunc(d.m.footerKeys(), func(k [2]string) bool { return k[0] == "a" })
	}
	d.selectID("APP-7")
	if hasA() {
		t.Fatal("the default prompt needs {pr}; a ticket fix with no PR must not offer a")
	}
	d.selectID("api#7")
	if !hasA() {
		t.Fatal("a PR with no ticket fills {key} with the PR, so a is offered")
	}
	d.press("a")
	if len(d.argv) != 1 || !strings.Contains(d.argv[0][1], "#7") {
		t.Fatalf("loose PR prompt: %q", d.argv)
	}
}
