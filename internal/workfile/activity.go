package workfile

import (
	"strings"
	"time"
)

// ActivityKind is one kind of thing that happened to a ticket or PR. Kinds are
// neutral: the inbox never knows which service an event came from.
type ActivityKind string

const (
	ActivityComment          ActivityKind = "comment"
	ActivityReview           ActivityKind = "review"
	ActivityApproved         ActivityKind = "approved"
	ActivityChangesRequested ActivityKind = "changes_requested"
	ActivityReviewRequested  ActivityKind = "review_requested"
	ActivityPushed           ActivityKind = "pushed"
)

// Activity is one event on a ticket or a PR. Ticket or PR may be empty, not
// both. PR is the record ID, as Record.ID has it. Who is a name to show and
// WhoID the identity to compare, on Connection only. Text, Where and Who come
// from a provider and are untrusted.
type Activity struct {
	Connection, Ticket, PR string
	Kind                   ActivityKind
	Who, WhoID             string
	At                     time.Time
	Text, Where, URL       string
}

// Key names the event, so seen and done survive restarts. An edit to its text
// keeps the key.
func (a Activity) Key() string {
	return strings.Join([]string{a.Connection, "t:" + a.Ticket, "p:" + a.PR, string(a.Kind), a.At.UTC().Format(time.RFC3339Nano), a.WhoID}, "|")
}

// ActivityQuery is what one poll asks for: comments on these tickets, and
// what happened on these PRs since each was last read.
type ActivityQuery struct {
	Tickets []string
	PRs     []PRCursor
}

// PRCursor is a PR and when the inbox last read its activity.
type PRCursor struct {
	ID   string
	Seen time.Time
}

// ActivityResult is what a poll found. Updated is the newest update time read
// for each PR, to store as its next cursor.
type ActivityResult struct {
	Events  []Activity
	Updated map[string]time.Time
}
