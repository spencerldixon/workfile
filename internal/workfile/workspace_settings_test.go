package workfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, name, source string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", dir)
	if source == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "workfile", name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workfile", name, "config.yml"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSettingsLoadForTheNamedWorkspace(t *testing.T) {
	writeSettings(t, "example-web", "only_mine: true\n")
	if w := example(t); !w.Settings.OnlyMine {
		t.Fatal("only_mine was not read for example-web")
	}
}

func TestWorkspaceSettingsAreOptional(t *testing.T) {
	writeSettings(t, "example-web", "")
	if w := example(t); w.Settings.OnlyMine {
		t.Fatal("only_mine must default to false")
	}
}

func TestWorkspaceSettingsRejectUnknownFields(t *testing.T) {
	writeSettings(t, "example-web", "only_my: true\n")
	if _, err := Load(filepath.Join("..", "..", "example")); err == nil {
		t.Fatal("a misspelt setting must not be ignored")
	}
}

func TestMyJiraAssigneePrefersTheConfiguredAccount(t *testing.T) {
	writeSettings(t, "example-web", "only_mine: true\n")
	w := example(t)
	if who, err := w.MyJiraAssignee(); err != nil || who != "currentUser()" {
		t.Fatalf("got %q, %v", who, err)
	}
	config := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "workfile", "config.yml")
	if err := os.WriteFile(config, []byte("me:\n  jira: \"acc-1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if who, err := w.MyJiraAssignee(); err != nil || who != `"acc-1"` {
		t.Fatalf("got %q, %v", who, err)
	}
}
