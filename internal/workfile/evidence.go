package workfile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Explicit IDs survive edits. Derived IDs survive reordering and message changes.
func ruleID(gate string, r Rule) string {
	if r.ID != "" {
		return r.ID
	}
	data, _ := json.Marshal(struct {
		If, When string
		By       []string
	}{r.If, r.When, r.By})
	return fmt.Sprintf("%s/%x", gate, sha256.Sum256(data))
}

func (w *Workspace) unavailable(e Expression, t Ticket, r Record) string {
	if e.Provider == w.Policy.Tracker {
		if reason := t.Unavailable[e.Fact]; reason != "" {
			return reason
		}
		if _, ok := t.Facts[e.Fact]; !ok {
			return "No evidence was supplied for " + e.Provider + "." + e.Fact
		}
		return ""
	}
	if e.Fact == "prs" {
		return t.Unavailable[e.Provider+".prs"]
	}
	if reason := r.Unavailable[e.Fact]; reason != "" {
		return reason
	}
	if _, ok := r.Facts[e.Fact]; !ok {
		return "No evidence was supplied for " + e.Provider + "." + e.Fact
	}
	return ""
}

// PolicyHash excludes credentials, provider settings, and personal identities.
func (w *Workspace) PolicyHash() string {
	data, _ := json.Marshal(w.Policy)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}
