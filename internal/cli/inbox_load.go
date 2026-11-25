package cli

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

// firstLook is how far back the inbox reads on its first run.
const firstLook = 24 * time.Hour

// inboxData is what one poll read. A full poll reads the person's tickets and
// PRs; a quick poll reads activity and re-reads only what it touched.
type inboxData struct {
	work     []workfile.InboxWork
	tickets  map[string]workfile.Ticket   // batched policy evidence, before adding Inbox PRs
	inboxPRs map[string][]workfile.Record // last successful Inbox PR search
	loose    map[string][]workfile.Record
	who      map[string]string
	names    map[string]string
	events   []workfile.Activity // new this poll
	updated  map[string]time.Time
	warnings []string
	at       time.Time
	listing  []workfile.Listing // the board, from the last full poll
	// trackerRead is false when the tracker's activity could not be read, so
	// its cursor stays put and the next poll asks again.
	trackerRead bool
	settings    workfile.InboxSettings
}

type inboxLoadedMsg inboxData
type inboxTickMsg time.Time

func pollEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return inboxTickMsg(t) })
}

// pollMode is how much one poll reads. A quick poll reads activity, my PRs
// and review requests, and re-reads only tickets whose work changed. A
// refresh also re-reads every ticket of mine already known, to catch changes
// that leave no activity. A full poll also reads the board, to find tickets
// newly assigned to me.
type pollMode int

const (
	pollQuick pollMode = iota
	pollRefresh
	pollFull
)

// loadInbox reads one poll, starting from prev. With prev nil it reads
// everything. It never fails: what could not be read becomes a warning, and
// the last known rows stay.
func loadInbox(ctx context.Context, e env, st workfile.InboxState, prev *inboxData, now time.Time, mode pollMode) inboxData {
	return loadInboxWithListing(ctx, e, st, prev, now, mode, nil)
}

func loadInboxWithListing(ctx context.Context, e env, st workfile.InboxState, prev *inboxData, now time.Time, mode pollMode, listing []workfile.Listing) inboxData {
	d := inboxData{at: now, tickets: map[string]workfile.Ticket{}, updated: map[string]time.Time{}, settings: e.inboxSettings}
	warn := func(err error) { d.warnings = append(d.warnings, err.Error()) }
	if prev == nil {
		mode = pollFull
		d.who, d.names = inboxIdentities(ctx, e)
	} else {
		d.who, d.names, d.listing, d.inboxPRs = prev.who, prev.names, prev.listing, prev.inboxPRs
		for _, w := range prev.work {
			if w.Mine {
				d.work = append(d.work, w)
			}
		}
	}
	if mode == pollFull {
		var err error
		if listing == nil {
			listing, err = e.client.Listing(ctx, "")
		}
		if err != nil {
			warn(err)
		} else {
			d.listing = listing
			known := d.work
			d.work = nil
			me := d.who[e.w.Policy.Tracker]
			for _, t := range listing {
				if me == "" || t.AssigneeID != me || e.w.Policy.Transitions[t.State].End {
					continue
				}
				w := workfile.InboxWork{Listing: t, Mine: true}
				if i := slices.IndexFunc(known, func(k workfile.InboxWork) bool { return k.Listing.Key == t.Key }); i >= 0 {
					w = known[i]
					w.Listing = t
				}
				d.work = append(d.work, w)
			}
		}
	}
	if prev != nil {
		for _, w := range d.work {
			if ticket, ok := prev.tickets[w.Listing.Key]; ok {
				d.tickets[ticket.Key] = ticket
			}
		}
	}
	if mode >= pollRefresh {
		d.readWork(ctx, e, mineKeys(d.work), warn)
	}
	if prs, err := e.client.InboxPRs(ctx, e.inboxSettings); err != nil {
		warn(err)
	} else {
		d.inboxPRs = prs
	}
	tickets := d.placePRs()

	query := workfile.ActivityQuery{Tickets: mineKeys(d.work)}
	since := func(key string) time.Time {
		if at, ok := st.Cursors[key]; ok {
			return at
		}
		return now.Add(-firstLook)
	}
	for _, prs := range allPRs(d) {
		for _, r := range flatten(prs) {
			if prOpen(r) {
				query.PRs = append(query.PRs, workfile.PRCursor{ID: r.ID, Seen: since(r.ID)})
			}
		}
	}
	result, err := e.client.Activity(ctx, query, since("tracker"))
	d.trackerRead = err == nil || !strings.Contains(err.Error(), e.w.Policy.Tracker+":")
	if err != nil {
		warn(err)
	}
	d.events = result.Events
	if result.Updated != nil {
		d.updated = result.Updated
	}

	if mode == pollQuick && prev != nil {
		changed := func(w workfile.InboxWork) bool {
			for _, r := range flatten(w.PRs) {
				if previous, ok := st.Checks[r.ID]; ok && r.Unavailable["checks"] == "" && r.Facts["checks"] != previous.State {
					return true
				}
				if at, ok := d.updated[r.ID]; ok && at.After(since(r.ID)) {
					return true
				}
			}
			return slices.ContainsFunc(d.events, func(a workfile.Activity) bool { return touches(w, a) })
		}
		var touched []string
		for _, w := range d.work {
			if w.Mine && changed(w) {
				touched = append(touched, w.Listing.Key)
			}
		}
		if len(touched) > 0 {
			d.readWork(ctx, e, touched, warn)
			tickets = d.placePRs()
		}
	}
	d.assess(e.w, tickets)
	return d
}

func mineKeys(work []workfile.InboxWork) []string {
	var keys []string
	for _, w := range work {
		if w.Mine {
			keys = append(keys, w.Listing.Key)
		}
	}
	return keys
}

// readWork refreshes the batched ticket and PR evidence.
func (d *inboxData) readWork(ctx context.Context, e env, keys []string, warn func(error)) {
	if len(keys) == 0 {
		return
	}
	tickets, err := e.client.Tickets(ctx, keys, 0)
	if err != nil {
		warn(err)
		return
	}
	tickets, _, err = e.client.Records(ctx, tickets, false)
	if err != nil {
		warn(err)
		return
	}
	for i := range d.work {
		w := &d.work[i]
		key := w.Listing.Key
		if !slices.Contains(keys, key) {
			continue
		}
		at := slices.IndexFunc(tickets, func(t workfile.Ticket) bool { return t.Key == key })
		if at < 0 {
			delete(d.tickets, key)
			w.Err = "not visible to your account"
			continue
		}
		ticket := tickets[at]
		d.tickets[key], w.Err = ticket, ticket.Error
		if w.Listing.URL == "" {
			w.Listing.URL = ticket.URL
		}
	}
}

func (d *inboxData) assess(ws *workfile.Workspace, tickets map[string]workfile.Ticket) {
	for i := range d.work {
		w := &d.work[i]
		ticket, ok := tickets[w.Listing.Key]
		if !ok {
			w.Assessment, w.Next, w.ReadyReason = nil, workfile.NextActions{}, ""
			continue
		}
		a := ws.Assess(ticket)
		next := ws.NextActions(a, d.who)
		destination := next.ReadyDestination()
		switch {
		case destination == "" || w.Assessment == nil || w.Assessment.Ticket.State != a.Ticket.State:
			w.ReadyReason = ""
		case destination != w.Next.ReadyDestination():
			w.ReadyReason = inboxReadyReason(*w.Assessment, a, destination)
		}
		w.Assessment, w.Next = &a, next
	}
}

// inboxReadyReason describes a change only when the previously failing
// requirements for the move now pass on the same records.
func inboxReadyReason(before, after workfile.Assessment, destination string) string {
	blocked := slices.IndexFunc(before.Routes, func(r workfile.RouteAssessment) bool { return r.To == destination })
	ready := slices.IndexFunc(after.Routes, func(r workfile.RouteAssessment) bool { return r.To == destination })
	if blocked < 0 || ready < 0 || before.Routes[blocked].Available() || !after.Routes[ready].Available() || before.Violation() ||
		slices.ContainsFunc(before.Earlier, func(c workfile.Check) bool { return c.Outcome == "unknown" }) {
		return ""
	}
	reason := ""
	for _, check := range before.Routes[blocked].Checks {
		switch check.Outcome {
		case "unknown":
			return ""
		case "fail":
			cleared := ""
			switch check.Condition.Fact {
			case "approvals", "fresh_approvals":
				cleared = "Approval requirement met"
			case "checks":
				cleared = "CI requirement met"
			}
			if cleared == "" || reason != "" && reason != cleared || !inboxCheckCleared(check, after.Routes[ready].Checks) {
				return ""
			}
			reason = cleared
		}
	}
	return reason
}

func inboxCheckCleared(previous workfile.Check, current []workfile.Check) bool {
	i := slices.IndexFunc(current, func(c workfile.Check) bool { return c.ID == previous.ID && c.Outcome == "pass" })
	if i < 0 {
		return false
	}
	for _, result := range previous.Results {
		if result.Outcome == "unknown" {
			return false
		}
		if result.Outcome == "fail" && !slices.ContainsFunc(current[i].Results, func(r workfile.RuleResult) bool {
			return r.Record == result.Record && r.Provider == result.Provider && r.Outcome == "pass"
		}) {
			return false
		}
	}
	return true
}

// placePRs builds policy evidence for every linked ticket and assigns each PR
// to one Inbox item for activity.
func (d *inboxData) placePRs() map[string]workfile.Ticket {
	tickets := make(map[string]workfile.Ticket, len(d.tickets))
	for key, ticket := range d.tickets {
		records := make(map[string][]workfile.Record, len(ticket.Records))
		for conn, prs := range ticket.Records {
			records[conn] = slices.Clone(prs)
		}
		ticket.Records = records
		tickets[key] = ticket
	}

	followed := map[string][]workfile.Record{}
	for conn, records := range d.inboxPRs {
		for _, pr := range records {
			if !inboxPRRelated(d.settings, conn, pr, d.who[conn]) {
				continue
			}
			followed[conn] = append(followed[conn], pr)
			for key, ticket := range tickets {
				prs := ticket.Records[conn]
				if i := slices.IndexFunc(prs, func(r workfile.Record) bool { return r.ID == pr.ID }); i >= 0 {
					prs[i] = pr
				} else if slices.Contains(pr.TicketKeys, key) {
					ticket.Records[conn] = append(prs, pr)
				}
			}
		}
	}

	placed := map[[2]string]bool{}
	for i := range d.work {
		w := &d.work[i]
		w.PRs = map[string][]workfile.Record{}
		for conn, records := range tickets[w.Listing.Key].Records {
			for _, r := range records {
				id := [2]string{conn, r.ID}
				if !placed[id] && inboxPRRelated(d.settings, conn, r, d.who[conn]) {
					w.PRs[conn] = append(w.PRs[conn], r)
					placed[id] = true
				}
			}
		}
	}
	d.placeRemainingPRs(followed, placed)
	return tickets
}

func inboxPRRelated(settings workfile.InboxSettings, connection string, r workfile.Record, me string) bool {
	if settings.WatchesRepository(connection, r.Repository) {
		return true
	}
	if settings.ReviewRequests == nil || *settings.ReviewRequests {
		return workfile.InboxPRRelated(r, me)
	}
	author, _ := r.Facts["author"].(string)
	return me != "" && r.Unavailable["author"] == "" && strings.EqualFold(author, me)
}

// placeRemainingPRs finds a home for followed PRs without an assessed ticket.
func (d *inboxData) placeRemainingPRs(followed map[string][]workfile.Record, placed map[[2]string]bool) {
	d.loose = map[string][]workfile.Record{}
	for _, conn := range workfile.SortedKeys(followed) {
		for _, r := range followed[conn] {
			id := [2]string{conn, r.ID}
			if placed[id] {
				continue
			}
			placed[id] = true
			at := slices.IndexFunc(d.work, func(w workfile.InboxWork) bool { return slices.Contains(r.TicketKeys, w.Listing.Key) })
			switch {
			case at >= 0:
				d.work[at].PRs[conn] = append(d.work[at].PRs[conn], r)
			default:
				i := slices.IndexFunc(d.listing, func(t workfile.Listing) bool { return slices.Contains(r.TicketKeys, t.Key) })
				if i < 0 {
					d.loose[conn] = append(d.loose[conn], r)
				} else {
					d.work = append(d.work, workfile.InboxWork{Listing: d.listing[i], PRs: map[string][]workfile.Record{conn: {r}}})
				}
			}
		}
	}
}

// inboxIdentities finds who I am on each connection, and a name for everyone
// in people.yml.
func inboxIdentities(ctx context.Context, e env) (who, names map[string]string) {
	me, _ := trackerIdentity(ctx, e)
	who = readActionIdentities(ctx, e, me).who
	names = map[string]string{}
	for person, ids := range e.w.People {
		name := ids["name"]
		if name == "" {
			name = person
		}
		for field, id := range ids {
			if field != "name" && id != "" {
				names[id] = name
			}
		}
	}
	return who, names
}

// inboxInput combines a poll with what the person has not seen yet.
func inboxInput(d inboxData, st workfile.InboxState) workfile.InboxInput {
	events := slices.Clone(st.Events)
	for _, e := range d.events {
		if !slices.ContainsFunc(events, func(o workfile.Activity) bool { return o.Key() == e.Key() }) {
			events = append(events, e)
		}
	}
	return workfile.InboxInput{Work: d.work, Loose: d.loose, Activity: events, Who: d.who, Names: d.names, Now: d.at, Signals: d.settings.Signals, ReviewRequests: d.settings.ReviewRequests, Repositories: d.settings.Repositories}
}

// inboxRows builds the rows, plus one ! row for each connection that could
// not be read.
func inboxRows(d inboxData, st workfile.InboxState) []workfile.InboxRow {
	rows := workfile.BuildInbox(inboxInput(d, st), st)
	for _, w := range d.warnings {
		for _, line := range strings.Split(w, "\n") {
			id := "!" + line
			row := workfile.InboxRow{ID: id, Fingerprint: line, Kind: workfile.RowCannotCheck, Text: "Couldn't read " + line, Since: d.at}
			if st.Done[id] != line && !st.Snoozed[id].Hides(row, d.at) {
				rows = append(rows, row)
			}
		}
	}
	if len(d.warnings) > 0 {
		workfile.SortInboxRows(rows)
	}
	return rows
}

// applyPoll moves the cursors on and keeps the new events until seen.
func applyPoll(st *workfile.InboxState, d inboxData) {
	st.AddEvents(d.events)
	for id, at := range d.updated {
		st.Cursors[id] = at
	}
	if d.trackerRead {
		st.Cursors["tracker"] = d.at
	}
}

// tracksActivity keeps pending activity while its ticket or PR is in scope.
func (d inboxData) tracksActivity(a workfile.Activity) bool {
	if me := d.who[a.Connection]; me != "" && strings.EqualFold(me, a.WhoID) {
		return false
	}
	if a.PR != "" && slices.ContainsFunc(d.loose[a.Connection], func(r workfile.Record) bool { return r.ID == a.PR }) {
		return true
	}
	return slices.ContainsFunc(d.work, func(w workfile.InboxWork) bool { return touches(w, a) })
}

func allPRs(d inboxData) []map[string][]workfile.Record {
	all := []map[string][]workfile.Record{d.loose}
	for _, w := range d.work {
		all = append(all, w.PRs)
	}
	var flat []map[string][]workfile.Record
	for _, m := range all {
		if len(m) > 0 {
			flat = append(flat, m)
		}
	}
	return flat
}

func flatten(m map[string][]workfile.Record) []workfile.Record {
	var out []workfile.Record
	for _, conn := range workfile.SortedKeys(m) {
		out = append(out, m[conn]...)
	}
	return out
}

func prOpen(r workfile.Record) bool {
	state, _ := r.Facts["state"].(string)
	return state == "open"
}

func hasPR(w workfile.InboxWork, conn, id string) bool {
	return slices.ContainsFunc(w.PRs[conn], func(r workfile.Record) bool { return r.ID == id })
}

func touches(w workfile.InboxWork, a workfile.Activity) bool {
	return (a.PR == "" && a.Ticket == w.Listing.Key) || (a.PR != "" && hasPR(w, a.Connection, a.PR))
}
