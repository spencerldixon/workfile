package cli

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

var loadNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func inboxEnv(t *testing.T, f *fakeSource) env {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	return env{view: view{out: io.Discard, width: 100, tint: &tintCache{}}, w: w, client: f, errOut: io.Discard}
}

func inboxSourceFixture() *fakeSource {
	pr := workfile.Record{ID: "app#84", URL: "https://github.example.com/example-team/app/pull/84", Title: "APP-1 fix", Head: "h1",
		Facts: workfile.Facts{"state": "open", "author": "example-bob", "checks": "failure", "review_requests": []string{}, "changes_requested": []string{}}}
	return &fakeSource{
		listing: []workfile.Listing{
			{Key: "APP-1", Title: "Fix login", State: "review", AssigneeID: "example-bob"},
			{Key: "APP-2", Title: "Shipped", State: "done", AssigneeID: "example-bob"},
			{Key: "APP-3", Title: "Not mine", State: "review", AssigneeID: "example-alice"},
		},
		tickets: []workfile.Ticket{fixtureTicket("APP-1", "review", "short"), fixtureTicket("APP-2", "done", "short")},
		records: map[string]map[string][]workfile.Record{"APP-1": {"github": {pr}}},
	}
}

func TestLoadInboxOnlyInFlight(t *testing.T) {
	f := inboxSourceFixture()
	d := loadInbox(context.Background(), inboxEnv(t, f), workfile.NewInboxState(), nil, loadNow, pollFull)
	if !slices.Equal(f.keys, []string{"APP-1"}) {
		t.Fatalf("only my in-flight tickets are read, got %v", f.keys)
	}
	if len(f.activityQueries) != 1 || !slices.Equal(f.activityQueries[0].Tickets, []string{"APP-1"}) {
		t.Fatalf("activity asked for the wrong tickets: %+v", f.activityQueries)
	}
	if prs := f.activityQueries[0].PRs; len(prs) != 1 || prs[0].ID != "app#84" {
		t.Fatalf("activity asked for the wrong PRs: %+v", prs)
	}
	rows := inboxRows(d, workfile.NewInboxState())
	if len(rows) != 2 || slices.ContainsFunc(rows, func(row workfile.InboxRow) bool { return row.ID != "APP-1" || !row.Action() }) {
		t.Fatalf("want only action rows for APP-1: %+v", rows)
	}
}

func TestLoadInboxLinksDiscoveredPRToTicketAcrossPolls(t *testing.T) {
	f := inboxSourceFixture()
	pr := inboxPRRecord("app#85", "example-bob", workfile.Facts{"checks": "failure"})
	pr.Title = "Fix validation"
	pr.TicketKeys = []string{"APP-1"}
	pr.UnresolvedReviewThreads = true
	f.myPRs = map[string][]workfile.Record{"github": {pr}}
	e := inboxEnv(t, f)
	st := workfile.NewInboxState()

	assertLinked := func(d inboxData) {
		t.Helper()
		rows := inboxRows(d, st)
		if len(d.loose["github"]) != 0 || !slices.ContainsFunc(rows, func(row workfile.InboxRow) bool { return row.ID == "APP-1" && row.PR == "#85" }) {
			t.Fatalf("want a ticket row with the discovered PR: %+v", rows)
		}
		if !slices.ContainsFunc(d.work[0].PRs["github"], func(r workfile.Record) bool {
			return r.ID == pr.ID && r.UnresolvedReviewThreads && r.Facts["checks"] == "failure"
		}) {
			t.Fatalf("missing linked PR with its review and CI state: %+v", d.work[0].PRs)
		}
	}
	first := loadInbox(context.Background(), e, st, nil, loadNow, pollFull)
	assertLinked(first)

	f.updated = map[string]time.Time{pr.ID: loadNow}
	f.keys = nil
	quick := loadInbox(context.Background(), e, st, &first, loadNow.Add(time.Minute), pollQuick)
	if !slices.Equal(f.keys, []string{"APP-1"}) {
		t.Fatalf("want the ticket re-read after its PR changed, got %v", f.keys)
	}
	assertLinked(quick)
}

func TestLoadInboxAssessesPRsFromBothReads(t *testing.T) {
	f := inboxSourceFixture()
	f.tickets[0].Facts["description"] = "Update account settings validation so an empty display name produces a clear message and keeps the previous value."
	prFacts := workfile.Facts{
		"labels":    []string{"low-risk"},
		"approvals": []workfile.Actor{{Value: "example-alice"}},
	}
	other := inboxPRRecord("app#84", "example-charlie", prFacts)
	discovered := inboxPRRecord("app#85", "example-bob", prFacts)
	discovered.TicketKeys = []string{"APP-1"}
	discovered.Facts["checks"] = "failure"
	f.records["APP-1"]["github"] = []workfile.Record{other}
	f.myPRs = map[string][]workfile.Record{"github": {discovered}}
	e := inboxEnv(t, f)
	st := workfile.NewInboxState()

	assertVerdict := func(d inboxData, allowed bool, ids ...string) {
		t.Helper()
		a := d.work[0].Assessment
		var got []string
		for _, pr := range a.Ticket.Records["github"] {
			got = append(got, pr.ID)
		}
		if !slices.Equal(got, ids) {
			t.Fatalf("policy evidence = %v, want %v", got, ids)
		}
		v := judgeMove(*a, "done")
		if v.allowed != allowed {
			t.Fatalf("move allowed = %v, want %v: %+v", v.allowed, allowed, v.unmet)
		}
		if !allowed && !slices.ContainsFunc(v.unmet, func(reason workfile.Reason) bool {
			return reason.Provider == "github" && reason.Record == discovered.ID
		}) {
			t.Fatalf("want the Inbox PR reported as a blocker: %+v", v.unmet)
		}
		if ready := d.work[0].Next.State == workfile.ReadyToMove; ready != allowed {
			t.Fatalf("Inbox ready = %v, want %v: %+v", ready, allowed, d.work[0].Next)
		}
	}
	full := loadInbox(context.Background(), e, st, nil, loadNow, pollFull)
	assertVerdict(full, false, other.ID, discovered.ID)

	passed := inboxPRRecord(discovered.ID, "example-bob", prFacts)
	passed.TicketKeys = discovered.TicketKeys
	f.myPRs["github"] = []workfile.Record{passed}
	f.keys = nil
	quick := loadInbox(context.Background(), e, st, &full, loadNow.Add(time.Minute), pollQuick)
	assertVerdict(quick, true, other.ID, discovered.ID)
	if got := quick.work[0].ReadyReason; got != "CI requirement met" {
		t.Fatalf("ready reason = %q, want the cleared CI requirement", got)
	}
	if len(f.keys) != 0 {
		t.Fatalf("updated Inbox evidence should need no ticket read, got %v", f.keys)
	}

	f.records["APP-1"] = map[string][]workfile.Record{"github": {other, discovered}}
	refresh := loadInbox(context.Background(), e, st, &quick, loadNow.Add(2*time.Minute), pollRefresh)
	assertVerdict(refresh, true, other.ID, discovered.ID)
	if got := refresh.work[0].ReadyReason; got != "CI requirement met" {
		t.Fatalf("quiet refresh lost the ready reason: %q", got)
	}

	f.myPRs["github"] = nil
	quick = loadInbox(context.Background(), e, st, &full, loadNow.Add(3*time.Minute), pollQuick)
	assertVerdict(quick, true, other.ID)
	if got := quick.work[0].ReadyReason; got != "" {
		t.Fatalf("a PR disappearing is not a passing check: %q", got)
	}
}

func TestLoadInboxExplainsReadyAfterRequiredApproval(t *testing.T) {
	f := inboxSourceFixture()
	f.tickets[0].Facts["description"] = "Update account settings validation so an empty display name produces a clear message and keeps the previous value."
	pr := inboxPRRecord("app#84", "example-bob", workfile.Facts{
		"labels": []string{"low-risk"}, "approvals": []workfile.Actor{},
	})
	f.records["APP-1"]["github"] = []workfile.Record{pr}
	e := inboxEnv(t, f)
	st := workfile.NewInboxState()
	blocked := loadInbox(context.Background(), e, st, nil, loadNow, pollFull)

	approved := pr
	approved.Facts = maps.Clone(pr.Facts)
	approved.Facts["approvals"] = []workfile.Actor{{Value: "example-alice"}}
	f.myPRs = map[string][]workfile.Record{"github": {approved}}
	ready := loadInbox(context.Background(), e, st, &blocked, loadNow.Add(time.Minute), pollQuick)
	rows := inboxRows(ready, st)
	if len(rows) != 1 || rows[0].Kind != workfile.RowReady || rows[0].ReadyReason != "Approval requirement met" {
		t.Fatalf("want one ready alert with the approval reason: %+v", rows)
	}

	d := driveInbox(t, workfile.InboxSettings{})
	d.send(inboxLoadedMsg(ready))
	d.selectID("APP-1")
	d.press("enter")
	if screen := inboxScreen(d); !strings.Contains(screen, "Approval requirement met") || !strings.Contains(screen, "Move to done") {
		t.Fatalf("ready preview is missing its reason or next step:\n%s", screen)
	}
}

func TestLoadInboxSharedPRBlocksBothTicketsWithOneActivityAlert(t *testing.T) {
	f := inboxSourceFixture()
	description := "Update account settings validation so an empty display name produces a clear message and keeps the previous value."
	f.listing[1] = workfile.Listing{Key: "APP-2", State: "review", AssigneeID: "example-bob"}
	f.tickets = []workfile.Ticket{
		fixtureTicket("APP-1", "review", description),
		fixtureTicket("APP-2", "review", description),
	}
	passing := inboxPRRecord("app#85", "example-bob", workfile.Facts{
		"labels":    []string{"low-risk"},
		"approvals": []workfile.Actor{{Value: "example-alice"}},
	})
	failing := passing
	failing.TicketKeys = []string{"APP-1", "APP-2"}
	failing.Facts = maps.Clone(passing.Facts)
	failing.Facts["checks"] = "failure"
	f.records = nil
	f.myPRs = map[string][]workfile.Record{"github": {failing}}
	f.activity = []workfile.Activity{
		{Connection: "github", PR: failing.ID, Kind: workfile.ActivityComment, WhoID: "example-alice", At: loadNow},
	}
	e := inboxEnv(t, f)
	st := workfile.NewInboxState()

	assertPoll := func(d inboxData, query workfile.ActivityQuery) {
		t.Helper()
		if len(d.work) != 2 {
			t.Fatalf("want two linked tickets, got %d", len(d.work))
		}
		for _, w := range d.work {
			v := judgeMove(*w.Assessment, "done")
			if v.allowed || len(v.unmet) != 1 || v.unmet[0].Record != failing.ID {
				t.Errorf("%s should be blocked by %s: %+v", w.Listing.Key, failing.ID, v)
			}
		}
		if len(query.PRs) != 1 || query.PRs[0].ID != failing.ID {
			t.Errorf("want one activity cursor for the shared PR: %+v", query.PRs)
		}
		events := 0
		for _, row := range inboxRows(d, st) {
			events += len(row.Events)
		}
		if events != 1 {
			t.Errorf("want one shared PR comment in Inbox, got %d", events)
		}
	}

	full := loadInbox(context.Background(), e, st, nil, loadNow, pollFull)
	assertPoll(full, f.activityQueries[0])

	f.records = map[string]map[string][]workfile.Record{
		"APP-1": {"github": {passing}},
		"APP-2": {"github": {passing}},
	}
	refresh := loadInbox(context.Background(), e, st, &full, loadNow.Add(time.Minute), pollRefresh)
	assertPoll(refresh, f.activityQueries[1])
}

func TestLoadInboxOnlyIncludesRelatedPRs(t *testing.T) {
	f := inboxSourceFixture()
	unrelated := inboxPRRecord("app#85", "example-charlie", nil)
	review := inboxPRRecord("app#86", "example-alice", workfile.Facts{"review_requests": []string{"EXAMPLE-BOB"}})
	f.records["APP-1"]["github"] = append(f.records["APP-1"]["github"], unrelated, review)
	f.myPRs = map[string][]workfile.Record{"github": {unrelated, review}}
	f.activity = []workfile.Activity{
		{Connection: "github", PR: unrelated.ID, Kind: workfile.ActivityApproved, WhoID: "example-alice", At: loadNow},
		{Connection: "other", PR: review.ID, Kind: workfile.ActivityComment, WhoID: "example-charlie", At: loadNow},
		{Connection: "github", PR: review.ID, Kind: workfile.ActivityReviewRequested, WhoID: "example-alice", At: loadNow},
	}

	st := workfile.NewInboxState()
	d := loadInbox(context.Background(), inboxEnv(t, f), st, nil, loadNow, pollFull)
	prs := f.activityQueries[0].PRs
	if len(prs) != 2 || prs[0].ID != "app#84" || prs[1].ID != review.ID {
		t.Fatalf("activity must only poll authored and review-requested PRs: %+v", prs)
	}
	rows := inboxRows(d, st)
	var activity []workfile.Activity
	for _, row := range rows {
		activity = append(activity, row.Events...)
	}
	if !slices.Equal(activity, []workfile.Activity{f.activity[2]}) {
		t.Fatalf("want only activity from the related PR on the configured connection: %+v", rows)
	}
}

func TestLoadInboxFirstRunUses24h(t *testing.T) {
	f := inboxSourceFixture()
	loadInbox(context.Background(), inboxEnv(t, f), workfile.NewInboxState(), nil, loadNow, pollFull)
	day := loadNow.Add(-24 * time.Hour)
	if !f.activitySince[0].Equal(day) || !f.activityQueries[0].PRs[0].Seen.Equal(day) {
		t.Fatalf("first run reads the last 24h: since %s, PR cursor %s", f.activitySince[0], f.activityQueries[0].PRs[0].Seen)
	}
	st := workfile.NewInboxState()
	st.Cursors["tracker"] = loadNow.Add(-time.Hour)
	st.Cursors["app#84"] = loadNow.Add(-2 * time.Hour)
	loadInbox(context.Background(), inboxEnv(t, f), st, nil, loadNow, pollFull)
	if !f.activitySince[1].Equal(loadNow.Add(-time.Hour)) || !f.activityQueries[1].PRs[0].Seen.Equal(loadNow.Add(-2*time.Hour)) {
		t.Fatal("later runs read from the stored cursors")
	}
}

func TestLoadInboxProviderDown(t *testing.T) {
	f := inboxSourceFixture()
	f.activityErr = errors.New("github: provider request failed")
	d := loadInbox(context.Background(), inboxEnv(t, f), workfile.NewInboxState(), nil, loadNow, pollFull)
	rows := inboxRows(d, workfile.NewInboxState())
	var cannot *workfile.InboxRow
	for i := range rows {
		if rows[i].Kind == workfile.RowCannotCheck {
			cannot = &rows[i]
		}
	}
	if cannot == nil || !strings.Contains(cannot.Text, "github") {
		t.Fatalf("a failing provider shows one ! row naming it: %+v", rows)
	}
	if len(rows) != 3 {
		t.Fatalf("other rows still built: %+v", rows)
	}
}

func TestLoadInboxQuickPollRereadsOnlyTouched(t *testing.T) {
	f := inboxSourceFixture()
	e := inboxEnv(t, f)
	first := loadInbox(context.Background(), e, workfile.NewInboxState(), nil, loadNow, pollFull)
	f.keys = nil
	loadInbox(context.Background(), e, workfile.NewInboxState(), &first, loadNow.Add(2*time.Minute), pollQuick)
	if f.keys != nil || f.listingReads != 1 {
		t.Fatalf("a quiet poll re-reads nothing but activity: tickets %v, listings %d", f.keys, f.listingReads)
	}
	f.activity = []workfile.Activity{{Connection: "github", PR: "app#84", Kind: workfile.ActivityComment, WhoID: "example-alice", At: loadNow}}
	loadInbox(context.Background(), e, workfile.NewInboxState(), &first, loadNow.Add(4*time.Minute), pollQuick)
	if !slices.Equal(f.keys, []string{"APP-1"}) {
		t.Fatalf("a ticket with new activity is re-read, got %v", f.keys)
	}
}

func TestApplyPollAdvancesCursors(t *testing.T) {
	st := workfile.NewInboxState()
	ev := workfile.Activity{Connection: "github", PR: "app#84", Kind: workfile.ActivityComment, WhoID: "example-alice", At: loadNow}
	d := inboxData{at: loadNow, trackerRead: true, events: []workfile.Activity{ev, ev}, updated: map[string]time.Time{"app#84": loadNow.Add(-time.Minute)}}
	applyPoll(&st, d)
	if !st.Cursors["tracker"].Equal(loadNow) || !st.Cursors["app#84"].Equal(loadNow.Add(-time.Minute)) {
		t.Fatalf("cursors not advanced: %v", st.Cursors)
	}
	if len(st.Events) != 1 {
		t.Fatalf("events kept once each: %+v", st.Events)
	}
	failed := workfile.NewInboxState()
	applyPoll(&failed, inboxData{at: loadNow})
	if _, moved := failed.Cursors["tracker"]; moved {
		t.Fatal("a failed tracker read must not move its cursor")
	}
}

func TestLoadInboxFullFailureKeepsRows(t *testing.T) {
	f := inboxSourceFixture()
	f.myPRs = map[string][]workfile.Record{"github": {inboxPRRecord("api#7", "example-alice", workfile.Facts{"review_requests": []string{"example-bob"}})}}
	e := inboxEnv(t, f)
	first := loadInbox(context.Background(), e, workfile.NewInboxState(), nil, loadNow, pollFull)
	f.listingErr = errors.New("jira: provider request failed")
	f.myPRsErr = errors.New("github: provider request failed")
	d := loadInbox(context.Background(), e, workfile.NewInboxState(), &first, loadNow.Add(10*time.Minute), pollFull)
	rows := inboxRows(d, workfile.NewInboxState())
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if !slices.Contains(ids, "APP-1") || !slices.Contains(ids, "api#7") {
		t.Fatalf("a failed read keeps the last known rows: %v", ids)
	}
}

func TestLoadInboxQuickPollFindsReviewRequests(t *testing.T) {
	f := inboxSourceFixture()
	e := inboxEnv(t, f)
	first := loadInbox(context.Background(), e, workfile.NewInboxState(), nil, loadNow, pollFull)
	f.myPRs = map[string][]workfile.Record{"github": {inboxPRRecord("api#7", "example-alice", workfile.Facts{"review_requests": []string{"example-bob"}})}}
	d := loadInbox(context.Background(), e, workfile.NewInboxState(), &first, loadNow.Add(2*time.Minute), pollQuick)
	if f.myPRsReads != 2 {
		t.Fatalf("every poll looks for review requests, got %d reads", f.myPRsReads)
	}
	if len(d.loose["github"]) != 1 {
		t.Fatalf("a new review request shows on the next quick poll: %+v", d.loose)
	}
}

func TestLoadInboxQuickRereadsChangedPR(t *testing.T) {
	f := inboxSourceFixture()
	e := inboxEnv(t, f)
	first := loadInbox(context.Background(), e, workfile.NewInboxState(), nil, loadNow, pollFull)
	st := workfile.NewInboxState()
	st.Cursors["app#84"] = loadNow.Add(-time.Hour)
	f.updated = map[string]time.Time{"app#84": loadNow}
	f.keys = nil
	loadInbox(context.Background(), e, st, &first, loadNow.Add(2*time.Minute), pollQuick)
	if !slices.Equal(f.keys, []string{"APP-1"}) {
		t.Fatalf("a ticket whose PR changed is re-read, even with no new event: %v", f.keys)
	}
}

func TestLoadInboxRefreshSkipsListing(t *testing.T) {
	f := inboxSourceFixture()
	e := inboxEnv(t, f)
	first := loadInbox(context.Background(), e, workfile.NewInboxState(), nil, loadNow, pollFull)
	f.keys = nil
	loadInbox(context.Background(), e, workfile.NewInboxState(), &first, loadNow.Add(10*time.Minute), pollRefresh)
	if f.listingReads != 1 || !slices.Equal(f.keys, []string{"APP-1"}) {
		t.Fatalf("a refresh re-reads my known tickets without the board: listings %d, tickets %v", f.listingReads, f.keys)
	}
}
