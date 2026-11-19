package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func TestInboxPlainNoWrite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	path := workfile.InboxStatePath(w)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"cursors":{"tracker":"2026-11-17T09:00:00Z"}}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := invoke(t, inboxSourceFixture(), "inbox")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	after, err := os.ReadFile(path)
	info, _ := os.Stat(path)
	if err != nil || !bytes.Equal(before, after) || !info.ModTime().Equal(old) {
		t.Fatal("plain output must never write inbox state")
	}
}

func TestInboxPlainOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := inboxSourceFixture()
	f.activity = []workfile.Activity{{Connection: "github", PR: "app#84", Kind: workfile.ActivityApproved, WhoID: "example-charlie", At: time.Now().Add(-5 * time.Minute)}}
	f.myPRs = map[string][]workfile.Record{"github": {inboxPRRecord("api#7", "example-alice", workfile.Facts{"review_requests": []string{"example-bob"}})}}
	code, out, errOut := invoke(t, f, "inbox")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output has escapes:\n%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "Inbox 2" {
		t.Fatalf("first line is the count of rows that need you:\n%s", out)
	}
	want := []string{
		"‼ APP-1  Out of policy: ",
		"  https://example-team.atlassian.net/browse/APP-1",
		"● #7  Alice asked you to review #7  ",
		"  https://github.example.com/example-team/api/pull/7",
	}
	body := strings.Join(lines[2:], "\n")
	at := 0
	for _, w := range want {
		i := strings.Index(body[at:], w)
		if i < 0 {
			t.Fatalf("plain output lacks %q in order:\n%s", w, out)
		}
		at += i + len(w)
	}
}
