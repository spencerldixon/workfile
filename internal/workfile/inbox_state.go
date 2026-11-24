package workfile

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// InboxState is what the Tracker inbox remembers on this computer. Done and Snoozed
// are keyed by row ID, FirstSeen by row ID, kind and state; Seen by Activity.Key(); Cursors and Checks by
// PR ID, plus "tracker" in Cursors. It is local only: clearing a row never
// changes a ticket or PR.
type InboxState struct {
	Cursors   map[string]time.Time  `json:"cursors"`
	Done      map[string]string     `json:"done"`
	Snoozed   map[string]Snooze     `json:"snoozed"`
	Seen      map[string]time.Time  `json:"seen"`
	FirstSeen map[string]time.Time  `json:"first_seen"`
	Checks    map[string]ChecksSeen `json:"checks"`
	Agents    []AgentNote           `json:"agents"`
	// Events are pending activity, kept across restarts even after the
	// provider cursor moves on.
	Events []Activity `json:"events"`
}

// Snooze hides a row until a time or until it has a new blocker.
type Snooze struct {
	Blocker     string    `json:"blocker,omitempty"`
	Until       time.Time `json:"until"`
	Fingerprint string    `json:"fingerprint"`
}

// ChecksSeen is the last checks state seen on a PR, and when they turned
// green after not being green, so "checks passed" shows once and can be seen.
type ChecksSeen struct {
	Head   string    `json:"head,omitempty"`
	State  string    `json:"state"`
	Failed time.Time `json:"failed,omitzero"`
	Passed time.Time `json:"passed"`
}

// AgentNote records an agent the person launched from the inbox.
type AgentNote struct {
	Row    string    `json:"row"`
	Ticket string    `json:"ticket"`
	PR     string    `json:"pr"`
	Kind   string    `json:"kind"`
	Prompt string    `json:"prompt"`
	At     time.Time `json:"at"`
}

func NewInboxState() InboxState {
	return InboxState{
		Cursors:   map[string]time.Time{},
		Done:      map[string]string{},
		Snoozed:   map[string]Snooze{},
		Seen:      map[string]time.Time{},
		FirstSeen: map[string]time.Time{},
		Checks:    map[string]ChecksSeen{},
	}
}

// InboxStatePath sits beside the status --since snapshots of this workspace.
func InboxStatePath(w *Workspace) string {
	return filepath.Join(filepath.Dir(StateDir(w)), "inbox.json")
}

// ReadInboxState never fails: a missing, corrupt or older file starts fresh.
func ReadInboxState(w *Workspace) InboxState {
	s := NewInboxState()
	data, err := os.ReadFile(InboxStatePath(w))
	if err != nil {
		return s
	}
	var read InboxState
	if json.Unmarshal(data, &read) != nil {
		return s
	}
	maps.Copy(s.Cursors, read.Cursors)
	maps.Copy(s.Done, read.Done)
	maps.Copy(s.Snoozed, read.Snoozed)
	maps.Copy(s.Seen, read.Seen)
	maps.Copy(s.FirstSeen, read.FirstSeen)
	maps.Copy(s.Checks, read.Checks)
	s.Agents = read.Agents
	s.Events = read.Events
	return s
}

// WriteInboxState replaces the file in one step, so a crash never leaves half
// a file behind.
func WriteInboxState(w *Workspace, s InboxState) error {
	path := InboxStatePath(w)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".inbox-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

const (
	seenKeep  = 14 * 24 * time.Hour
	agentKeep = 7 * 24 * time.Hour
	// firstSeenKeep outlasts any wait worth showing.
	firstSeenKeep = 30 * 24 * time.Hour
)

// Prune drops expired metadata and old activity outside the current inbox.
func (s *InboxState) Prune(now time.Time, keepPending func(Activity) bool) {
	for k, v := range s.Snoozed {
		if !v.Until.After(now) {
			delete(s.Snoozed, k)
		}
	}
	for k, v := range s.FirstSeen {
		if now.Sub(v) > firstSeenKeep {
			delete(s.FirstSeen, k)
		}
	}
	kept := s.Agents[:0]
	for _, note := range s.Agents {
		if now.Sub(note.At) <= agentKeep {
			kept = append(kept, note)
		}
	}
	s.Agents = kept
	events := s.Events[:0]
	for _, e := range s.Events {
		if _, seen := s.Seen[e.Key()]; !seen && (now.Sub(e.At) <= seenKeep || keepPending(e)) {
			events = append(events, e)
		}
	}
	s.Events = events
	for k, v := range s.Seen {
		if now.Sub(v) > seenKeep {
			delete(s.Seen, k)
		}
	}
}

// AddEvents keeps new events, once each.
func (s *InboxState) AddEvents(events []Activity) {
	have := map[string]bool{}
	for _, e := range s.Events {
		have[e.Key()] = true
	}
	for _, e := range events {
		if !have[e.Key()] {
			have[e.Key()] = true
			s.Events = append(s.Events, e)
		}
	}
}
