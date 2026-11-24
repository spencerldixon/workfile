package workfile

import (
	"testing"
	"time"
)

var inboxNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func inboxWho() map[string]string {
	return map[string]string{"github": "example-alice", "jira": "alice-id"}
}

type prOpt func(*Record)

func withChanges(who ...string) prOpt {
	return func(r *Record) { r.Facts["changes_requested"] = who }
}
func withChecks(state string) prOpt { return func(r *Record) { r.Facts["checks"] = state } }
func withRequests(who ...string) prOpt {
	return func(r *Record) { r.Facts["review_requests"] = who }
}

func inboxPR(id, author string, opts ...prOpt) Record {
	r := Record{ID: id, URL: "https://github.example.com/example-org/" + id, Title: "PR " + id, Head: "head-" + id, Facts: Facts{
		"state": "open", "author": author, "checks": "success", "review_requests": []string{}, "changes_requested": []string{},
	}}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func inboxEvent(kind ActivityKind, pr, who string, ago time.Duration) Activity {
	return Activity{Connection: "github", PR: pr, Kind: kind, WhoID: who, At: inboxNow.Add(-ago), URL: "https://github.example.com/x"}
}

func mineWork(key string, prs ...Record) InboxWork {
	return InboxWork{Listing: Listing{Key: key, Title: "Ticket " + key, State: "review"}, Mine: true, PRs: map[string][]Record{"github": prs}}
}

func build(t *testing.T, in InboxInput, st InboxState) []InboxRow {
	t.Helper()
	if in.Who == nil {
		in.Who = inboxWho()
	}
	if in.Names == nil {
		in.Names = map[string]string{"example-alice": "Alice", "example-bob": "Bob", "example-charlie": "Charlie"}
	}
	if in.Now.IsZero() {
		in.Now = inboxNow
	}
	return BuildInbox(in, st)
}

func rowByID(rows []InboxRow, id string) *InboxRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

func TestInboxOneRowPerTicket(t *testing.T) {
	pr := inboxPR("web#84", "example-alice", withChanges("example-bob"), withChecks("failure"))
	rows := build(t, InboxInput{
		Work:     []InboxWork{mineWork("ABC-12", pr)},
		Activity: []Activity{inboxEvent(ActivityComment, "web#84", "example-bob", time.Minute)},
	}, NewInboxState())
	if len(rows) != 1 {
		t.Fatalf("want one row for ABC-12, got %+v", rows)
	}
	if rows[0].Kind != RowChangesRequested {
		t.Fatalf("changes requested outranks failing checks and comments, got %s", rows[0].Kind)
	}
	if len(rows[0].Events) != 1 || rows[0].New != 1 {
		t.Fatalf("other events stay in the row: %+v", rows[0])
	}
}

func TestInboxStateNotEvents(t *testing.T) {
	pr := inboxPR("web#84", "example-alice", withChecks("success"))
	rows := build(t, InboxInput{Work: []InboxWork{mineWork("ABC-12", pr)}}, NewInboxState())
	for _, r := range rows {
		if r.Kind == RowChecksFailed {
			t.Fatal("checks now pass: no failing-checks row")
		}
	}
}

func TestInboxHidesOwnActions(t *testing.T) {
	pr := inboxPR("web#84", "example-alice")
	rows := build(t, InboxInput{
		Work:     []InboxWork{mineWork("ABC-12", pr)},
		Activity: []Activity{inboxEvent(ActivityComment, "web#84", "example-alice", time.Minute)},
	}, NewInboxState())
	if len(rows) != 0 {
		t.Fatalf("my own comment must not show: %+v", rows)
	}
}

func TestInboxDoneUntilFingerprintChanges(t *testing.T) {
	pr := inboxPR("web#84", "example-alice", withChanges("example-bob"))
	in := InboxInput{Work: []InboxWork{mineWork("ABC-12", pr)}}
	first := build(t, in, NewInboxState())
	st := NewInboxState()
	st.Done["ABC-12"] = first[0].Fingerprint
	if rows := build(t, in, st); len(rows) != 0 {
		t.Fatalf("done row came back with nothing new: %+v", rows)
	}
	pr.Head = "head-new"
	in.Work = []InboxWork{mineWork("ABC-12", pr)}
	if rows := build(t, in, st); len(rows) != 1 {
		t.Fatal("a new push should bring the row back")
	}
	in.Work = []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice", withChanges("example-bob")))}
	in.Activity = []Activity{inboxEvent(ActivityComment, "web#84", "example-bob", time.Minute)}
	if rows := build(t, in, st); len(rows) != 1 {
		t.Fatal("a new event on the ticket should bring the row back")
	}
}

func TestInboxSnoozeExpires(t *testing.T) {
	in := InboxInput{Work: []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice", withChanges("example-bob")))}}
	fp := build(t, in, NewInboxState())[0].Fingerprint
	st := NewInboxState()
	st.Snoozed["ABC-12"] = Snooze{Until: inboxNow.Add(time.Minute), Fingerprint: fp}
	if rows := build(t, in, st); len(rows) != 0 {
		t.Fatal("snoozed row shown")
	}
	in.Now = inboxNow.Add(2 * time.Minute)
	if rows := build(t, in, st); len(rows) != 1 {
		t.Fatal("snooze expired, row should be back")
	}
}

func TestInboxCommentsOnMyPRAreAction(t *testing.T) {
	rows := build(t, InboxInput{
		Work: []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice"))},
		Activity: []Activity{
			inboxEvent(ActivityComment, "web#84", "example-bob", 5*time.Minute),
			inboxEvent(ActivityReview, "web#84", "example-bob", 2*time.Minute),
		},
	}, NewInboxState())
	if len(rows) != 1 || rows[0].Kind != RowComments || !rows[0].Action() {
		t.Fatalf("comments on my PR are an action row: %+v", rows)
	}
	if rows[0].Text != "Bob left 2 comments on #84" || rows[0].Count != 2 || rows[0].Who != "Bob" {
		t.Fatalf("wrong copy: %+v", rows[0])
	}
	if !rows[0].Since.Equal(inboxNow.Add(-5 * time.Minute)) {
		t.Fatalf("waiting since the oldest unread comment, got %s", rows[0].Since)
	}
}

func TestInboxCommentsElsewhereAreInfo(t *testing.T) {
	other := InboxWork{Listing: Listing{Key: "ABC-21", Title: "Invoice"}, PRs: map[string][]Record{"github": {inboxPR("web#97", "example-charlie")}}}
	rows := build(t, InboxInput{
		Work:     []InboxWork{other},
		Activity: []Activity{inboxEvent(ActivityComment, "web#97", "example-bob", time.Minute)},
	}, NewInboxState())
	if len(rows) != 1 || rows[0].Kind != RowInfo || rows[0].Action() {
		t.Fatalf("comments on a PR I do not own are information: %+v", rows)
	}
}

func TestInboxSeenInfoHidden(t *testing.T) {
	ev := inboxEvent(ActivityApproved, "web#84", "example-charlie", time.Minute)
	st := NewInboxState()
	st.Seen[ev.Key()] = inboxNow
	rows := build(t, InboxInput{Work: []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice"))}, Activity: []Activity{ev}}, st)
	if len(rows) != 0 {
		t.Fatalf("seen information row shown: %+v", rows)
	}
}

func TestInboxOthersTicketNoPolicyRows(t *testing.T) {
	a := Assessment{Ticket: Ticket{Key: "ABC-21"}, Earlier: []Check{{ID: "c", Outcome: "fail"}}}
	other := InboxWork{
		Listing:    Listing{Key: "ABC-21"},
		Assessment: &a,
		Next:       NextActions{State: NeedsMe, Summary: "Get QA sign-off"},
		PRs:        map[string][]Record{"github": {inboxPR("web#97", "example-charlie", withRequests("example-alice"))}},
	}
	rows := build(t, InboxInput{Work: []InboxWork{other}}, NewInboxState())
	if len(rows) != 1 || rows[0].Kind != RowReviewRequested || rows[0].ID != "ABC-21" {
		t.Fatalf("someone else's ticket shows only the review request, keyed to the ticket: %+v", rows)
	}
	if rows[0].Text != "Charlie asked you to review #97" {
		t.Fatalf("wrong copy: %q", rows[0].Text)
	}
}

func TestInboxPolicyRows(t *testing.T) {
	violating := Assessment{Ticket: Ticket{Key: "ABC-9"}, Earlier: []Check{{ID: "qa", Outcome: "fail"}}}
	ready := Assessment{Ticket: Ticket{Key: "ABC-11"}}
	rows := build(t, InboxInput{Work: []InboxWork{
		{Listing: Listing{Key: "ABC-9", State: "qa"}, Mine: true, Assessment: &violating, Next: NextActions{State: NeedsMe, Summary: "Get QA sign-off"}},
		{Listing: Listing{Key: "ABC-11", State: "qa"}, Mine: true, Assessment: &ready, Next: NextActions{State: ReadyToMove, Summary: "Move to release", Actions: []WorkAction{{State: ReadyToMove, Destinations: []string{"release"}}}}},
		{Listing: Listing{Key: "ABC-5"}, Mine: true, Err: "Jira did not answer"},
	}}, NewInboxState())
	want := map[string]struct {
		kind RowKind
		text string
	}{
		"ABC-9":  {RowOutOfPolicy, "Out of policy: Get QA sign-off"},
		"ABC-11": {RowReady, "Ready to move → release"},
		"ABC-5":  {RowCannotCheck, "Couldn't check: Jira did not answer"},
	}
	for id, w := range want {
		r := rowByID(rows, id)
		if r == nil || r.Kind != w.kind || r.Text != w.text {
			t.Errorf("%s: want %s %q, got %+v", id, w.kind, w.text, r)
		}
	}
	if r := rowByID(rows, "ABC-9"); r == nil || r.Fix != "Get QA sign-off" {
		t.Errorf("out of policy row carries the fix: %+v", r)
	}
	if r := rowByID(rows, "ABC-11"); r == nil || r.To != "release" {
		t.Errorf("ready row carries the destination: %+v", r)
	}
}

func TestInboxNeedsWorkOnTicket(t *testing.T) {
	a := Assessment{Ticket: Ticket{Key: "ABC-3"}}
	rows := build(t, InboxInput{Work: []InboxWork{{Listing: Listing{Key: "ABC-3"}, Mine: true, Assessment: &a, Next: NextActions{State: NeedsMe, Summary: "Add acceptance criteria"}}}}, NewInboxState())
	if len(rows) != 1 || rows[0].Kind != RowNeedsWork || rows[0].Text != "Add acceptance criteria" {
		t.Fatalf("a fix assigned to me on the ticket is an action row: %+v", rows)
	}
}

func TestInboxChecksPassedOnce(t *testing.T) {
	in := InboxInput{Work: []InboxWork{mineWork("ABC-16", inboxPR("web#93", "example-alice", withChecks("pending")))}}
	st := NewInboxState()
	st.Observe(in, build(t, in, st))
	in.Work = []InboxWork{mineWork("ABC-16", inboxPR("web#93", "example-alice", withChecks("success")))}
	in.Now = inboxNow.Add(time.Minute)
	st.Observe(in, build(t, in, st))
	rows := build(t, in, st)
	if len(rows) != 1 || rows[0].Kind != RowChecksPassed || rows[0].Text != "Checks passed on #93" {
		t.Fatalf("want one checks-passed information row: %+v", rows)
	}
	for _, e := range rows[0].Events {
		st.Seen[e.Key()] = in.Now
	}
	in.Now = inboxNow.Add(5 * time.Minute)
	st.Observe(in, build(t, in, st))
	if rows := build(t, in, st); len(rows) != 0 {
		t.Fatalf("checks passed shows once, until seen: %+v", rows)
	}
}

func TestInboxFirstSeenSetsWait(t *testing.T) {
	in := InboxInput{Now: inboxNow, Work: []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice", withChecks("failure")))}}
	st := NewInboxState()
	st.Observe(in, build(t, in, st))
	in.Now = inboxNow.Add(time.Hour)
	rows := build(t, in, st)
	if !rows[0].Since.Equal(inboxNow) {
		t.Fatalf("a row with no event waits since first seen, got %s", rows[0].Since)
	}
}

func TestInboxOrder(t *testing.T) {
	rows := build(t, InboxInput{
		Work: []InboxWork{
			mineWork("ABC-1", inboxPR("web#1", "example-alice")),
			mineWork("ABC-2", inboxPR("web#2", "example-alice")),
			mineWork("ABC-3", inboxPR("web#3", "example-charlie")),
			mineWork("ABC-4", inboxPR("web#4", "example-charlie")),
		},
		Activity: []Activity{
			inboxEvent(ActivityComment, "web#1", "example-bob", 5*time.Minute),
			inboxEvent(ActivityComment, "web#2", "example-bob", 50*time.Minute),
			inboxEvent(ActivityApproved, "web#3", "example-bob", 30*time.Minute),
			inboxEvent(ActivityApproved, "web#4", "example-bob", 3*time.Minute),
		},
	}, NewInboxState())
	got := []string{}
	for _, r := range rows {
		got = append(got, r.ID)
	}
	want := []string{"ABC-4", "ABC-1", "ABC-3", "ABC-2"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("newest first across all alerts: got %v want %v", got, want)
		}
	}
}

func TestInboxLoosePR(t *testing.T) {
	rows := build(t, InboxInput{Loose: map[string][]Record{"github": {inboxPR("api#7", "example-bob", withRequests("example-alice"))}}}, NewInboxState())
	if len(rows) != 1 || rows[0].ID != "api#7" || rows[0].Ticket != "" || rows[0].PR != "#7" {
		t.Fatalf("a PR with no ticket is its own row: %+v", rows)
	}
}

func TestInboxCopy(t *testing.T) {
	for _, tc := range []struct {
		name string
		pr   Record
		evs  []Activity
		want string
	}{
		{"changes", inboxPR("web#84", "example-alice", withChanges("example-bob")), nil, "Bob requested changes on #84"},
		{"checks", inboxPR("web#93", "example-alice", withChecks("failure")), nil, "Checks failing on #93"},
		{"one comment", inboxPR("web#84", "example-alice"), []Activity{inboxEvent(ActivityComment, "web#84", "example-bob", time.Minute)}, "Bob commented on #84"},
		{"approved", inboxPR("web#91", "example-charlie"), []Activity{inboxEvent(ActivityApproved, "web#91", "example-charlie", time.Minute)}, "Charlie approved #91"},
		{"pushed", inboxPR("web#84", "example-alice"), []Activity{
			inboxEvent(ActivityPushed, "web#84", "example-bob", time.Minute),
			inboxEvent(ActivityPushed, "web#84", "example-bob", 2*time.Minute),
			inboxEvent(ActivityPushed, "web#84", "example-bob", 3*time.Minute),
		}, "Bob pushed 3 commits to #84"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := build(t, InboxInput{Work: []InboxWork{mineWork("ABC-1", tc.pr)}, Activity: tc.evs}, NewInboxState())
			if len(rows) != 1 || rows[0].Text != tc.want {
				t.Fatalf("want %q, got %+v", tc.want, rows)
			}
		})
	}
	ticketComment := Activity{Connection: "jira", Ticket: "ABC-10", Kind: ActivityComment, Who: "Alice Example", WhoID: "bob-id", At: inboxNow.Add(-time.Minute)}
	rows := build(t, InboxInput{Work: []InboxWork{{Listing: Listing{Key: "ABC-10"}, Mine: true}}, Activity: []Activity{ticketComment}}, NewInboxState())
	if len(rows) != 1 || rows[0].Text != "Alice Example commented on ABC-10" || rows[0].Kind != RowInfo {
		t.Fatalf("ticket comment copy: %+v", rows)
	}
}

func TestInboxWaitRestartsWhenStateReturns(t *testing.T) {
	failing := func(head string) InboxInput {
		pr := inboxPR("web#84", "example-alice", withChecks("failure"))
		pr.Head = head
		return InboxInput{Work: []InboxWork{mineWork("ABC-12", pr)}}
	}
	st := NewInboxState()
	in := failing("h1")
	in.Now = inboxNow
	st.Observe(in, build(t, in, st))
	in = failing("h2")
	in.Now = inboxNow.Add(96 * time.Hour)
	rows := build(t, in, st)
	if !rows[0].Since.Equal(in.Now) {
		t.Fatalf("checks failing again on a new push wait from now, got %s", rows[0].Since)
	}
}

func TestInboxEventNotHiddenByStaleFacts(t *testing.T) {
	ev := inboxEvent(ActivityChangesRequested, "web#84", "example-bob", time.Minute)
	rows := build(t, InboxInput{Work: []InboxWork{mineWork("ABC-12", inboxPR("web#84", "example-alice"))}, Activity: []Activity{ev}}, NewInboxState())
	if len(rows) != 1 || rows[0].Text != "Bob requested changes on #84" {
		t.Fatalf("an event the facts do not show yet still appears: %+v", rows)
	}
}

func TestInboxWatchedLoosePRRecoversAndFailsAgain(t *testing.T) {
	pr := inboxPR("example-api#7", "example-bob", withChecks("failure"))
	pr.Head, pr.Repository = "example-head", "example-org/example-api"
	in := InboxInput{
		Now: inboxNow, Who: map[string]string{"github": "example-alice"},
		Loose:        map[string][]Record{"github": {pr}},
		Repositories: map[string][]string{"github": {"example-api"}},
	}
	st := NewInboxState()
	observe := func(state string, at time.Time) []InboxRow {
		pr.Facts["checks"] = state
		in.Now = at
		st.Observe(in, nil)
		return BuildInbox(in, st)
	}
	rows := observe("failure", inboxNow)
	if len(rows) != 1 || rows[0].Ticket != "" || rows[0].Kind != RowChecksFailed {
		t.Fatalf("wanted a failure on the watched PR without a ticket: %+v", rows)
	}
	id := rows[0].ID
	st.Done[id] = rows[0].Fingerprint
	if rows := observe("failure", inboxNow.Add(time.Minute)); len(rows) != 0 {
		t.Fatalf("an acknowledged failure should stay resolved: %+v", rows)
	}
	rows = observe("success", inboxNow.Add(2*time.Minute))
	if len(rows) != 1 || rows[0].Kind != RowChecksPassed || rows[0].ID != id {
		t.Fatalf("expected the same PR to recover: %+v", rows)
	}
	rows = observe("failure", inboxNow.Add(3*time.Minute))
	if len(rows) != 1 || rows[0].Kind != RowChecksFailed || rows[0].ID != id {
		t.Fatalf("a fresh failure on the same commit should reopen the alert: %+v", rows)
	}
}
