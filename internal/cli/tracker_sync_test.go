package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func syncClock(d *trackerDriver) *time.Time {
	now := time.Date(2026, 1, 2, 11, 30, 0, 0, time.Local)
	d.m.now = func() time.Time { return now }
	d.run(d.m.refresh.markSynced(d.m.now()))
	return &now
}

func TestTrackerBoardSyncLabelsAndManualRefresh(t *testing.T) {
	f := inboxSource(20)
	d := driveWithSettings(t, f, 140, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	now := syncClock(d)
	d.wants("Synced 11:30:00", "next 11:32:00", "r refresh")
	if strings.Contains(d.m.navigation(), "checked") || strings.Contains(d.m.navigation(), " of ") {
		t.Fatal("check progress still occupies the sync bar")
	}
	old := d.m.ticketReads.byKey[d.m.current]
	f.listing = append(slices.Clone(f.listing), workfile.Listing{Key: "APP-21", Title: "New work", State: "doing", AssigneeID: "example-bob"})
	*now = now.Add(30 * time.Second)
	d.press("r")
	if f.listingReads != 2 || len(f.policyReads) != 24 || len(f.detailReads) != 0 {
		t.Fatalf("unexpected sync reads: %d listings, %d policies, %d details", f.listingReads, len(f.policyReads), len(f.detailReads))
	}
	if d.m.current != "APP-1" || len(d.m.all) != 21 || !d.m.refresh.lastSync.Equal(*now) || !d.m.refresh.nextSync.Equal(now.Add(trackerSyncInterval)) {
		t.Fatal("refresh lost selection, new tickets or sync timestamps")
	}
	d.wants("Synced 11:30:30", "next 11:32:30")
	d.send(ticketPolicyMsg{old, workfile.Assessment{}, nil})
	if d.m.ticketReads.byKey[d.m.current] == old {
		t.Fatal("old evidence survived a sync")
	}
}

func TestTrackerBoardAutoSyncUsesSameRefreshAndIgnoresStaleTimers(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	now := syncClock(d)
	generation := d.m.refresh.generation
	*now = d.m.refresh.nextSync
	timer := d.m.refresh.timer(trackerSyncScheduleMsg{generation}, d.m.now())
	msg, ok := timer().(trackerSyncDueMsg)
	if !ok {
		t.Fatal("sync timer did not produce the due message")
	}
	d.send(msg)
	if f.listingReads != 2 || !d.m.refresh.lastSync.Equal(*now) {
		t.Fatal("auto sync did not reload the listing")
	}
	d.send(trackerSyncDueMsg{generation})
	if f.listingReads != 2 {
		t.Fatal("stale timer triggered another refresh")
	}
	if d.m.refresh.timer(trackerSyncScheduleMsg{generation}, d.m.now()) != nil {
		t.Fatal("stale schedule was rearmed")
	}
}

func TestTrackerBoardSyncFailureKeepsOldDataAndRetries(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	now := syncClock(d)
	last := d.m.refresh.lastSync
	before := listKeys(d)
	f.listingErr = errors.New("SECRET provider body")
	*now = now.Add(time.Minute)
	d.press("r")
	if d.m.fatal != nil || listKeys(d) != before || !d.m.refresh.lastSync.Equal(last) || !d.m.refresh.nextSync.Equal(now.Add(trackerSyncInterval)) {
		t.Fatal("failed sync discarded data, lied about freshness, or failed to retry")
	}
	d.wants("Cannot sync tickets")
	if strings.Contains(d.screen(), "SECRET") {
		t.Fatal("provider response leaked")
	}
	f.listingErr = nil
	d.press("r")
	if !d.m.refresh.lastSync.Equal(*now) || d.m.notice != "" {
		t.Fatal("manual retry did not recover")
	}
}

func TestTrackerBoardSyncDoesNotOverlapAndListTabsDoNotPoll(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	syncClock(d)
	cmd := d.m.syncTracker()
	if d.m.syncTracker() != nil {
		t.Fatal("overlapping sync was allowed")
	}
	d.wants("Syncing…")
	d.send(trackerSyncedMsg{generation: d.m.refresh.generation - 1})
	if !d.m.refresh.syncing {
		t.Fatal("stale result completed a newer sync")
	}
	d.run(cmd)
	if f.listingReads != 2 {
		t.Fatal("manual refresh duplicated listing reads")
	}
	d.press("]")
	d.send(trackerSyncDueMsg{generation: d.m.refresh.generation})
	if f.listingReads != 2 {
		t.Fatal("list tab issued an automatic polling request")
	}
}

func TestTrackerBoardSyncTimesStayVisibleAtNarrowWidths(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		f := boardSource()
		d := driveWithSettings(t, f, width, 24, graphics{}, options{}, boardSettings())
		now := syncClock(d)
		d.check()
		d.wants("11:30", "next 11:32", "r refresh", "s scope")
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("sync header emitted escapes in plain mode")
		}
		f.listingErr = errors.New("offline")
		*now = now.Add(24 * time.Hour)
		d.press("r")
		d.wants("2 Jan")
	}
}
