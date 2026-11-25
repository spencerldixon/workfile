package workfile

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// RowKind is what an inbox row asks of the person. Every kind but RowInfo
// needs them; the action kinds match the prompt kinds in inbox settings, plus
// needs_work and cannot_check, which use the default prompt or none.
type RowKind string

const (
	RowOutOfPolicy      RowKind = "out_of_policy"
	RowChangesRequested RowKind = "changes_requested"
	RowChecksPassed     RowKind = "checks_passed"
	RowChecksFailed     RowKind = "checks_failed"
	RowNeedsWork        RowKind = "needs_work"
	RowReviewRequested  RowKind = "review_requested"
	RowComments         RowKind = "comments"
	RowReady            RowKind = "ready"
	RowCannotCheck      RowKind = "cannot_check"
	RowInfo             RowKind = "info"
)

// rowRank orders kinds by importance when one ticket has several.
var rowRank = []RowKind{RowOutOfPolicy, RowChangesRequested, RowChecksFailed, RowNeedsWork, RowReviewRequested, RowComments, RowReady, RowChecksPassed, RowCannotCheck, RowInfo}

// activityChecksPassed is made by the inbox itself, not read from a provider.
const activityChecksPassed ActivityKind = "checks_passed"

// InboxPRRelated reports whether the person authored the PR or has been asked
// to review it. The identity belongs to the connection that supplied the PR.
func InboxPRRelated(r Record, me string) bool {
	return me != "" && (strings.EqualFold(factText(&r, "author"), me) ||
		slices.ContainsFunc(factList(&r, "review_requests"), func(id string) bool { return strings.EqualFold(id, me) }))
}

// InboxWork is one ticket in the person's inbox. PRs are its related records
// by connection. Mine is false for someone else's
// ticket reached through a review request: it gets no policy rows. Err says
// why the ticket could not be read.
type InboxWork struct {
	Listing     Listing
	Assessment  *Assessment
	Next        NextActions
	ReadyReason string
	PRs         map[string][]Record
	Mine        bool
	Err         string
}

// InboxInput is everything one build needs. Loose holds PRs with no ticket,
// by connection. Who is the person's identity per connection, and Names turns
// an identity into a name to show.
type InboxInput struct {
	Repositories   map[string][]string
	Signals        []RowKind
	ReviewRequests *bool
	Work           []InboxWork
	Loose          map[string][]Record
	Activity       []Activity
	Who            map[string]string
	Names          map[string]string
	Now            time.Time
}

// InboxRow represents one signal. ID is the ticket key or the standalone PR ID.
// StateKey distinguishes signals on the same item. The fingerprint lets new
// activity or evidence bring back a resolved signal.
type InboxRow struct {
	ID, Fingerprint string
	Kind            RowKind
	Ticket, PR      string
	PRURL, URL      string
	ThreadURL       string
	signalID        string
	ChecksURL       string
	Title, Text     string
	ReadyReason     string
	Stage, To, Fix  string
	Who             string
	Count           int
	Since           time.Time
	Events          []Activity
	New             int
	// waitKey names this row's state, so a state that clears and returns
	// waits from when it returned.
	waitKey string
}

// Action reports whether the row needs the person.
func (r InboxRow) Action() bool { return r.Kind != RowInfo && r.Kind != RowChecksPassed }

// StateKey identifies the individual signal, including its provider record.
func (r InboxRow) StateKey() string {
	if r.signalID != "" {
		return r.signalID
	}
	return r.ID
}

func (r InboxRow) Snooze(until time.Time) Snooze {
	return Snooze{Until: until, Fingerprint: r.Fingerprint, Blocker: r.blocker()}
}

func (r InboxRow) blocker() string {
	switch r.Kind {
	case RowOutOfPolicy, RowChangesRequested, RowChecksFailed, RowNeedsWork:
		return r.waitKey
	default:
		return ""
	}
}

func (s Snooze) Hides(row InboxRow, now time.Time) bool {
	blocker := row.blocker()
	return s.Until.After(now) && (s.Fingerprint == row.Fingerprint || blocker == "" || blocker == s.Blocker)
}

type inboxItem struct {
	id, ticket, title, stage, url string
	work                          *InboxWork
	prs                           map[string][]Record
}

type candidate struct {
	row                InboxRow
	core               string // what the fingerprint is built from
	connection, record string
}

// BuildInbox turns current state and recent activity into newest-first rows.
func BuildInbox(in InboxInput, st InboxState) []InboxRow {
	var items []inboxItem
	for i := range in.Work {
		w := &in.Work[i]
		items = append(items, inboxItem{id: w.Listing.Key, ticket: w.Listing.Key, title: w.Listing.Title, stage: w.Listing.State, url: w.Listing.URL, work: w, prs: w.PRs})
	}
	for _, conn := range SortedKeys(in.Loose) {
		for _, r := range in.Loose[conn] {
			items = append(items, inboxItem{id: r.ID, title: r.Title, url: r.URL, prs: map[string][]Record{conn: {r}}})
		}
	}
	var rows []InboxRow
	for _, item := range items {
		rows = append(rows, buildRows(in, st, item)...)
	}
	SortInboxRows(rows)
	return rows
}

func (r InboxRow) Latest() time.Time {
	if n := len(r.Events); n > 0 && r.Events[n-1].At.After(r.Since) {
		return r.Events[n-1].At
	}
	return r.Since
}

func SortInboxRows(rows []InboxRow) {
	slices.SortStableFunc(rows, func(a, b InboxRow) int {
		if newer := b.Latest().Compare(a.Latest()); newer != 0 {
			return newer
		}
		return slices.Index(rowRank, a.Kind) - slices.Index(rowRank, b.Kind)
	})
}

func buildRows(in InboxInput, st InboxState, item inboxItem) []InboxRow {
	prIDs := map[[2]string]bool{}
	for conn, records := range item.prs {
		for _, r := range records {
			prIDs[[2]string{conn, r.ID}] = true
		}
	}
	// Unseen events on this item, not by me.
	var events []Activity
	for _, e := range in.Activity {
		if !(e.PR != "" && prIDs[[2]string{e.Connection, e.PR}]) && !(e.PR == "" && item.ticket != "" && e.Ticket == item.ticket) {
			continue
		}
		if me := in.Who[e.Connection]; me != "" && strings.EqualFold(me, e.WhoID) {
			continue
		}
		if e.Kind == ActivityReviewRequested && in.ReviewRequests != nil && !*in.ReviewRequests {
			continue
		}
		if _, seen := st.Seen[e.Key()]; seen {
			continue
		}
		events = append(events, e)
	}
	for _, conn := range SortedKeys(item.prs) {
		for _, r := range item.prs[conn] {
			if seen, ok := st.Checks[r.ID]; ok && !seen.Passed.IsZero() && factText(&r, "checks") == "success" {
				e := Activity{Connection: conn, PR: r.ID, Kind: activityChecksPassed, At: seen.Passed, URL: r.ChecksURL}
				if _, done := st.Seen[e.Key()]; !done {
					events = append(events, e)
				}
			}
		}
	}
	slices.SortStableFunc(events, func(a, b Activity) int { return a.At.Compare(b.At) })

	var cands []candidate
	add := func(kind RowKind, text, core, conn string, r *Record) {
		row := InboxRow{Kind: kind, Text: text}
		c := candidate{core: core, connection: conn}
		if r != nil {
			row.PR, row.PRURL = PRLabel(r.ID), r.URL
			row.ChecksURL = r.ChecksURL
			c.record = r.ID
		}
		c.row = row
		cands = append(cands, c)
	}
	covered := map[string]bool{} // event keys explained by an action row
	for _, conn := range SortedKeys(item.prs) {
		me := in.Who[conn]
		for i := range item.prs[conn] {
			r := &item.prs[conn][i]
			if factText(r, "state") != "open" {
				continue
			}
			mine := me != "" && strings.EqualFold(factText(r, "author"), me)
			label := PRLabel(r.ID)
			requests := factList(r, "review_requests")
			if changes := factList(r, "changes_requested"); mine && len(changes) > 0 && !allRequested(changes, requests) {
				who := in.name(changes[0])
				add(RowChangesRequested, "Address "+who+"'s review on "+label, r.Head+"|"+strings.Join(sorted(changes), ","), conn, r)
				row := &cands[len(cands)-1].row
				row.Who = who
			}
			watched := (InboxSettings{Repositories: in.Repositories}).WatchesRepository(conn, r.Repository)
			seen := st.Checks[r.ID]
			checks := factText(r, "checks")
			if checks == "" && seen.Head == r.Head {
				checks = seen.State
			}
			if (mine || watched) && checks == "failure" {
				core := r.Head
				if !seen.Failed.IsZero() {
					core += "|" + seen.Failed.Format(time.RFC3339Nano)
				}
				add(RowChecksFailed, "Inspect failing checks on "+label, core, conn, r)
			}
			if (in.ReviewRequests == nil || *in.ReviewRequests) && me != "" && slices.ContainsFunc(requests, func(id string) bool { return strings.EqualFold(id, me) }) {
				who := in.name(factText(r, "author"))
				if e := latest(events, conn, r.ID, ActivityReviewRequested); e != nil {
					who = in.who(*e)
				}
				add(RowReviewRequested, "Review "+label, r.Head+"|"+strings.Join(sorted(requests), ","), conn, r)
				cands[len(cands)-1].row.Who = who
			}
			if mine {
				var comments []Activity
				for _, e := range events {
					if e.Connection == conn && e.PR == r.ID && (e.Kind == ActivityComment || e.Kind == ActivityReview) {
						comments = append(comments, e)
						covered[e.Key()] = true
					}
				}
				if len(comments) > 0 {
					who := in.who(comments[0])
					text := "Read " + who + "'s comment on " + label
					if len(comments) > 1 {
						text = fmt.Sprintf("Read %d new comments on %s", len(comments), label)
					}
					add(RowComments, text, comments[len(comments)-1].Key(), conn, r)
					row := &cands[len(cands)-1].row
					row.Who, row.Count, row.Since = who, len(comments), comments[0].At
					row.Events = comments
					if len(comments) == 1 {
						row.ThreadURL = comments[0].URL
					}
				} else if r.UnresolvedReviewThreads {
					add(RowNeedsWork, ResolveReviewThreads+" on "+label, r.Head+"|review_threads", conn, r)
					cands[len(cands)-1].row.Fix = ResolveReviewThreads
				}
			}
		}
	}
	// A changes-requested or review-requested event is explained only when a
	// row for that state was built; otherwise it shows as news.
	for i := range cands {
		c := &cands[i]
		for _, e := range events {
			if e.Connection == c.connection && e.PR == c.record && (c.row.Kind == RowChangesRequested && e.Kind == ActivityChangesRequested || c.row.Kind == RowReviewRequested && e.Kind == ActivityReviewRequested) {
				covered[e.Key()] = true
				c.row.Events = append(c.row.Events, e)
				c.row.Since = e.At
			}
		}
	}
	if w := item.work; w != nil {
		if w.Err != "" {
			add(RowCannotCheck, "Couldn't check: "+w.Err, w.Err, "", nil)
		}
		if w.Mine && w.Assessment != nil {
			switch {
			case w.Assessment.Violation():
				add(RowOutOfPolicy, "Out of policy: "+w.Next.Summary, w.Next.Summary, "", nil)
				cands[len(cands)-1].row.Fix = w.Next.Summary
			case w.Next.State == ReadyToMove:
				to := w.Next.ReadyDestination()
				add(RowReady, "Move to "+to, to, "", nil)
				cands[len(cands)-1].row.To = to
				cands[len(cands)-1].row.ReadyReason = w.ReadyReason
			case w.Next.State == NeedsMe && len(cands) == 0:
				add(RowNeedsWork, w.Next.Summary, w.Next.Summary, "", nil)
				cands[len(cands)-1].row.Fix = w.Next.Summary
			}
		}
	}
	type activityGroup struct {
		connection, record string
		kind               RowKind
	}
	groups := map[activityGroup][]Activity{}
	var order []activityGroup
	readyFromCI := slices.ContainsFunc(cands, func(c candidate) bool { return c.row.Kind == RowReady && c.row.ReadyReason == "CI requirement met" })
	for _, e := range events {
		if covered[e.Key()] {
			continue
		}
		kind := RowInfo
		if e.Kind == activityChecksPassed {
			if readyFromCI && len(prIDs) == 1 && (in.Signals == nil || slices.Contains(in.Signals, RowReady)) {
				continue
			}
			kind = RowChecksPassed
		} else if e.Kind == ActivityPushed && len(cands) > 0 {
			continue
		}
		group := activityGroup{e.Connection, e.PR, kind}
		if _, exists := groups[group]; !exists {
			order = append(order, group)
		}
		groups[group] = append(groups[group], e)
	}
	for _, group := range order {
		activity := groups[group]
		event := activity[len(activity)-1]
		row := InboxRow{Kind: group.kind, Text: infoText(in, activity, event, item), Since: event.At, Events: activity}
		if event.PR != "" {
			row.PR = PRLabel(event.PR)
			for _, r := range item.prs[event.Connection] {
				if r.ID == event.PR {
					row.PRURL = r.URL
					break
				}
			}
		}
		cands = append(cands, candidate{row: row, core: event.Key(), connection: group.connection, record: group.record})
	}
	if in.Signals != nil {
		cands = slices.DeleteFunc(cands, func(c candidate) bool { return !slices.Contains(in.Signals, c.row.Kind) })
	}
	var rows []InboxRow
	for _, c := range cands {
		row := c.row
		row.ID, row.Ticket, row.Title, row.Stage, row.URL = item.id, item.ticket, item.title, item.stage, item.url
		row.signalID = strings.Join([]string{item.id, string(row.Kind), c.connection, c.record}, "|")
		newestKey := ""
		if n := len(row.Events); n > 0 {
			newestKey = row.Events[n-1].Key()
		}
		row.Fingerprint = string(row.Kind) + "|" + c.core + "|" + newestKey
		row.New = len(row.Events)
		row.waitKey = row.signalID + "|" + c.core
		if row.Since.IsZero() {
			row.Since = st.FirstSeen[row.waitKey]
			if row.Since.IsZero() {
				row.Since = st.FirstSeen[row.ID+"|"+string(row.Kind)+"|"+c.core]
			}
			if row.Since.IsZero() {
				row.Since = in.Now
			}
		}
		legacyDone := st.Done[row.ID]
		legacySnooze := st.Snoozed[row.ID]
		legacyFingerprint := string(row.Kind) + "|" + c.core + "|"
		if len(events) > 0 {
			legacyFingerprint += events[len(events)-1].Key()
		}
		legacyCleared := legacyDone != "" && (legacyDone == row.Fingerprint || legacyDone == legacyFingerprint)
		legacyHidden := legacySnooze.Until.After(in.Now) && (legacySnooze.Fingerprint == row.Fingerprint || legacySnooze.Fingerprint == legacyFingerprint)
		if st.Done[row.StateKey()] == row.Fingerprint || legacyCleared || legacyHidden || st.Snoozed[row.StateKey()].Hides(row, in.Now) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// Observe records what this build saw: when each row kind was first seen, and
// checks turning green. Call it after each poll; BuildInbox stays pure.
func (s *InboxState) Observe(in InboxInput, rows []InboxRow) {
	for _, r := range rows {
		key := r.waitKey
		if key == "" {
			continue
		}
		if _, ok := s.FirstSeen[key]; !ok {
			s.FirstSeen[key] = in.Now
		}
	}
	observe := func(records map[string][]Record) {
		for _, conn := range SortedKeys(records) {
			for _, r := range records[conn] {
				state, known := r.Facts["checks"].(string)
				if !known || r.Unavailable["checks"] != "" {
					continue
				}
				prev, had := s.Checks[r.ID]
				next := ChecksSeen{State: state, Head: r.Head, Passed: prev.Passed, Failed: prev.Failed}
				if state == "failure" && had && prev.State != "failure" {
					next.Failed = in.Now
				}
				if state != "success" {
					next.Passed = time.Time{}
				} else if had && prev.State != "success" {
					next.Passed = in.Now
				}
				s.Checks[r.ID] = next
			}
		}
	}
	for _, w := range in.Work {
		observe(w.PRs)
	}
	observe(in.Loose)
}

func infoText(in InboxInput, events []Activity, e Activity, item inboxItem) string {
	same := 0
	for _, o := range events {
		if o.Kind == e.Kind && o.PR == e.PR && o.Ticket == e.Ticket {
			same++
		}
	}
	where := item.ticket
	if e.PR != "" {
		where = PRLabel(e.PR)
	}
	return e.Summary(in.who(e), where, same)
}

// Summary describes the event at a display location, including repeated events.
func (e Activity) Summary(who, where string, count int) string {
	if who == "" {
		who = "Someone"
	}
	switch e.Kind {
	case activityChecksPassed:
		return "Checks passed on " + where
	case ActivityApproved:
		return fmt.Sprintf("%s approved %s", who, where)
	case ActivityChangesRequested:
		return fmt.Sprintf("%s requested changes on %s", who, where)
	case ActivityReviewRequested:
		return fmt.Sprintf("%s asked for a review on %s", who, where)
	case ActivityPushed:
		if count > 1 {
			return fmt.Sprintf("%s pushed %d commits to %s", who, count, where)
		}
		return fmt.Sprintf("%s pushed to %s", who, where)
	default:
		if count > 1 {
			return fmt.Sprintf("%d new comments on %s", count, where)
		}
		return fmt.Sprintf("%s commented on %s", who, where)
	}
}

func (in InboxInput) name(id string) string {
	if n := in.Names[id]; n != "" {
		return n
	}
	return id
}

func (in InboxInput) who(e Activity) string {
	if e.Who != "" {
		return e.Who
	}
	return in.name(e.WhoID)
}

func factText(r *Record, name string) string {
	if r.Unavailable[name] != "" {
		return ""
	}
	v, _ := r.Facts[name].(string)
	return v
}

func factList(r *Record, name string) []string {
	if r.Unavailable[name] != "" {
		return nil
	}
	v, _ := r.Facts[name].([]string)
	return v
}

func allRequested(reviewers, requests []string) bool {
	return !slices.ContainsFunc(reviewers, func(reviewer string) bool {
		return !slices.ContainsFunc(requests, func(r string) bool { return strings.EqualFold(r, reviewer) })
	})
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func latest(events []Activity, connection, pr string, kind ActivityKind) *Activity {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].PR == pr && events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}
