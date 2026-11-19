package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

func inboxSource(count int) *fakeSource {
	f := &fakeSource{details: map[string]workfile.Detail{}, records: map[string]map[string][]workfile.Record{}}
	for i := 1; i <= count; i++ {
		key := fmt.Sprintf("APP-%d", i)
		f.listing = append(f.listing, workfile.Listing{Key: key, Title: "Improve search", State: "doing", Status: "In Progress", AssigneeID: "example-bob", Assignee: "Bob", URL: "https://example.com/" + key})
		ticket := fixtureTicket(key, "doing", strings.Repeat("Clear description. ", 10))
		ticket.Assignee = "example-bob"
		f.tickets = append(f.tickets, ticket)
		f.details[key] = workfile.Detail{Key: key, Body: para("Description")}
		record := workfile.Record{ID: fmt.Sprintf("web#%d", i), URL: fmt.Sprintf("https://example.com/web/pull/%d", i), ChecksURL: fmt.Sprintf("https://example.com/web/pull/%d/checks", i), Facts: workfile.Facts{
			"state": "open", "author": "example-bob", "checks": "failure", "mergeable": "mergeable", "draft": "false",
			"changes_requested": []string{}, "review_requests": []string{}, "approvals": []workfile.Actor{}, "labels": []string{},
		}}
		f.records[key] = map[string][]workfile.Record{"github": {record}}
	}
	return f
}

func enableInbox(d *trackerDriver) {
	d.m.overview = true
	d.run(d.m.actionIdentity())
	d.run(d.m.pumpChecks())
	d.check()
}

func inboxViews() workfile.TrackerSettings {
	var views []workfile.TrackerView
	for _, v := range []struct{ id, title, attention string }{{"in-flight", "All in flight", ""}, {"needs-me", "Needs me", "needs-me"}, {"waiting", "Waiting", "waiting"}, {"ready-now", "Ready to move", "ready"}, {"unknown", "Cannot check", "unknown"}} {
		views = append(views, workfile.TrackerView{ID: v.id, Title: v.title, Filter: workfile.TrackerFilter{Assignee: "me", States: []string{"doing"}, Attention: v.attention}})
	}
	return workfile.TrackerSettings{Views: views, Columns: []string{"key", "title", "status", "action"}}
}

func TestTrackerActionInboxIsBoundedAndDoesNotReadBackgroundDescriptions(t *testing.T) {
	f := inboxSource(30)
	d := driveWithSettings(t, f, 160, 40, graphics{}, options{}, inboxViews())
	enableInbox(d)
	if len(f.policyReads) != trackerCheckBatch || len(f.detailReads) != 1 || f.listingReads != 1 {
		t.Fatalf("unbounded reads: policies %d, descriptions %d, listings %d", len(f.policyReads), len(f.detailReads), f.listingReads)
	}
	if got := d.m.nextActions("APP-1"); got.State != workfile.NeedsMe || got.Summary != "Fix failing checks" {
		t.Fatalf("known operational issue missing: %+v", got)
	}
	d.wants("Synced", "Fix failing checks")
	d.press("n")
	if len(f.policyReads) != 24 || f.listingReads != 1 || len(f.detailReads) != 1 {
		t.Fatalf("check-more did not load exactly one bounded batch: %d", len(f.policyReads))
	}
	d.press("n")
	if len(f.policyReads) != 30 {
		t.Fatalf("final batch fetched duplicates: %d", len(f.policyReads))
	}
	for _, key := range f.policyReads {
		if count := slicesCount(f.policyReads, key); count != 1 {
			t.Fatalf("read %s %d times", key, count)
		}
	}
}

func slicesCount(values []string, value string) int {
	count := 0
	for _, candidate := range values {
		if candidate == value {
			count++
		}
	}
	return count
}

func TestTrackerAttentionViewsDistinguishNeedsMeWaitingReadyAndUnknown(t *testing.T) {
	f := inboxSource(4)
	f.records["APP-2"]["github"][0].Facts["checks"] = "success"
	f.records["APP-2"]["github"][0].Facts["review_requests"] = []string{"example-reviewer"}
	f.records["APP-3"]["github"][0].Facts["checks"] = "success"
	f.records["APP-4"]["github"][0].Facts["checks"] = "success"
	f.records["APP-4"]["github"][0].Unavailable = map[string]string{"checks": "unavailable"}
	d := driveWithSettings(t, f, 160, 50, graphics{}, options{}, inboxViews())
	enableInbox(d)
	for _, want := range []string{"APP-1", "APP-2", "APP-3", "APP-4"} {
		d.press("right")
		if got := listKeys(d); got != want {
			t.Fatalf("%s: wanted %s, got %s", d.m.shownView().label(), want, got)
		}
	}
	if len(f.policyReads) != 4 || f.listingReads != 1 {
		t.Fatal("switching computed views fetched policy again")
	}
}

func TestTrackerEmptyAttentionViewReportsUncheckedWork(t *testing.T) {
	f := inboxSource(30)
	s := inboxViews()
	s.DefaultView = "waiting"
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	enableInbox(d)
	d.wants("12 of 30 checked", "press n")
	if len(f.policyReads) != 12 || len(f.detailReads) != 0 {
		t.Fatal("empty computed view fetched all tickets or previews")
	}
	d.press("r")
	if len(f.policyReads) != 24 || len(f.detailReads) != 0 {
		t.Fatal("empty attention view cannot refresh its bounded sample")
	}
}

func TestTrackerEmptyAttentionViewDoesNotHideUnavailableEvidence(t *testing.T) {
	f := inboxSource(3)
	f.policyErr = errors.New("unavailable")
	s := inboxViews()
	s.DefaultView = "waiting"
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	enableInbox(d)
	d.wants("3 have unavailable evidence")
	if len(f.policyReads) != 3 || len(f.detailReads) != 0 {
		t.Fatal("unavailable evidence retried without an explicit request")
	}
}

func TestTrackerBackgroundPolicySharesReadWithSelectionAndLimitsConcurrency(t *testing.T) {
	f := inboxSource(20)
	d := driveWithSettings(t, f, 160, 40, graphics{}, options{}, inboxViews())
	d.m.overview = true
	d.m.who = map[string]string{"jira": "example-bob", "github": "example-bob"}
	cmd := d.m.pumpChecks()
	active := 0
	for _, r := range d.m.ticketReads.byKey {
		if r.policyStarted && !r.policyDone() {
			active++
		}
	}
	if active != trackerCheckWorkers || d.m.pumpChecks() != nil {
		t.Fatalf("expected %d in-flight workers, got %d", trackerCheckWorkers, active)
	}
	d.press("down") // APP-2 is already queued; selection must share that check.
	d.run(cmd)
	if slicesCount(f.policyReads, "APP-2") != 1 || !slices.Contains(f.detailReads, "APP-2") {
		t.Fatalf("selection duplicated or missed queued check: %v", f.policyReads)
	}
}

func TestTrackerActionLinksFreshnessAndRefresh(t *testing.T) {
	f := inboxSource(3)
	d := driveWithSettings(t, f, 160, 50, graphics{}, options{}, inboxViews())
	d.m.now = func() time.Time { return time.Date(2026, 1, 2, 12, 34, 0, 0, time.UTC) }
	enableInbox(d)
	d.press("p", "c")
	if !slices.Equal(d.opened, []string{"https://example.com/web/pull/1", "https://example.com/web/pull/1/checks"}) {
		t.Fatalf("wrong targets: %v", d.opened)
	}
	before := d.m.ticketReads.byKey["APP-1"]
	d.press("r")
	if d.m.ticketReads.byKey["APP-1"] == before || slicesCount(f.policyReads, "APP-1") != 2 {
		t.Fatal("refresh kept stale policy")
	}
	d.wants("Checked at 12:34")
	d.send(ticketPolicyMsg{before, workfile.Assessment{}, nil})
	if d.m.ticketReads.byKey["APP-1"].policy == nil || d.m.nextActions("APP-1").State != workfile.NeedsMe {
		t.Fatal("late response replaced refreshed result")
	}
}

func TestTrackerDoesNotCallPendingOwnershipUnavailable(t *testing.T) {
	d := driveWithSettings(t, inboxSource(1), 100, 30, graphics{}, options{}, inboxViews())
	d.m.overview = true
	d.m.actionIdentity() // Schedule, but do not deliver the identity result yet.
	if got := d.m.nextActions("APP-1").Summary; got != "Identifying action owner…" {
		t.Fatalf("pending ownership became unavailable: %s", got)
	}
	d.m.me = ""
	if d.m.findMe() != nil {
		t.Fatal("pending identity was requested a second time")
	}
}

func TestTrackerActionLinksRejectNonWebTargetsAndTicketFallback(t *testing.T) {
	f := inboxSource(1)
	r := &f.records["APP-1"]["github"][0]
	r.URL, r.ChecksURL = "file:///tmp/example", "--unsafe-option"
	d := driveWithSettings(t, f, 160, 40, graphics{}, options{}, inboxViews())
	enableInbox(d)
	d.press("p", "c")
	if len(d.opened) != 0 {
		t.Fatal("unsafe provider target was opened")
	}
	f = inboxSource(1)
	delete(f.records, "APP-1")
	d = driveWithSettings(t, f, 160, 40, graphics{}, options{}, inboxViews())
	enableInbox(d)
	d.press("p")
	if len(d.opened) != 0 {
		t.Fatal("PR shortcut fell back to opening a tracker ticket")
	}
}

func TestTrackerPlainAttentionViewIsASampleAndHasNoDetailReads(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "config.yml")
	t.Setenv("WORKFILE_CONFIG_FILE", path)
	if err := os.WriteFile(path, []byte("tracker:\n  default_view: needs-me\n  views:\n    - id: needs-me\n      title: Needs me\n      filter: {assignee: me, states: [doing], attention: needs-me}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := inboxSource(30)
	code, out, errOut := invoke(t, f, "tracker")
	if code != 0 || errOut != "" || !strings.Contains(out, "Checked 12 of 30 active tickets") {
		t.Fatalf("plain attention output is misleading: %d %s %s", code, out, errOut)
	}
	if len(f.policyReads) != 12 || len(f.detailReads) != 0 || f.listingReads != 1 || strings.Contains(out, "\x1b") {
		t.Fatal("plain attention view fetched too much or emitted escapes")
	}
}

func TestTrackerActionInboxFitsNarrowScreensAndSearchOwnsNewKeys(t *testing.T) {
	for _, width := range []int{40, 60} {
		f := inboxSource(3)
		d := driveWithSettings(t, f, width, 30, graphics{}, options{}, inboxViews())
		enableInbox(d)
		d.press("tab", "down", "tab", "/", "p", "c", "n")
		if d.m.list.FilterValue() != "pcn" || len(d.opened) != 0 || len(f.policyReads) != 3 {
			t.Fatal("new shortcuts leaked out of search")
		}
		d.press("esc")
		d.width = width + 1
		d.send(tea.WindowSizeMsg{Width: width + 1, Height: 30})
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("plain action inbox contains escapes")
		}
	}
}
