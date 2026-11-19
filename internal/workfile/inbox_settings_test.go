package workfile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInboxSettingsDefaults(t *testing.T) {
	w := trackerWorkspace(t, "", "", "")
	s, err := ReadInboxSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if s.Poll.Duration != 2*time.Minute || s.Snooze.Duration != time.Hour || s.Agent != "" || s.RefreshEvery != 5 || s.FullEvery != 15 {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	if len(s.Prompts) != len(PromptKinds) {
		t.Fatalf("want a default prompt for every kind, got %v", s.Prompts)
	}
	if s.Prompts["default"] != "Get {pr} past {stage} for {key}" || s.Prompts["review_requested"] != "Review {pr} for {who}" {
		t.Fatalf("wrong default prompts: %v", s.Prompts)
	}
}

func TestInboxSettingsLayers(t *testing.T) {
	w := trackerWorkspace(t,
		"inbox:\n  poll: 5m\n  refresh_every: 4\n  full_every: 12\n  agent: personal {prompt}\n  prompts:\n    ready: \"Ship {key}\"\n",
		"tracker:\n  default_view: all\ninbox:\n  poll: 3m\n  refresh_every: 6\n  snooze: 2h\n",
		"inbox:\n  poll: 1m\n  full_every: 18\n")
	s, err := ReadInboxSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if s.Poll.Duration != time.Minute || s.Snooze.Duration != 2*time.Hour || s.Agent != "personal {prompt}" || s.RefreshEvery != 6 || s.FullEvery != 18 {
		t.Fatalf("layers merged wrongly: %+v", s)
	}
	if s.Prompts["ready"] != "Ship {key}" || s.Prompts["default"] != "Get {pr} past {stage} for {key}" {
		t.Fatalf("one prompt override should keep the others: %v", s.Prompts)
	}
	if _, err := ReadTrackerSettings(w); err != nil {
		t.Fatalf("an inbox section must not break tracker settings: %v", err)
	}
}

func TestInboxSettingsRejects(t *testing.T) {
	for _, tc := range []struct{ name, shared, want string }{
		{"negative refresh count", "inbox:\n  refresh_every: -1\n", "refresh_every"},
		{"negative full count", "inbox:\n  full_every: -1\n", "full_every"},
		{"short poll", "inbox:\n  poll: 10s\n", "at least 30s"},
		{"unknown kind", "inbox:\n  prompts:\n    approved: x\n", "approved"},
		{"unknown placeholder", "inbox:\n  prompts:\n    default: \"{nope}\"\n", "{nope}"},
		{"agent in the repo", "inbox:\n  agent: run {prompt}\n", "personal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := trackerWorkspace(t, "", tc.shared, "")
			_, err := ReadInboxSettings(w)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error with %q, got %v", tc.want, err)
			}
			if !strings.HasPrefix(err.Error(), filepath.Join(w.Dir, "config.yml")+": inbox.") {
				t.Fatalf("error should name the file and inbox key: %v", err)
			}
		})
	}
}

func TestInboxAgentOnlyFromPersonalLayers(t *testing.T) {
	w := trackerWorkspace(t, "inbox:\n  agent: mine {prompt}\n", "", "")
	s, err := ReadInboxSettings(w)
	if err != nil || s.Agent != "mine {prompt}" {
		t.Fatalf("personal agent: %q %v", s.Agent, err)
	}
	w = trackerWorkspace(t, "", "", "inbox:\n  agent: here {prompt}\n")
	if s, err := ReadInboxSettings(w); err != nil || s.Agent != "here {prompt}" {
		t.Fatalf("per-workspace agent: %q %v", s.Agent, err)
	}
	w = trackerWorkspace(t, "inbox:\n  agent: run {nope}\n", "", "")
	if _, err := ReadInboxSettings(w); err == nil || !strings.Contains(err.Error(), "{nope}") {
		t.Fatalf("unknown placeholder in a personal agent: %v", err)
	}
}
