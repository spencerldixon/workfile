package workfile

import (
	"strings"
	"testing"
)

func actionFixture() (*Workspace, Assessment, map[string]string) {
	w := &Workspace{Policy: Policy{Tracker: "tracker"}, People: map[string]map[string]string{"alice": {"code": "example-reviewer"}}}
	r := Record{ID: "web#128", URL: "https://example.com/web/pull/128", ChecksURL: "https://example.com/web/pull/128/checks", Facts: Facts{
		"state": "open", "author": "example-author", "checks": "success", "mergeable": "mergeable",
		"review_requests": []string{}, "approvals": []Actor{}, "changes_requested": []string{},
	}}
	a := Assessment{Ticket: Ticket{Key: "APP-42", URL: "https://example.com/APP-42", Assignee: "example-jira", Records: map[string][]Record{"code": {r}}}, Routes: []RouteAssessment{{To: "review"}}}
	return w, a, map[string]string{"tracker": "example-jira", "code": "example-author"}
}

func approvalRule(a *Assessment) {
	a.Routes[0].Checks = []Check{{ID: "approval", Outcome: "fail", Condition: Expression{Provider: "code", Fact: "approvals"}, Reasons: []Reason{{Provider: "code", Record: "web#128", URL: "https://example.com/web/pull/128", Action: "Get at least 1 approval"}}}}
}

func TestNextActionsOperationalSignalsDoNotDependOnCurrentGate(t *testing.T) {
	for _, tc := range []struct {
		fact  string
		value any
		want  string
	}{
		{"checks", "failure", "Fix failing checks"},
		{"mergeable", "conflicting", "Resolve merge conflicts"},
		{"changes_requested", []string{"example-reviewer"}, "Address requested changes"},
	} {
		t.Run(tc.fact, func(t *testing.T) {
			w, a, who := actionFixture()
			a.Ticket.Records["code"][0].Facts[tc.fact] = tc.value
			got := w.NextActions(a, who)
			if got.State != NeedsMe || got.Summary != tc.want || got.Actions[0].URL != a.Ticket.Records["code"][0].URL {
				t.Fatalf("current gate passes, operational signal must still act: %+v", got)
			}
		})
	}
}

func TestNextActionsSeparatesRequestingReviewFromWaiting(t *testing.T) {
	for _, tc := range []struct {
		name        string
		requests    any
		unavailable bool
		state       AttentionState
		want        string
	}{
		{"no request", []string{}, false, NeedsMe, "Request a review"},
		{"requested", []string{"example-reviewer"}, false, Waiting, "Waiting for approval"},
		{"team requested", []string{"team:example-team"}, false, Waiting, "Waiting for approval"},
		{"unknown request evidence", nil, true, AttentionUnknown, "review requests unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, a, who := actionFixture()
			approvalRule(&a)
			r := &a.Ticket.Records["code"][0]
			r.Facts["review_requests"] = tc.requests
			if tc.unavailable {
				r.Unavailable = map[string]string{"review_requests": "unavailable"}
			}
			got := w.NextActions(a, who)
			if got.State != tc.state || !strings.Contains(got.Summary, tc.want) {
				t.Fatalf("want %s / %s, got %+v", tc.state, tc.want, got)
			}
		})
	}
}

func TestNextActionsExplicitReReviewRequestIsWaitingNotMoreChanges(t *testing.T) {
	w, a, who := actionFixture()
	r := &a.Ticket.Records["code"][0]
	r.Facts["changes_requested"] = []string{"example-reviewer"}
	r.Facts["review_requests"] = []string{"example-reviewer"}
	if got := w.NextActions(a, who); got.State != Waiting || got.Summary != "Waiting for re-review" {
		t.Fatalf("explicit re-review still blamed the author: %+v", got)
	}
}

func TestNextActionsKnownFixWinsEvenWhenPolicyCannotBeChecked(t *testing.T) {
	w, a, who := actionFixture()
	a.Ticket.Error = "some earlier evidence unavailable"
	a.Ticket.Records["code"][0].Facts["checks"] = "failure"
	if got := w.NextActions(a, who); got.State != NeedsMe || got.Summary != "Fix failing checks" {
		t.Fatalf("known actionable evidence was hidden: %+v", got)
	}
}

func TestNextActionsRequiredReviewerCannotBeHiddenByAnotherRequest(t *testing.T) {
	w, a, who := actionFixture()
	approvalRule(&a)
	a.Routes[0].Checks[0].By = []string{"alice"}
	a.Ticket.Records["code"][0].Facts["review_requests"] = []string{"example-someone-else"}
	got := w.NextActions(a, who)
	if got.State != NeedsMe || got.Summary != "Request the required review" {
		t.Fatalf("wrong reviewer counted as waiting: %+v", got)
	}
}

func TestNextActionsDoesNotGuessOwnershipOrUnknownEvidence(t *testing.T) {
	for _, tc := range []string{"different author", "missing identity", "unknown checks", "unknown mergeability"} {
		t.Run(tc, func(t *testing.T) {
			w, a, who := actionFixture()
			r := &a.Ticket.Records["code"][0]
			r.Facts["checks"] = "failure"
			switch tc {
			case "different author":
				r.Facts["author"] = "example-someone-else"
			case "missing identity":
				delete(who, "code")
			case "unknown checks":
				r.Facts["checks"] = "success"
				r.Unavailable = map[string]string{"checks": "unavailable"}
			case "unknown mergeability":
				r.Facts["checks"] = "success"
				r.Unavailable = map[string]string{"mergeable": "not known"}
			}
			if got := w.NextActions(a, who); got.State != AttentionUnknown {
				t.Fatalf("unknown became actionable or ready: %+v", got)
			}
		})
	}
}

func TestNextActionsReviewRequestedFromMeIsActionable(t *testing.T) {
	w, a, who := actionFixture()
	a.Ticket.Records["code"][0].Facts["author"] = "example-other-author"
	a.Ticket.Records["code"][0].Facts["review_requests"] = []string{who["code"]}
	if got := w.NextActions(a, who); got.State != NeedsMe || got.Summary != "Review this pull request" {
		t.Fatalf("review request was lost: %+v", got)
	}
}

func TestNextActionsDeduplicatesFixesAndMergesDestinations(t *testing.T) {
	w, a, who := actionFixture()
	a.Ticket.Records["code"][0].Facts["checks"] = "failure"
	check := Check{ID: "checks", Outcome: "fail", Condition: Expression{Provider: "code", Fact: "checks"}, Reasons: []Reason{{Provider: "code", Record: "web#128", Action: "Fix the failing checks"}}}
	a.Routes = []RouteAssessment{{To: "review", Checks: []Check{check}}, {To: "queued", Checks: []Check{check}}}
	got := w.NextActions(a, who)
	if len(got.Actions) != 1 || len(got.Actions[0].Destinations) != 2 || got.Actions[0].ChecksURL == "" {
		t.Fatalf("duplicate fixes or missing target link: %+v", got)
	}
}

func TestNextActionsNeedsMeWinsOverWaitingAndReady(t *testing.T) {
	w, a, who := actionFixture()
	approvalRule(&a)
	a.Ticket.Records["code"][0].Facts["review_requests"] = []string{"example-reviewer"}
	a.Ticket.Records["code"][0].Facts["checks"] = "failure"
	got := w.NextActions(a, who)
	if got.State != NeedsMe || got.Summary != "Fix failing checks" {
		t.Fatalf("waiting hid a concrete fix: %+v", got)
	}
}

func TestNextActionsReadyAndFinished(t *testing.T) {
	w, a, who := actionFixture()
	if got := w.NextActions(a, who); got.State != ReadyToMove || got.Summary != "Move to review" {
		t.Fatalf("ready route missing: %+v", got)
	}
	a.Routes = nil
	a.Ticket.Records = nil
	if got := w.NextActions(a, who); got.State != AttentionDone {
		t.Fatalf("finished ticket is not done: %+v", got)
	}
}
