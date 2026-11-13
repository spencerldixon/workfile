// Package snapshot saves what each wf status run saw, and compares runs, so
// wf status --since can say what changed. It does no terminal output and no
// network requests.
package snapshot

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"workfile/internal/workfile"
)

// Entry is what one status run saw for one ticket.
type Entry struct {
	Key, Title, Stage string
	Standing          workfile.Standing
	Unmet             []string // gates failing on the ticket's forward moves, pipeline order
	Ready             []string // stages a forward move is available to now
	SeenAt            time.Time
}

// Snapshot is what one status run saw.
type Snapshot struct {
	Version    int // 1
	TakenAt    time.Time
	PolicyHash string
	Entries    map[string]Entry
}

// FromAssessments records the tickets a run read. A ticket that could not be
// checked is left out, so it is not mistaken for one that recovered later.
func FromAssessments(hash string, at time.Time, all []workfile.Assessment) Snapshot {
	s := Snapshot{Version: 1, TakenAt: at, PolicyHash: hash, Entries: map[string]Entry{}}
	for _, a := range all {
		standing := a.Standing()
		if standing == workfile.CannotCheck {
			continue
		}
		e := Entry{Key: a.Ticket.Key, Title: a.Ticket.Title, Stage: a.Ticket.State, Standing: standing, SeenAt: at, Unmet: []string{}, Ready: []string{}}
		for _, c := range a.Earlier {
			e.addUnmet(c)
		}
		for _, r := range a.Routes {
			if r.Backward {
				continue
			}
			for _, c := range r.Checks {
				e.addUnmet(c)
			}
			if r.Available() && !slices.Contains(e.Ready, r.To) {
				e.Ready = append(e.Ready, r.To)
			}
		}
		s.Entries[e.Key] = e
	}
	return s
}

func (e *Entry) addUnmet(c workfile.Check) {
	if c.Outcome == "fail" && !slices.Contains(e.Unmet, c.Gate) {
		e.Unmet = append(e.Unmet, c.Gate)
	}
}

// Since is how far back --since looks: the last run, or a time ago.
type Since struct {
	Last bool
	Ago  time.Duration
}

// ParseSince reads "last", "Nh" (1 to 168) or "Nd" (1 to 7).
func ParseSince(s string) (Since, error) {
	bad := errors.New("--since must be last, 1h to 168h, or 1d to 7d")
	if s == "last" {
		return Since{Last: true}, nil
	}
	if len(s) < 2 {
		return Since{}, bad
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || strings.TrimLeft(s[:len(s)-1], "0123456789") != "" {
		return Since{}, bad
	}
	switch s[len(s)-1] {
	case 'h':
		if n >= 1 && n <= 168 {
			return Since{Ago: time.Duration(n) * time.Hour}, nil
		}
	case 'd':
		if n >= 1 && n <= 7 {
			return Since{Ago: time.Duration(n) * 24 * time.Hour}, nil
		}
	}
	return Since{}, bad
}

// ChangeKind says what changed for a ticket.
type ChangeKind string

const (
	Moved        ChangeKind = "moved"         // stage changed
	BecameReady  ChangeKind = "ready"         // a new stage in Ready
	NewlyBlocked ChangeKind = "blocked"       // standing became Blocked
	OutOfPolicy  ChangeKind = "out_of_policy" // standing became OutOfPolicy
	BackOnTrack  ChangeKind = "back_on_track" // left Blocked or OutOfPolicy
	Appeared     ChangeKind = "appeared"      // not in the earlier snapshot
)

// Change is one thing that happened to a ticket.
type Change struct {
	Key   string     `json:"key"`
	Title string     `json:"title"`
	Kind  ChangeKind `json:"kind"`
	Stage string     `json:"stage"`           // the ticket's stage now
	From  string     `json:"from,omitempty"`  // stages, for Moved
	To    string     `json:"to,omitempty"`    // for Moved and BecameReady
	Gates []string   `json:"gates,omitempty"` // for NewlyBlocked and OutOfPolicy
}

func unhealthy(s workfile.Standing) bool { return s == workfile.Blocked || s == workfile.OutOfPolicy }

// Diff compares the tickets in now against before. A ticket missing from now
// is not reported: a run may read only part of the scope. Changes are in key
// order, each ticket once, with its changes in the order of the kinds above;
// OrderByStages puts them in pipeline order.
func Diff(before, now Snapshot) []Change {
	var changes []Change
	for _, key := range workfile.SortedKeys(now.Entries) {
		cur := now.Entries[key]
		change := func(kind ChangeKind) Change {
			return Change{Key: cur.Key, Title: cur.Title, Kind: kind, Stage: cur.Stage}
		}
		prev, seen := before.Entries[key]
		if !seen {
			changes = append(changes, change(Appeared))
			continue
		}
		if prev.Stage != cur.Stage {
			c := change(Moved)
			c.From, c.To = prev.Stage, cur.Stage
			changes = append(changes, c)
		}
		for _, stage := range cur.Ready {
			if !slices.Contains(prev.Ready, stage) {
				c := change(BecameReady)
				c.To = stage
				changes = append(changes, c)
			}
		}
		if cur.Standing != prev.Standing {
			switch {
			case cur.Standing == workfile.Blocked:
				c := change(NewlyBlocked)
				c.Gates = slices.Clone(cur.Unmet)
				changes = append(changes, c)
			case cur.Standing == workfile.OutOfPolicy:
				c := change(OutOfPolicy)
				c.Gates = slices.Clone(cur.Unmet)
				changes = append(changes, c)
			case unhealthy(prev.Standing):
				changes = append(changes, change(BackOnTrack))
			}
		}
	}
	return changes
}

// OrderByStages sorts changes by the pipeline position of each ticket's stage,
// keeping key order within a stage. A stage not in the pipeline goes last.
func OrderByStages(changes []Change, stages []string) []Change {
	rank := func(c Change) int {
		if i := slices.Index(stages, c.Stage); i >= 0 {
			return i
		}
		return len(stages)
	}
	out := slices.Clone(changes)
	slices.SortStableFunc(out, func(a, b Change) int { return rank(a) - rank(b) })
	return out
}
