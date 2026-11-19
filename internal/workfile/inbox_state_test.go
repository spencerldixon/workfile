package workfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func stateWorkspace(t *testing.T) *Workspace {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return &Workspace{Dir: t.TempDir(), Policy: Policy{Name: "example-web"}}
}

func TestInboxStateRoundTrip(t *testing.T) {
	w := stateWorkspace(t)
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := NewInboxState()
	s.Cursors["example-pr"] = at
	s.Done["ABC-1"] = "changes_requested|abc"
	s.Snoozed["ABC-2"] = Snooze{Until: at.Add(time.Hour), Fingerprint: "ready|x"}
	s.Seen["k"] = at
	s.FirstSeen["ABC-3"] = at
	s.Checks["example-pr"] = ChecksSeen{State: "success", Passed: at}
	s.Agents = []AgentNote{{Row: "ABC-1", Ticket: "ABC-1", PR: "#84", Kind: "comments", Prompt: "Answer", At: at}}
	if err := WriteInboxState(w, s); err != nil {
		t.Fatal(err)
	}
	if got := ReadInboxState(w); !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip changed state:\n got %+v\nwant %+v", got, s)
	}
	info, err := os.Stat(InboxStatePath(w))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode %v, want 0600", info.Mode().Perm())
	}
	if filepath.Dir(InboxStatePath(w)) != filepath.Dir(StateDir(w)) {
		t.Fatal("inbox.json should sit beside the snapshots folder")
	}
}

func TestInboxStateCorruptStartsFresh(t *testing.T) {
	w := stateWorkspace(t)
	path := InboxStatePath(w)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := ReadInboxState(w)
	if s.Done == nil || s.Seen == nil || s.Cursors == nil || s.Snoozed == nil || s.FirstSeen == nil || s.Checks == nil {
		t.Fatalf("corrupt state must start fresh with usable maps: %+v", s)
	}
	s.Done["x"] = "y" // must not panic
}

func TestInboxStateUnwritable(t *testing.T) {
	w := stateWorkspace(t)
	blocker := filepath.Dir(InboxStatePath(w))
	if err := os.MkdirAll(filepath.Dir(blocker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("a file where a folder is needed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteInboxState(w, NewInboxState()); err == nil {
		t.Fatal("want an error when the folder cannot be made")
	}
}

func TestInboxStatePrune(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := NewInboxState()
	s.Snoozed["old"] = Snooze{Until: now.Add(-time.Minute)}
	s.Snoozed["live"] = Snooze{Until: now.Add(time.Minute)}
	s.Seen["old"] = now.Add(-15 * 24 * time.Hour)
	s.Seen["new"] = now.Add(-time.Hour)
	s.Agents = []AgentNote{{Row: "old", At: now.Add(-8 * 24 * time.Hour)}, {Row: "new", At: now.Add(-time.Hour)}}
	seenEvent := Activity{PR: "a#1", Kind: ActivityComment, At: now.Add(-time.Hour)}
	s.Seen[seenEvent.Key()] = now
	s.Events = []Activity{
		seenEvent,
		{PR: "a#2", Kind: ActivityComment, At: now.Add(-15 * 24 * time.Hour)},
		{PR: "a#3", Kind: ActivityComment, At: now.Add(-time.Hour)},
		{PR: "a#4", Kind: ActivityComment, At: now.Add(-15 * 24 * time.Hour)},
	}
	s.Prune(now, func(e Activity) bool { return e.PR == "a#4" })
	if len(s.Events) != 2 || s.Events[0].PR != "a#3" || s.Events[1].PR != "a#4" {
		t.Errorf("keep recent events and pending activity still in scope: %+v", s.Events)
	}
	if _, ok := s.Snoozed["old"]; ok {
		t.Error("expired snooze kept")
	}
	if _, ok := s.Snoozed["live"]; !ok {
		t.Error("live snooze dropped")
	}
	if _, ok := s.Seen["old"]; ok {
		t.Error("seen older than 14 days kept")
	}
	if _, ok := s.Seen["new"]; !ok {
		t.Error("recent seen dropped")
	}
	if len(s.Agents) != 1 || s.Agents[0].Row != "new" {
		t.Errorf("agent notes older than 7 days should go: %+v", s.Agents)
	}
}
