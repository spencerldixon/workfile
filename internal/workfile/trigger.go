package workfile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Workfile reads today's facts, not history, so a trigger is a change between
// two checks. Each trigger is worked out as one of these values per ticket, or
// per linked PR. Unknown never fires and never replaces a known value.
const (
	FactTrue    = "true"
	FactFalse   = "false"
	FactUnknown = "unknown"
)

// Subject is what one trigger fact is about: a ticket, or one of its PRs.
type Subject struct {
	Ticket   string
	Provider string
	Record   string
}

func (s Subject) Key() string {
	if s.Record == "" {
		return s.Ticket
	}
	return s.Ticket + " " + s.Provider + " " + s.Record
}

func ParseSubject(key string) Subject {
	parts := strings.SplitN(key, " ", 3)
	if len(parts) < 3 {
		return Subject{Ticket: parts[0]}
	}
	return Subject{Ticket: parts[0], Provider: parts[1], Record: parts[2]}
}

// Fingerprint changes when the trigger, stage, when or scope changes. Editing
// any of them starts the automation from a fresh starting point, so a fixed
// condition does not fire for every ticket at once.
func (a Automation) Fingerprint() string {
	data, _ := json.Marshal(struct {
		On          Trigger
		Stage       []string
		When, Scope string
	}{a.On, a.Stage, a.When, a.Scope})
	return fmt.Sprintf("%x", sha256.Sum256(data))[:16]
}

func truth(ok bool) string {
	if ok {
		return FactTrue
	}
	return FactFalse
}

// TriggerFacts works out the automation's trigger for each assessed ticket,
// and for a trigger about PRs, for each linked PR. unread lists tickets whose
// PRs could not be read, so their earlier PR facts are kept, not forgotten.
func (w *Workspace) TriggerFacts(a Automation, all []Assessment) (facts map[string]string, unread []string) {
	facts = map[string]string{}
	for _, as := range all {
		t := as.Ticket
		switch {
		case a.On.Ready != "":
			value := FactFalse
			if t.Error != "" {
				value = FactUnknown
			} else if i := slices.IndexFunc(as.Routes, func(r RouteAssessment) bool { return r.To == a.On.Ready }); i >= 0 {
				route := as.Routes[i]
				switch {
				case route.Available():
					value = FactTrue
				case !failed(route.Checks):
					value = FactUnknown
				}
			}
			facts[t.Key] = value
		case a.On.Enters != "":
			value := FactUnknown
			if slices.Contains(w.Policy.States, t.State) {
				value = truth(t.State == a.On.Enters)
			}
			facts[t.Key] = value
		case a.Condition != nil && a.Condition.Record():
			provider := a.Condition.Provider
			if t.Unavailable[provider+".prs"] != "" {
				unread = append(unread, t.Key)
				continue
			}
			for _, record := range t.Records[provider] {
				key := Subject{t.Key, provider, record.ID}.Key()
				facts[key] = w.factOf(*a.Condition, t, record)
			}
		case a.Condition != nil:
			facts[t.Key] = w.factOf(*a.Condition, t, Record{})
		}
	}
	return facts, unread
}

func (w *Workspace) factOf(e Expression, t Ticket, r Record) string {
	if w.unavailable(e, t, r) != "" {
		return FactUnknown
	}
	return truth(e.Evaluate(w.subject(e, t, r, nil)))
}

// Changes compares this check's facts with the saved ones. It returns the
// facts to save, and the subjects whose trigger turned from false to true.
//
//   - The first sighting of a subject only records it. Nothing fires for
//     something that was already true.
//   - Unknown is not a change: the saved value stays, so coming back from
//     unknown to the same value does not fire either.
//   - Each change fires once. It fires again only after turning false.
//   - A subject that is no longer in the search is forgotten, unless its
//     ticket's PRs could not be read.
func Changes(saved, now map[string]string, unread []string) (next map[string]string, fired []string) {
	next = map[string]string{}
	for key, value := range now {
		old, had := saved[key]
		if value == FactUnknown {
			if had {
				next[key] = old
			}
			continue
		}
		next[key] = value
		if had && old == FactFalse && value == FactTrue {
			fired = append(fired, key)
		}
	}
	for key, old := range saved {
		if _, ok := now[key]; !ok && slices.Contains(unread, ParseSubject(key).Ticket) {
			next[key] = old
		}
	}
	slices.Sort(fired)
	return next, fired
}

// HoldsFor checks an automation's stage and when for a trigger that just
// fired. It returns why the automation is skipped this time, or "" to go on.
// A when about PRs must hold for the PR that fired, or, for a trigger about
// the ticket, for every linked PR.
func (w *Workspace) HoldsFor(a Automation, as Assessment, record *Record) string {
	t := as.Ticket
	if len(a.Stage) > 0 && !slices.Contains(a.Stage, t.State) {
		return fmt.Sprintf("the ticket is in %s, and this automation is only for %s", t.State, strings.Join(a.Stage, " or "))
	}
	if a.Guard == nil {
		return ""
	}
	g := *a.Guard
	records := []Record{{}}
	if g.Record() {
		records = t.Records[g.Provider]
		if record != nil && a.Condition != nil && a.Condition.Provider == g.Provider {
			records = []Record{*record}
		}
	}
	for _, r := range records {
		if reason := w.unavailable(g, t, r); reason != "" {
			return "cannot check when: " + reason
		}
		if !g.Evaluate(w.subject(g, t, r, nil)) {
			if r.ID != "" {
				return fmt.Sprintf("when is not true for %s: %s", r.ID, strings.TrimSpace(a.When))
			}
			return "when is not true: " + strings.TrimSpace(a.When)
		}
	}
	return ""
}
