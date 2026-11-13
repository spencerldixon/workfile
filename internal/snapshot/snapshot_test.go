package snapshot

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func TestParseSince(t *testing.T) {
	for in, want := range map[string]Since{
		"last": {Last: true},
		"1h":   {Ago: time.Hour},
		"12h":  {Ago: 12 * time.Hour},
		"168h": {Ago: 168 * time.Hour},
		"1d":   {Ago: 24 * time.Hour},
		"7d":   {Ago: 7 * 24 * time.Hour},
	} {
		got, err := ParseSince(in)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0h", "169h", "0d", "8d", "h", "d", "12", "1.5d", "-1d", "+2h", "12m", "Last", "1dd"} {
		if _, err := ParseSince(in); err == nil {
			t.Errorf("%q should be refused", in)
		}
	}
}

func entry(key, stage string, standing workfile.Standing, unmet, ready []string) Entry {
	return Entry{Key: key, Title: "Title " + key, Stage: stage, Standing: standing, Unmet: unmet, Ready: ready}
}

func snap(entries ...Entry) Snapshot {
	s := Snapshot{Version: 1, Entries: map[string]Entry{}}
	for _, e := range entries {
		s.Entries[e.Key] = e
	}
	return s
}

func TestDiffReportsEveryKind(t *testing.T) {
	before := snap(
		entry("APP-1", "doing", workfile.NeedsWork, nil, nil),
		entry("APP-2", "review", workfile.NeedsWork, nil, nil),
		entry("APP-3", "review", workfile.NeedsWork, nil, nil),
		entry("APP-4", "review", workfile.Blocked, []string{"code_review"}, nil),
		entry("APP-5", "doing", workfile.NeedsWork, nil, []string{"review"}),
		entry("APP-6", "doing", workfile.NeedsWork, nil, nil),
		entry("APP-7", "done", workfile.OutOfPolicy, []string{"refinement"}, nil),
		entry("APP-9", "doing", workfile.NeedsWork, nil, nil),
	)
	now := snap(
		entry("APP-1", "review", workfile.NeedsWork, nil, nil),
		entry("APP-2", "review", workfile.NeedsWork, nil, []string{"done"}),
		entry("APP-3", "review", workfile.Blocked, []string{"code_review"}, nil),
		entry("APP-4", "review", workfile.NeedsWork, nil, nil),
		entry("APP-5", "doing", workfile.NeedsWork, nil, []string{"review"}),
		entry("APP-6", "doing", workfile.OutOfPolicy, []string{"pull_request"}, nil),
		entry("APP-7", "done", workfile.Done, nil, nil),
		entry("APP-8", "todo", workfile.NeedsWork, nil, nil),
	)
	want := []Change{
		{Key: "APP-1", Title: "Title APP-1", Kind: Moved, Stage: "review", From: "doing", To: "review"},
		{Key: "APP-2", Title: "Title APP-2", Kind: BecameReady, Stage: "review", To: "done"},
		{Key: "APP-3", Title: "Title APP-3", Kind: NewlyBlocked, Stage: "review", Gates: []string{"code_review"}},
		{Key: "APP-4", Title: "Title APP-4", Kind: BackOnTrack, Stage: "review"},
		{Key: "APP-6", Title: "Title APP-6", Kind: OutOfPolicy, Stage: "doing", Gates: []string{"pull_request"}},
		{Key: "APP-7", Title: "Title APP-7", Kind: BackOnTrack, Stage: "done"},
		{Key: "APP-8", Title: "Title APP-8", Kind: Appeared, Stage: "todo"},
	}
	if got := Diff(before, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestDiffGivesOneTicketItsChangesInKindOrder(t *testing.T) {
	before := snap(entry("APP-1", "doing", workfile.NeedsWork, nil, nil))
	now := snap(entry("APP-1", "review", workfile.Blocked, []string{"code_review"}, []string{"qa"}))
	var kinds []ChangeKind
	for _, c := range Diff(before, now) {
		kinds = append(kinds, c.Kind)
	}
	if !reflect.DeepEqual(kinds, []ChangeKind{Moved, BecameReady, NewlyBlocked}) {
		t.Fatalf("%v", kinds)
	}
}

func TestDiffIgnoresTicketsMissingFromNow(t *testing.T) {
	before := snap(entry("APP-1", "doing", workfile.NeedsWork, nil, nil), entry("APP-2", "doing", workfile.NeedsWork, nil, nil))
	now := snap(entry("APP-1", "doing", workfile.NeedsWork, nil, nil))
	if got := Diff(before, now); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestDiffStillComparesAcrossAPolicyChange(t *testing.T) {
	before := snap(entry("APP-1", "qa", workfile.NeedsWork, nil, nil))
	before.PolicyHash = "sha256:old"
	now := snap(entry("APP-1", "review", workfile.NeedsWork, nil, nil))
	now.PolicyHash = "sha256:new"
	got := Diff(before, now)
	if len(got) != 1 || got[0].From != "qa" || got[0].To != "review" {
		t.Fatalf("%+v", got)
	}
}

func TestOrderByStagesFollowsThePipelineThenKey(t *testing.T) {
	in := Diff(Snapshot{}, snap(entry("A-3", "review", 0, nil, nil), entry("A-1", "review", 0, nil, nil), entry("A-2", "doing", 0, nil, nil), entry("A-4", "gone", 0, nil, nil)))
	var keys []string
	for _, c := range OrderByStages(in, []string{"todo", "doing", "review"}) {
		keys = append(keys, c.Key)
	}
	if strings.Join(keys, " ") != "A-2 A-1 A-3 A-4" {
		t.Fatalf("%v", keys)
	}
}

func TestFromAssessmentsIsIdle(t *testing.T) {
	s := FromAssessments("sha256:x", time.Unix(0, 0), nil)
	if s.Version != 1 || s.PolicyHash != "sha256:x" || s.Entries == nil {
		t.Fatalf("%+v", s)
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newStore(t *testing.T) (Store, *clock) {
	c := &clock{time.Date(2026, 11, 13, 9, 10, 0, 0, time.UTC)}
	return Store{Dir: filepath.Join(t.TempDir(), "snapshots"), Now: c.now, Keep: 8 * 24 * time.Hour}, c
}

func saved(s Store, c *clock, entries ...Entry) error {
	snapshot := snap(entries...)
	for key, e := range snapshot.Entries {
		e.SeenAt = c.t
		snapshot.Entries[key] = e
	}
	return s.Save(snapshot)
}

func names(t *testing.T, s Store) []string {
	t.Helper()
	list, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		out = append(out, e.Name())
	}
	return out
}

func TestStoreWithNoSnapshots(t *testing.T) {
	s, c := newStore(t)
	if _, ok, err := s.Latest(); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, ok, err := s.Before(c.t); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestStoreKeepsOneSnapshotPerHourAndUpserts(t *testing.T) {
	s, c := newStore(t)
	if err := saved(s, c, entry("APP-1", "todo", workfile.NeedsWork, nil, nil), entry("APP-2", "todo", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(30 * time.Minute)
	if err := saved(s, c, entry("APP-1", "doing", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if got := names(t, s); !reflect.DeepEqual(got, []string{"2026-11-13T09.json"}) {
		t.Fatalf("%v", got)
	}
	latest, ok, err := s.Latest()
	if err != nil || !ok || latest.Entries["APP-1"].Stage != "doing" || latest.Entries["APP-2"].Stage != "todo" {
		t.Fatalf("%+v %v %v", latest, ok, err)
	}
	c.t = c.t.Add(time.Hour)
	if err := saved(s, c, entry("APP-3", "todo", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if got := names(t, s); !reflect.DeepEqual(got, []string{"2026-11-13T09.json", "2026-11-13T10.json"}) {
		t.Fatalf("%v", got)
	}
	latest, _, _ = s.Latest()
	if len(latest.Entries) != 3 {
		t.Fatalf("the rest of the entries are kept: %+v", latest.Entries)
	}
}

func TestStoreDropsOldEntriesAndFiles(t *testing.T) {
	s, c := newStore(t)
	if err := saved(s, c, entry("APP-1", "todo", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(9 * 24 * time.Hour)
	if err := saved(s, c, entry("APP-2", "todo", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if got := names(t, s); !reflect.DeepEqual(got, []string{"2026-11-22T09.json"}) {
		t.Fatalf("old file kept: %v", got)
	}
	latest, _, _ := s.Latest()
	if _, old := latest.Entries["APP-1"]; old || len(latest.Entries) != 1 {
		t.Fatalf("old entry kept: %+v", latest.Entries)
	}
}

func TestStoreBeforeFindsTheNewestAtOrBeforeOrFallsBack(t *testing.T) {
	s, c := newStore(t)
	start := c.t
	for i := range 3 {
		c.t = start.Add(time.Duration(i) * 24 * time.Hour)
		if err := saved(s, c, entry("APP-1", "s"+string(rune('a'+i)), workfile.NeedsWork, nil, nil)); err != nil {
			t.Fatal(err)
		}
	}
	stage := func(at time.Time) (string, bool) {
		snap, ok, err := s.Before(at)
		if err != nil {
			t.Fatal(err)
		}
		return snap.Entries["APP-1"].Stage, ok
	}
	if got, ok := stage(start.Add(24 * time.Hour)); got != "sb" || !ok {
		t.Fatalf("exact: %s %v", got, ok)
	}
	if got, ok := stage(start.Add(36 * time.Hour)); got != "sb" || !ok {
		t.Fatalf("between: %s %v", got, ok)
	}
	if got, ok := stage(start.Add(100 * time.Hour)); got != "sc" || !ok {
		t.Fatalf("after all: %s %v", got, ok)
	}
	if got, ok := stage(start.Add(-time.Hour)); got != "sa" || ok {
		t.Fatalf("fallback is the oldest, not ok: %s %v", got, ok)
	}
}

func TestStoreWritesAtomicallyAndIgnoresStrayFiles(t *testing.T) {
	s, c := newStore(t)
	if err := saved(s, c, entry("APP-1", "todo", workfile.NeedsWork, nil, nil)); err != nil {
		t.Fatal(err)
	}
	for _, name := range names(t, s) {
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".") {
			t.Fatalf("temporary file left behind: %s", name)
		}
	}
	if err := os.WriteFile(filepath.Join(s.Dir, ".snapshot-123.tmp"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Latest(); !ok || err != nil {
		t.Fatal(ok, err)
	}
}
