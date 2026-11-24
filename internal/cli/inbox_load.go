package cli

import (
	"context"
	"maps"
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
	d := inboxData{at: now, updated: map[string]time.Time{}}
	warn := func(err error) { d.warnings = append(d.warnings, err.Error()) }
	if prev == nil {
		mode = pollFull
		d.who, d.names = inboxIdentities(ctx, e)
	} else {
		d.who, d.names, d.listing, d.loose = prev.who, prev.names, prev.listing, prev.loose
		for _, w := range prev.work {
			if w.Mine {
				w.PRs = maps.Clone(w.PRs)
				d.work = append(d.work, w)
			}
		}
	}
	if mode == pollFull {
		if listing, err := e.client.Listing(ctx, ""); err != nil {
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
	if mode >= pollRefresh {
		readWork(ctx, e, d.who, d.work, mineKeys(d.work), warn)
	}
	if mine, err := e.client.MyPRs(ctx); err != nil {
		warn(err)
		if prev != nil {
			for _, w := range prev.work {
				if !w.Mine {
					d.work = append(d.work, w)
				}
			}
		}
	} else {
		d.work, d.loose = placePRs(e, d.who, d.listing, d.work, mine)
	}

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
		readWork(ctx, e, d.who, d.work, touched, warn)
	}
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

// readWork reads the named tickets with their PRs in one tracker read and one
// code host read, then checks each against policy.
func readWork(ctx context.Context, e env, who map[string]string, work []workfile.InboxWork, keys []string, warn func(error)) {
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
	}
	for i := range work {
		if !slices.Contains(keys, work[i].Listing.Key) {
			continue
		}
		at := slices.IndexFunc(tickets, func(t workfile.Ticket) bool { return t.Key == work[i].Listing.Key })
		if at < 0 {
			work[i].Err = "not visible to your account"
			continue
		}
		if work[i].Listing.URL == "" {
			work[i].Listing.URL = tickets[at].URL
		}
		a := e.w.Assess(tickets[at])
		w := &work[i]
		w.Assessment, w.Next, w.Err = &a, e.w.NextActions(a, who), tickets[at].Error
		w.PRs = map[string][]workfile.Record{}
		for conn, records := range tickets[at].Records {
			for _, r := range records {
				if workfile.InboxPRRelated(r, who[conn]) {
					w.PRs[conn] = append(w.PRs[conn], r)
				}
			}
		}
	}
}

// placePRs puts each of my PRs under its ticket. A PR on someone else's
// ticket adds that ticket, not checked against policy. A PR naming no ticket
// is loose.
func placePRs(e env, who map[string]string, listing []workfile.Listing, work []workfile.InboxWork, mine map[string][]workfile.Record) ([]workfile.InboxWork, map[string][]workfile.Record) {
	loose := map[string][]workfile.Record{}
	keys := e.w.TrackerKeys()
	for _, conn := range workfile.SortedKeys(mine) {
		for _, r := range mine[conn] {
			if !workfile.InboxPRRelated(r, who[conn]) || slices.ContainsFunc(work, func(w workfile.InboxWork) bool {
				return hasPR(w, conn, r.ID)
			}) {
				continue
			}
			var key string
			if keys != nil {
				if found := keys.Mentions(r.Title); len(found) > 0 {
					key = found[0]
				}
			}
			at := slices.IndexFunc(work, func(w workfile.InboxWork) bool { return key != "" && w.Listing.Key == key })
			switch {
			case at >= 0:
				if work[at].PRs == nil {
					work[at].PRs = map[string][]workfile.Record{}
				}
				work[at].PRs[conn] = append(slices.Clone(work[at].PRs[conn]), r)
			case key != "":
				l := workfile.Listing{Key: key}
				if i := slices.IndexFunc(listing, func(t workfile.Listing) bool { return t.Key == key }); i >= 0 {
					l = listing[i]
				}
				work = append(work, workfile.InboxWork{Listing: l, PRs: map[string][]workfile.Record{conn: {r}}})
			default:
				loose[conn] = append(loose[conn], r)
			}
		}
	}
	return work, loose
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
	return workfile.InboxInput{Work: d.work, Loose: d.loose, Activity: events, Who: d.who, Names: d.names, Now: d.at}
}

// inboxRows builds the rows, plus one ! row for each connection that could
// not be read.
func inboxRows(d inboxData, st workfile.InboxState) []workfile.InboxRow {
	rows := workfile.BuildInbox(inboxInput(d, st), st)
	for _, w := range d.warnings {
		for _, line := range strings.Split(w, "\n") {
			id := "!" + line
			if st.Done[id] == line {
				continue
			}
			rows = append(rows, workfile.InboxRow{ID: id, Fingerprint: line, Kind: workfile.RowCannotCheck, Text: "Couldn't read " + line, Since: d.at})
		}
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
