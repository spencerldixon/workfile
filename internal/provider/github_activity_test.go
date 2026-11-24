package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"workfile/internal/workfile"
)

var activityNow = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// updatedReply answers a change check: every alias gets updatedAt, newer
// than the cursor only for the PRs listed in changed.
func updatedReply(t *testing.T, query string, changed map[string]bool) string {
	t.Helper()
	var parts []string
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "p") || !strings.Contains(line, "repository(") {
			continue
		}
		alias := line[:strings.Index(line, ":")]
		var repo string
		var number int
		fmt.Sscanf(line[strings.Index(line, `name: "`)+7:], "%s", &repo)
		repo = strings.TrimSuffix(strings.TrimSuffix(repo, ")"), `"`)
		fmt.Sscanf(line[strings.Index(line, "number: ")+8:], "%d", &number)
		at := activityNow.Add(-2 * time.Hour)
		if changed[fmt.Sprintf("%s#%d", repo, number)] {
			at = activityNow.Add(-time.Minute)
		}
		parts = append(parts, fmt.Sprintf(`"%s":{"pullRequest":{"updatedAt":"%s"}}`, alias, at.Format(time.RFC3339)))
	}
	return `{"data":{` + strings.Join(parts, ",") + `}}`
}

const timelineReply = `{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[
 {"__typename":"IssueComment","author":{"login":"example-bob"},"createdAt":"2026-09-30T23:58:00Z","body":"Can we keep return_to?","url":"https://github.example.com/c1"},
 {"__typename":"IssueComment","author":{"login":"example-bob"},"createdAt":"2026-09-01T00:00:00Z","body":"too old","url":"https://github.example.com/c0"},
 {"__typename":"PullRequestReview","author":{"login":"example-charlie"},"submittedAt":"2026-09-30T23:57:00Z","state":"APPROVED","body":"","url":"https://github.example.com/r1","comments":{"nodes":[]}},
 {"__typename":"PullRequestReview","author":{"login":"example-bob"},"submittedAt":"2026-09-30T23:56:00Z","state":"CHANGES_REQUESTED","body":"","url":"https://github.example.com/r2","comments":{"nodes":[{"path":"auth/redirect.go","line":41,"body":"This drops return_to"}]}},
 {"__typename":"PullRequestReview","author":{"login":"example-bob"},"submittedAt":"2026-09-30T23:55:00Z","state":"COMMENTED","body":"Looks close","url":"https://github.example.com/r3","comments":{"nodes":[]}},
 {"__typename":"ReviewRequestedEvent","actor":{"login":"example-alice"},"createdAt":"2026-09-30T23:54:00Z","requestedReviewer":{"login":"example-bob"}},
 {"__typename":"PullRequestCommit","url":"https://github.example.com/k1","commit":{"committedDate":"2026-09-30T23:53:00Z","author":{"user":{"login":"example-bob"}}}}
]}}}}}`

func activityClient(t *testing.T, changed map[string]bool) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var queries []string
	c := client(t, func(r *http.Request) (int, string) {
		q := requestBody(t, r)["query"].(string)
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		if strings.Contains(q, "timelineItems") {
			return 200, timelineReply
		}
		return 200, updatedReply(t, q, changed)
	})
	return c, &queries
}

func cursors(ids ...string) []workfile.PRCursor {
	var out []workfile.PRCursor
	for _, id := range ids {
		out = append(out, workfile.PRCursor{ID: id, Seen: activityNow.Add(-time.Hour)})
	}
	return out
}

func TestGitHubActivitySkipsUnchanged(t *testing.T) {
	c, queries := activityClient(t, map[string]bool{"app#84": true})
	res, err := hostOf(t, c, "github").Activity(context.Background(), cursors("app#84", "app#85", "api#7"))
	if err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 2 {
		t.Fatalf("want one change check and one timeline read, got %d requests", len(*queries))
	}
	if len(res.Updated) != 3 || !res.Updated["app#84"].Equal(activityNow.Add(-time.Minute)) {
		t.Fatalf("every PR's update time is returned for its next cursor: %v", res.Updated)
	}
	for _, e := range res.Events {
		if e.PR != "app#84" {
			t.Fatalf("event for an unchanged PR: %+v", e)
		}
	}
}

func TestGitHubActivityBatches(t *testing.T) {
	var ids []string
	for i := 1; i <= 150; i++ {
		ids = append(ids, fmt.Sprintf("app#%d", i))
	}
	c, queries := activityClient(t, nil)
	res, err := hostOf(t, c, "github").Activity(context.Background(), cursors(ids...))
	if err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 2 {
		t.Fatalf("150 PRs: want 2 change checks and no timelines, got %d requests", len(*queries))
	}
	if len(res.Updated) != 150 || len(res.Events) != 0 {
		t.Fatalf("got %d updated, %d events", len(res.Updated), len(res.Events))
	}
}

func TestGitHubActivityMapsKinds(t *testing.T) {
	c, _ := activityClient(t, map[string]bool{"app#84": true})
	res, err := hostOf(t, c, "github").Activity(context.Background(), cursors("app#84"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[workfile.ActivityKind]workfile.Activity{}
	for _, e := range res.Events {
		if e.Connection != "github" || e.PR != "app#84" {
			t.Fatalf("event not tied to its connection and PR: %+v", e)
		}
		got[e.Kind] = e
	}
	if len(res.Events) != 6 {
		t.Fatalf("want 6 events newer than the cursor, got %d: %+v", len(res.Events), res.Events)
	}
	if e := got[workfile.ActivityComment]; e.WhoID != "example-bob" || e.Text != "Can we keep return_to?" {
		t.Errorf("comment: %+v", e)
	}
	if e := got[workfile.ActivityApproved]; e.WhoID != "example-charlie" {
		t.Errorf("approval: %+v", e)
	}
	if e := got[workfile.ActivityChangesRequested]; e.Where != "auth/redirect.go:41" || e.Text != "This drops return_to" {
		t.Errorf("changes requested should carry its first comment: %+v", e)
	}
	if e := got[workfile.ActivityReview]; e.Text != "Looks close" {
		t.Errorf("commented review: %+v", e)
	}
	if e := got[workfile.ActivityReviewRequested]; e.WhoID != "example-alice" {
		t.Errorf("review request is by its actor: %+v", e)
	}
	if e := got[workfile.ActivityPushed]; e.WhoID != "example-bob" {
		t.Errorf("push: %+v", e)
	}
}

func TestGitHubMineFiltersRepos(t *testing.T) {
	var searches []string
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		q := body["variables"].(map[string]any)["q"].(string)
		searches = append(searches, q)
		node := func(repo string, n int) string {
			return fmt.Sprintf(`{"id":"pr-%s-%d","number":%d,"title":"PR","url":"https://github.example.com/%s/%d","state":"OPEN","isDraft":false,"headRefOid":"h","mergeable":"MERGEABLE","repository":{"name":"%s"},"author":{"login":"example-alice"},"labels":{"nodes":[],"pageInfo":{}},"latestOpinionatedReviews":{"nodes":[],"pageInfo":{}},"reviewRequests":{"nodes":[],"pageInfo":{}},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}`, repo, n, n, repo, n, repo)
		}
		return 200, `{"data":{"search":{"issueCount":2,"pageInfo":{"hasNextPage":false},"nodes":[` + node("app", 1) + `,` + node("example-other", 2) + `]}}}`
	})
	records, err := hostOf(t, c, "github").InboxPRs(context.Background(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(searches) != 2 || !strings.Contains(searches[0], "author:@me") || !strings.Contains(searches[1], "review-requested:@me") || !strings.Contains(searches[0], "org:example-team") {
		t.Fatalf("want two searches scoped to the org: %q", searches)
	}
	if len(records) != 1 || records[0].ID != "app#1" {
		t.Fatalf("PRs outside the listed repos must be dropped, and duplicates merged: %+v", records)
	}
}

func TestJiraActivitySince(t *testing.T) {
	calls := 0
	c := jiraClient(t, func(r *http.Request) (int, string) {
		calls++
		jql := requestBody(t, r)["jql"].(string)
		// Absolute JQL dates are read in the person's own time zone, so ask in
		// minutes relative to now: 60 since the cursor, plus 2 to spare.
		if !strings.Contains(jql, `key in ("APP-1")`) || !strings.Contains(jql, `updated >= "-62m"`) {
			t.Errorf("wrong JQL %s", jql)
		}
		return 200, `{"issues":[{"key":"APP-1","fields":{"comment":{"total":2,"comments":[
 {"author":{"displayName":"Bob Example","accountId":"bob-id"},"created":"2026-09-30T23:30:00.000+0000","body":{"type":"doc","content":[]}},
 {"author":{"displayName":"Bob Example","accountId":"bob-id"},"created":"2026-09-29T10:00:00.000+0000","body":{"type":"doc","content":[]}},
 {"author":{"displayName":"Bob Example","accountId":"bob-id"},"created":"2026-09-30T22:59:30.000+0000","body":{"type":"doc","content":[]}}
]}}}],"isLast":true}`
	})
	tr := trackerOf(t, c)
	events, err := tr.Activity(context.Background(), []string{"APP-1"}, activityNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Ticket != "APP-1" || events[0].WhoID != "bob-id" || events[0].Who != "Bob Example" || events[0].Kind != workfile.ActivityComment {
		t.Fatalf("want the newer comment, and one indexed late just before the cursor: %+v", events)
	}
	if events, err := tr.Activity(context.Background(), nil, activityNow); err != nil || len(events) != 0 || calls != 1 {
		t.Fatalf("no tickets must make no request: %v %v calls=%d", events, err, calls)
	}
}

func TestClientActivityPartial(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/_edge/tenant_info" {
			return 200, `{"cloudId":"cloud-example"}`
		}
		if strings.Contains(r.URL.Host, "github") {
			return 502, `bad gateway`
		}
		return 200, `{"issues":[{"key":"APP-1","fields":{"comment":{"total":1,"comments":[{"author":{"displayName":"Bob Example","accountId":"bob-id"},"created":"2026-09-30T23:30:00.000+0000","body":{"type":"doc","content":[]}}]}}}],"isLast":true}`
	})
	res, err := c.Activity(context.Background(), workfile.ActivityQuery{Tickets: []string{"APP-1"}, PRs: cursors("app#84")}, activityNow.Add(-time.Hour))
	if err == nil || !strings.Contains(err.Error(), "github") {
		t.Fatalf("want an error naming the github connection, got %v", err)
	}
	if strings.Contains(err.Error(), "bad gateway") {
		t.Fatal("provider response bodies must never be shown")
	}
	if len(res.Events) != 1 || res.Events[0].Ticket != "APP-1" {
		t.Fatalf("Jira events still returned when GitHub fails: %+v", res.Events)
	}
}

func TestGitHubTimelineReadsNewest(t *testing.T) {
	if !strings.Contains(timelineQuery, "timelineItems(since: $since, last: 50") {
		t.Fatal("a busy PR must keep its newest events, not its oldest")
	}
}
