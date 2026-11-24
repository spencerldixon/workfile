package workfile

import (
	"slices"
	"strings"
)

// AttentionState describes who can move work forward, not its workflow stage.
type AttentionState string

const (
	NeedsMe          AttentionState = "needs-me"
	Waiting          AttentionState = "waiting"
	ReadyToMove      AttentionState = "ready"
	AttentionUnknown AttentionState = "unknown"
	AttentionDone    AttentionState = "done"
)

type WorkAction struct {
	State                        AttentionState
	Target, URL, ChecksURL, Text string
	// Fact is the evidence the action is about, such as "checks", so a screen
	// can link to the right place. It may be empty.
	Fact         string
	Destinations []string
}

type NextActions struct {
	State   AttentionState
	Summary string
	Actions []WorkAction
}

// NextActions combines policy fixes with operational PR signals. Identities are
// keyed by connection, so a Jira assignee is never compared to a GitHub login.
// Missing ownership or evidence stays unknown, not ready or waiting.
func (w *Workspace) NextActions(a Assessment, who map[string]string) NextActions {
	result := NextActions{State: AttentionUnknown, Summary: "No next action established"}
	var approvalWaits []WorkAction
	seen := map[string]int{}
	add := func(action WorkAction, identity string) {
		if at, ok := seen[identity]; ok {
			for _, destination := range action.Destinations {
				if !slices.Contains(result.Actions[at].Destinations, destination) {
					result.Actions[at].Destinations = append(result.Actions[at].Destinations, destination)
				}
			}
			return
		}
		seen[identity] = len(result.Actions)
		result.Actions = append(result.Actions, action)
	}
	if a.Ticket.Error != "" {
		add(WorkAction{State: AttentionUnknown, Target: a.Ticket.Key, URL: a.Ticket.URL, Text: "Cannot check this ticket's policy"}, "ticket/error")
	}
	owned := func(provider string, record Record) bool {
		id := who[provider]
		author, _ := record.Facts["author"].(string)
		return id != "" && author != "" && strings.EqualFold(id, author) && record.Unavailable["author"] == ""
	}
	for _, provider := range SortedKeys(a.Ticket.Records) {
		for _, record := range a.Ticket.Records[provider] {
			state, _ := record.Facts["state"].(string)
			if state == "" || record.Unavailable["state"] != "" {
				add(WorkAction{State: AttentionUnknown, Target: record.ID, URL: record.URL, Text: "PR state unavailable"}, provider+"/"+record.ID+"/state")
				continue
			}
			if state != "open" {
				continue
			}
			mine := owned(provider, record)
			need := NeedsMe
			if !mine {
				need = AttentionUnknown
			}
			addSignal := func(fact, text string, state AttentionState) {
				add(WorkAction{State: state, Target: record.ID, URL: record.URL, ChecksURL: record.ChecksURL, Text: text, Fact: fact}, provider+"/"+record.ID+"/"+fact)
			}
			if who[provider] == "" {
				addSignal("identity", "Code-host identity unavailable", AttentionUnknown)
			}
			for _, fact := range []string{"mergeable", "changes_requested", "author", "review_requests", "approvals"} {
				if record.Unavailable[fact] != "" {
					addSignal(fact, "PR "+strings.ReplaceAll(fact, "_", " ")+" unavailable", AttentionUnknown)
				}
			}
			changes, changesKnown := record.Facts["changes_requested"].([]string)
			if changesKnown && len(changes) > 0 && record.Unavailable["changes_requested"] == "" {
				requests, known := record.Facts["review_requests"].([]string)
				known = known && record.Unavailable["review_requests"] == ""
				reRequested := known && !slices.ContainsFunc(changes, func(reviewer string) bool {
					return !slices.ContainsFunc(requests, func(request string) bool { return strings.EqualFold(request, reviewer) })
				})
				switch {
				case mine && reRequested:
					addSignal("changes_requested", "Waiting for re-review", Waiting)
				case mine && !known:
					addSignal("changes_requested", "Requested changes; re-review requests unavailable", AttentionUnknown)
				default:
					addSignal("changes_requested", "Address requested changes", need)
				}
			}
			if mergeable, _ := record.Facts["mergeable"].(string); mergeable == "conflicting" && record.Unavailable["mergeable"] == "" {
				addSignal("mergeable", "Resolve merge conflicts", need)
			}
			if record.UnresolvedReviewThreads {
				addSignal("review_threads", ResolveReviewThreads, need)
			}
			checks, known := record.Facts["checks"].(string)
			if record.Unavailable["checks"] != "" {
				known = false
			}
			switch {
			case known && checks == "failure":
				addSignal("checks", "Fix failing checks", need)
			case known && checks == "pending":
				addSignal("checks", "Waiting for checks", Waiting)
			case !known:
				addSignal("checks", "Check results unavailable", AttentionUnknown)
			}
			requests, requestsKnown := record.Facts["review_requests"].([]string)
			requestsKnown = requestsKnown && record.Unavailable["review_requests"] == ""
			if who[provider] != "" && requestsKnown && slices.ContainsFunc(requests, func(id string) bool { return strings.EqualFold(id, who[provider]) }) {
				addSignal("review", "Review this pull request", NeedsMe)
			}
			// With no review request or policy requirement, do not invent a wait.
			if mine && requestsKnown && len(requests) > 0 && len(changes) == 0 {
				approvals, approvalsKnown := record.Facts["approvals"].([]Actor)
				if approvalsKnown && len(approvals) == 0 && record.Unavailable["approvals"] == "" {
					approvalWaits = append(approvalWaits, WorkAction{State: Waiting, Target: record.ID, URL: record.URL, Text: "Waiting for approval", Fact: "approvals"})
				}
			}
		}
	}

	type requirement struct {
		check       Check
		destination string
	}
	var requirements []requirement
	for _, check := range a.Earlier {
		requirements = append(requirements, requirement{check: check})
	}
	for _, route := range a.Routes {
		if route.Backward {
			continue
		}
		for _, check := range route.Checks {
			requirements = append(requirements, requirement{check, route.To})
		}
	}
	for _, requirement := range requirements {
		check := requirement.check
		if check.Outcome != "fail" && check.Outcome != "unknown" {
			continue
		}
		if len(check.Reasons) == 0 {
			add(WorkAction{State: AttentionUnknown, Target: a.Ticket.Key, URL: a.Ticket.URL, Text: "Check unmet requirements"}, "unknown/"+check.ID)
		}
		for _, reason := range check.Reasons {
			state := AttentionUnknown
			target, url := reason.Record, reason.URL
			text := strings.TrimSuffix(reason.Action, ".")
			if text == "" {
				text = "Check unmet requirements"
			}
			fact := check.Condition.Fact
			identity := reason.Provider + "/" + target + "/" + fact
			checksURL := ""
			if reason.OnTicket || reason.Provider == w.Policy.Tracker || target == "" {
				target, url = a.Ticket.Key, a.Ticket.URL
				identity = "ticket/" + a.Ticket.Key + "/" + text
				if check.Outcome == "fail" && who[w.Policy.Tracker] != "" && a.Ticket.Assignee == who[w.Policy.Tracker] {
					state = NeedsMe
				}
			} else {
				for _, record := range a.Ticket.Records[reason.Provider] {
					if record.ID != reason.Record {
						continue
					}
					checksURL = record.ChecksURL
					if url == "" {
						url = record.URL
					}
					if check.Outcome == "fail" && owned(reason.Provider, record) {
						state = NeedsMe
						if fact == "approvals" || fact == "fresh_approvals" {
							requests, known := record.Facts["review_requests"].([]string)
							known = known && record.Unavailable["review_requests"] == ""
							reviewers := w.reviewers(Rule{By: check.By})
							switch {
							case !known:
								state, text = AttentionUnknown, "Approval needed; review requests unavailable"
							case requestedReview(w, check, reason.Provider, requests):
								state, text = Waiting, "Waiting for approval"
								if reviewers != "" {
									text += " from " + reviewers
								}
							case len(check.By) > 0:
								text = "Request review from " + reviewers
							default:
								text = "Request a review"
							}
							// Approval rules with different required reviewers must not
							// collapse into a misleading generic waiting action.
							if len(check.By) > 0 {
								identity += "/" + check.ID
							}
						}
					}
					break
				}
			}
			action := WorkAction{State: state, Target: target, URL: url, ChecksURL: checksURL, Text: text, Fact: fact}
			if requirement.destination != "" {
				action.Destinations = []string{requirement.destination}
			}
			add(action, identity)
		}
	}
	for _, wait := range approvalWaits {
		if !slices.ContainsFunc(result.Actions, func(action WorkAction) bool {
			return action.URL == wait.URL && action.Target == wait.Target && (action.Fact == "approvals" || action.Fact == "fresh_approvals")
		}) {
			result.Actions = append(result.Actions, wait)
		}
	}
	// Never call a ticket ready while linked evidence is unavailable.
	for _, message := range a.Ticket.Unavailable {
		if message != "" {
			add(WorkAction{State: AttentionUnknown, Target: a.Ticket.Key, URL: a.Ticket.URL, Text: "Linked evidence unavailable"}, "ticket/unavailable")
			break
		}
	}
	slices.SortStableFunc(result.Actions, func(x, y WorkAction) int {
		if x.Target == a.Ticket.Key && y.Target != a.Ticket.Key {
			return -1
		}
		if y.Target == a.Ticket.Key && x.Target != a.Ticket.Key {
			return 1
		}
		return 0
	})
	for _, desired := range []AttentionState{NeedsMe, AttentionUnknown, Waiting} {
		for _, action := range result.Actions {
			if action.State == desired {
				result.State, result.Summary = desired, action.Text
				return result
			}
		}
	}
	for _, route := range a.Routes {
		if !route.Backward && route.Available() {
			result.State, result.Summary = ReadyToMove, "Move to "+route.To
			result.Actions = append(result.Actions, WorkAction{State: ReadyToMove, Target: a.Ticket.Key, URL: a.Ticket.URL, Text: result.Summary, Destinations: []string{route.To}})
			return result
		}
	}
	if len(a.Routes) == 0 && !a.Violation() {
		result.State, result.Summary = AttentionDone, "Done"
	}
	return result
}

const ResolveReviewThreads = "Resolve open review threads"

func requestedReview(w *Workspace, check Check, provider string, requests []string) bool {
	if len(requests) == 0 {
		return false
	}
	for _, person := range check.By {
		id := w.People[person][provider]
		if id == "" || !slices.ContainsFunc(requests, func(request string) bool { return strings.EqualFold(request, id) }) {
			return false
		}
	}
	return true
}
