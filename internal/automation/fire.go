package automation

import (
	"encoding/json"
	"slices"
	"time"

	"workfile/internal/workfile"
)

// Event is one automation firing for one ticket, or one PR, and what became
// of it.
type Event struct {
	Automation string    `json:"automation"`
	Private    bool      `json:"private"`
	Mode       string    `json:"mode"`
	Trigger    string    `json:"trigger"`
	At         time.Time `json:"at"`

	Ticket    string `json:"ticket"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Stage     string `json:"stage"`
	To        string `json:"to,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Record    string `json:"record,omitempty"`
	RecordURL string `json:"record_url,omitempty"`

	// Report is the ticket's assessment when it fired, in the same format as
	// `wf status --json`. It is handed to the action in WORKFILE_EVENT.
	Report json.RawMessage `json:"report,omitempty"`

	// Outcome is what happened: observed, skipped, waiting, declined,
	// finished or failed. Detail says why or how.
	Outcome  string        `json:"outcome,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Log      string        `json:"log,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

const (
	Observed = "observed"
	Skipped  = "skipped"
	Waiting  = "waiting"
	Declined = "declined"
	Finished = "finished"
	Failed   = "failed"
)

// Subject names what the event is about: the PR when there is one.
func (e Event) Subject() string {
	if e.Record != "" {
		return e.Record
	}
	return e.Ticket
}

// MaxPerCheck is the most actions one check starts. The rest wait for the
// next check, so a flood of changes cannot start dozens of agents at once.
const MaxPerCheck = 10

// Fire compares the assessments with the facts saved in st and returns every
// automation that fired. It updates st's facts. An event that should not run
// comes back with its Outcome already set: skipped, or observed for an
// automation in observe mode. report turns an assessment into the JSON
// handed to actions. who is the person's identity, for scope: mine.
func Fire(w *workfile.Workspace, s *Set, st *State, all []workfile.Assessment, who map[string]string, now time.Time, report func(workfile.Assessment) json.RawMessage) []Event {
	var events []Event
	present := map[string]bool{}
	for _, entry := range s.Entries() {
		a := entry.Automation
		present[a.ID] = true
		if entry.File.Private && s.PrivateProblem != "" {
			continue // a private file that cannot run here does not even watch
		}
		saved := st.Facts[a.ID]
		if saved.Fingerprint != a.Fingerprint() {
			saved = FactSet{} // edited: a fresh starting point
		}
		// Facts are kept for every ticket, so work that becomes yours in the
		// same check as it changes, such as a ticket you pick up, still fires.
		facts, unread := w.TriggerFacts(a, all)
		next, fired := workfile.Changes(saved.Values, facts, unread)
		st.Facts[a.ID] = FactSet{Fingerprint: a.Fingerprint(), Values: next}
		for _, key := range fired {
			subject := workfile.ParseSubject(key)
			i := slices.IndexFunc(all, func(as workfile.Assessment) bool { return as.Ticket.Key == subject.Ticket })
			as := all[i]
			if !a.Everyone() && !as.Ticket.Involves(w.Policy.Tracker, who) {
				continue
			}
			e := Event{
				Automation: a.ID, Private: entry.File.Private, Mode: a.EffectiveMode(), Trigger: a.Trigger(), At: now,
				Ticket: as.Ticket.Key, Title: as.Ticket.Title, URL: as.Ticket.URL, Stage: as.Ticket.State, To: a.On.Ready,
			}
			var record *workfile.Record
			if subject.Record != "" {
				records := as.Ticket.Records[subject.Provider]
				if j := slices.IndexFunc(records, func(r workfile.Record) bool { return r.ID == subject.Record }); j >= 0 {
					record = &records[j]
					e.Provider, e.Record, e.RecordURL = subject.Provider, record.ID, record.URL
				}
			}
			if report != nil {
				e.Report = report(as)
			}
			switch reason := w.HoldsFor(a, as, record); {
			case reason != "":
				e.Outcome, e.Detail = Skipped, reason
			case entry.Blocked != "":
				e.Outcome, e.Detail = Skipped, "not run: "+entry.Blocked
			case e.Mode == workfile.ModeObserve:
				e.Outcome = Observed
			}
			events = append(events, e)
		}
	}
	// Forget automations that were removed, so re-adding one starts fresh.
	for id := range st.Facts {
		if !present[id] {
			delete(st.Facts, id)
		}
	}
	return events
}

// Questions expire after a day: a question about yesterday's change is
// usually no longer the right question.
const questionLife = 24 * time.Hour

// Next takes the events to handle in this check from the queue: up to
// MaxPerCheck actions. Questions are kept for a terminal to answer when ask
// is false. Expired questions come back with their outcome set.
func Next(st *State, now time.Time, ask bool) (take, expired []Event) {
	var keep []Event
	for _, e := range st.Queue {
		switch {
		case e.Mode == workfile.ModeAsk && now.Sub(e.At) > questionLife:
			e.Outcome, e.Detail = Skipped, "nobody answered within a day"
			expired = append(expired, e)
		case e.Mode == workfile.ModeAsk && !ask:
			keep = append(keep, e)
		case len(take) < MaxPerCheck:
			take = append(take, e)
		default:
			keep = append(keep, e)
		}
	}
	st.Queue = keep
	return take, expired
}
