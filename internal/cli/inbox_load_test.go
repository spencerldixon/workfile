package cli

import (
	"context"
	"errors"
	"io"
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
	if len(rows) != 1 || rows[0].ID != "APP-1" || !rows[0].Action() {
		t.Fatalf("want one action row for APP-1: %+v", rows)
	}
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
	if len(rows) != 1 || len(rows[0].Events) != 1 || rows[0].Events[0] != f.activity[2] {
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
	if len(rows) != 2 {
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
