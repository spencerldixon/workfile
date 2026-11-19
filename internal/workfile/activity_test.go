package workfile

import (
	"slices"
	"testing"
	"time"
)

func TestActivityKeyStable(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	a := Activity{Connection: "github", PR: "example-pr", Kind: ActivityComment, WhoID: "example-bob", At: at, Text: "one"}
	b := a
	b.Text = "edited later"
	if a.Key() != b.Key() {
		t.Fatalf("same event gave two keys: %s %s", a.Key(), b.Key())
	}
	c := a
	c.At = at.Add(time.Second)
	if a.Key() == c.Key() {
		t.Fatal("events at different times share a key")
	}
	d := a
	d.PR, d.Ticket = "", "ABC-1"
	if a.Key() == d.Key() {
		t.Fatal("a ticket event and a PR event share a key")
	}
}

func TestKindsListActivity(t *testing.T) {
	known := []string{string(ActivityComment), string(ActivityReview), string(ActivityApproved), string(ActivityChangesRequested), string(ActivityReviewRequested), string(ActivityPushed)}
	for name, kind := range Kinds {
		for _, role := range []*Role{kind.Tracker, kind.CodeHost} {
			if role == nil {
				continue
			}
			for _, a := range role.Activity {
				if !slices.Contains(known, a) {
					t.Errorf("%s lists unknown activity %q", name, a)
				}
			}
		}
	}
	if !slices.Contains(Kinds["github"].CodeHost.Activity, string(ActivityReviewRequested)) {
		t.Error("GitHub code host must supply review requests")
	}
	if got := Kinds["jira"].Tracker.Activity; !slices.Equal(got, []string{"comment"}) {
		t.Errorf("Jira tracker activity = %v, want [comment]", got)
	}
	if got := Kinds["github"].Tracker.Activity; len(got) != 0 {
		t.Errorf("GitHub Issues tracker supplies no activity yet, got %v", got)
	}
}
