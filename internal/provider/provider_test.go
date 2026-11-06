package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"workfile/internal/workfile"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func client(t *testing.T, handle func(*http.Request) (int, string)) *Client {
	t.Helper()
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXAMPLE_JIRA_EMAIL", "reader@example.com")
	t.Setenv("EXAMPLE_JIRA_TOKEN", "test-jira-token")
	t.Setenv("EXAMPLE_GITHUB_TOKEN", "test-github-token")
	c := New(w)
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		status, body := handle(r)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	c.Now = func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	return c
}

func requestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return body
}

func TestJiraPaginationScopeAndLabelHistory(t *testing.T) {
	searchCalls, scopedCalls, historyCalls := 0, 0, 0
	c := client(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/_edge/tenant_info" {
			if r.Header.Get("Authorization") != "" {
				t.Error("credentials sent during public discovery")
			}
			return 200, `{"cloudId":"cloud-example"}`
		}
		if r.URL.Host != "api.atlassian.com" {
			t.Errorf("authenticated request went to %s", r.URL.Host)
		}
		email, token, ok := r.BasicAuth()
		if !ok || email != "reader@example.com" || token != "test-jira-token" {
			t.Error("wrong basic auth")
		}
		body := requestBody(t, r)
		switch r.URL.Path {
		case "/ex/jira/cloud-example/rest/api/3/search/jql":
			jql := body["jql"].(string)
			if strings.Contains(jql, "project = APP") {
				// The scoped check that finds named tickets the board would hide.
				scopedCalls++
				if !strings.HasSuffix(jql, `AND key in ("APP-1", "APP-2")`) {
					t.Errorf("wrong scoped query %s", jql)
				}
				return 200, `{"issues":[{"id":"1","key":"APP-1","fields":{}}],"isLast":true}`
			}
			searchCalls++
			if jql != `key in ("APP-1", "APP-2") ORDER BY updated DESC` {
				t.Errorf("a named ticket must skip the scope, got %s", jql)
			}
			if strings.Contains(jql, "status in") {
				t.Error("status must include finished and unmapped stages")
			}
			if searchCalls == 1 {
				return 200, `{"issues":[{"id":"1","key":"APP-1","fields":{"summary":"A ticket","description":"Details","status":{"name":"In Review"},"labels":["approved"],"assignee":{"accountId":"bob","displayName":"Bob"},"issuetype":{"name":"Bug"}}}],"nextPageToken":"second","isLast":false}`
			}
			if body["nextPageToken"] != "second" {
				t.Error("missing search cursor")
			}
			return 200, `{"issues":[{"id":"2","key":"APP-2","fields":{"summary":"Other ticket","status":{"name":"Unmapped"}}}],"isLast":true}`
		case "/ex/jira/cloud-example/rest/api/3/changelog/bulkfetch":
			historyCalls++
			if historyCalls == 1 {
				return 200, `{"issueChangeLogs":[{"issueId":"1","changeHistories":[{"created":"2026-09-03T00:00:00Z","author":{"accountId":"alice","displayName":"Alice"},"items":[{"fieldId":"labels","fromString":"","toString":"approved"}]}]}],"nextPageToken":"older"}`
			}
			if body["nextPageToken"] != "older" {
				t.Error("missing history cursor")
			}
			return 200, `{"issueChangeLogs":[{"issueId":"1","changeHistories":[{"created":"2026-09-01T00:00:00Z","author":{"accountId":"bob"},"items":[{"fieldId":"labels","fromString":"","toString":"approved"}]},{"created":"2026-09-02T00:00:00Z","author":{"accountId":"bob"},"items":[{"fieldId":"labels","fromString":"approved","toString":""}]}]}]}`
		}
		t.Errorf("unexpected request %s", r.URL.Path)
		return 500, `{}`
	})
	c.Workspace.Policy.Gates["sign_off"] = []workfile.Rule{{Condition: workfile.Expression{Provider: "jira", Fact: "labels"}, By: []string{"alice"}}}
	tickets, err := c.Tickets(context.Background(), []string{"APP-1", "APP-2"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if searchCalls != 2 || scopedCalls != 1 || historyCalls != 2 || len(tickets) != 2 {
		t.Fatalf("calls %d/%d/%d, tickets %d", searchCalls, scopedCalls, historyCalls, len(tickets))
	}
	if tickets[0].OutOfScope || !tickets[1].OutOfScope {
		t.Fatalf("out of scope flags %v %v", tickets[0].OutOfScope, tickets[1].OutOfScope)
	}
	if tickets[0].State != "review" || tickets[0].Assignee != "bob" || tickets[1].Error == "" {
		t.Fatalf("bad mapping %+v", tickets)
	}
	if actor := tickets[0].Facts["labels"].([]workfile.Actor)[0]; actor.ID != "alice" {
		t.Fatalf("wrong latest label actor %+v", actor)
	}
}

func TestJiraOnlyMineNarrowsListings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var jql string
	c := client(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/_edge/tenant_info" {
			return 200, `{"cloudId":"cloud-example"}`
		}
		jql = requestBody(t, r)["jql"].(string)
		return 200, `{"issues":[],"isLast":true}`
	})
	condition, order := splitScope(c.Workspace.Providers["jira"].Scope)
	c.Workspace.Settings.OnlyMine = true
	if _, err := c.Tickets(context.Background(), nil, 0); err != nil {
		t.Fatal(err)
	}
	if want := "((" + condition + ") AND assignee = currentUser()) ORDER BY " + order; jql != want {
		t.Fatalf("got %s\nwant %s", jql, want)
	}
}

func TestJiraEmptyScopeAndRepeatedCursor(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			c := client(t, func(r *http.Request) (int, string) {
				if r.URL.Path == "/_edge/tenant_info" {
					return 200, `{"cloudId":"example"}`
				}
				if empty {
					return 200, `{"issues":[],"isLast":true}`
				}
				return 200, `{"issues":[],"nextPageToken":"same"}`
			})
			tickets, err := c.Tickets(context.Background(), nil, 0)
			if empty && (err != nil || len(tickets) != 0) {
				t.Fatalf("%v %v", tickets, err)
			}
			if !empty && (err == nil || !strings.Contains(err.Error(), "repeated")) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestADFTextAndHeadings(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","content":[{"type":"heading","content":[{"type":"text","text":"Acceptance criteria"}]},{"type":"paragraph","content":[{"type":"text","text":"Context","marks":[{"type":"strong"}]}]},{"type":"paragraph","content":[{"type":"mention","attrs":{"text":"@Bob"}},{"type":"text","text":" can sign in."}]}]}`)
	text, headings := description(raw)
	if text != "Acceptance criteria\nContext\n@Bob can sign in." || !slices.Equal(headings, []string{"Acceptance criteria", "Context"}) {
		t.Fatalf("%q %v", text, headings)
	}
}

func TestGitHubPaginationLinkingAndApprovals(t *testing.T) {
	var mu sync.Mutex
	repoCalls, searchCalls, detailCalls := 0, 0, 0
	c := client(t, func(r *http.Request) (int, string) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-github-token" {
			t.Error("missing GitHub token")
		}
		body := requestBody(t, r)
		query := body["query"].(string)
		variables, _ := body["variables"].(map[string]any)
		switch {
		case strings.Contains(query, "organization(login"):
			repoCalls++
			t.Error("a report must not list the organisation's repositories")
		case strings.Contains(query, "search(query"):
			searchCalls++
			if strings.Contains(query, "labels(") || strings.Contains(query, "latestOpinionatedReviews(") || strings.Contains(query, "reviewRequests(") {
				t.Error("discovery fetched expensive PR facts")
			}
			if variables["q"] != `org:example-team is:pr archived:false in:title,body "APP-12"` {
				t.Errorf("wrong search %v", variables)
			}
			if searchCalls == 1 {
				return 200, `{"data":{"search":{"issueCount":3,"pageInfo":{"hasNextPage":true,"endCursor":"prs2"},"nodes":[{"id":"pr1","title":"APP-12 Improve login","headRefName":"feature/app-12","state":"OPEN"}]}}}`
			}
			if variables["after"] != "prs2" {
				t.Error("missing PR cursor")
			}
			return 200, `{"data":{"search":{"issueCount":3,"pageInfo":{"hasNextPage":false},"nodes":[{"number":2,"title":"APP-123 not APP-12x","state":"MERGED","repository":{"name":"api"}},{"number":3,"title":"APP-12 closed","state":"CLOSED","repository":{"name":"api"}}]}}}`
		case strings.Contains(query, "node(id"):
			detailCalls++
			if variables["id"] != "pr1" {
				t.Error("fetched an unrelated PR")
			}
			if detailCalls == 1 {
				return 200, `{"data":{"node":{"id":"pr1","number":1,"title":"APP-12 Improve login","headRefName":"feature/app-12","state":"OPEN","url":"https://github.com/example-team/api/pull/1","repository":{"name":"api"},"author":{"login":"bob"},"labels":{"nodes":[{"name":"high-risk"}]},"latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"bob"}}],"pageInfo":{"hasNextPage":true,"endCursor":"reviews2"}},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"alice"}}]}}}}`
			}
			if variables["reviews"] != "reviews2" {
				t.Error("missing nested review cursor")
			}
			return 200, `{"data":{"node":{"latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"alice"}}],"pageInfo":{"hasNextPage":false}}}}}`
		}
		t.Errorf("unexpected query %s", query)
		return 400, `{}`
	})
	items, notes, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-12"}})
	if err != nil {
		t.Fatal(err)
	}
	records := items[0].Records["github"]
	if repoCalls != 0 || searchCalls != 2 || detailCalls != 2 || len(notes) != 0 || len(records) != 1 {
		t.Fatalf("calls %d/%d/%d, records %+v, notes %v", repoCalls, searchCalls, detailCalls, records, notes)
	}
	if records[0].Repository != "example-team/api" {
		t.Fatalf("missing repository owner/name: %q", records[0].Repository)
	}
	if got := records[0].Facts["approvals"].([]workfile.Actor); len(got) != 1 || got[0].ID != "alice" {
		t.Fatalf("wrong approvals %+v", got)
	}
	if !slices.Equal(records[0].People, []string{"bob", "alice"}) {
		t.Fatal("author/reviewer filter identities missing")
	}
}

func TestGitHubSearchCapAndGraphQLErrors(t *testing.T) {
	for _, body := range []string{`{"data":{"search":{"issueCount":1001,"nodes":[]}}}`, `{"errors":[{"message":"secret-sentinel"}],"data":{"search":{"nodes":[]}}}`, `{"data":null}`} {
		c := client(t, func(*http.Request) (int, string) { return 200, body })
		_, err := c.searchPRs(context.Background(), "token", "org:example-team is:pr", false)
		if err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatalf("wrong error %v", err)
		}
	}
}

func TestGitHubSearchesByKeyInBatches(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		if strings.Contains(body["query"].(string), "node(id") {
			return 200, `{"data":{"node":{"id":"pr1","number":1,"title":"APP-1 and APP-7","state":"OPEN","repository":{"name":"api"},"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}`
		}
		mu.Lock()
		queries = append(queries, body["variables"].(map[string]any)["q"].(string))
		mu.Unlock()
		// The same PR mentions a ticket from each batch.
		return 200, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"pr1","title":"APP-1 and APP-7","state":"OPEN"}]}}}`
	})
	var tickets []workfile.Ticket
	for i := 1; i <= 7; i++ {
		tickets = append(tickets, workfile.Ticket{Key: fmt.Sprintf("APP-%d", i)})
	}
	items, _, err := c.Records(context.Background(), tickets)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(queries)
	want := []string{
		`org:example-team is:pr archived:false in:title,body "APP-1" OR "APP-2" OR "APP-3" OR "APP-4" OR "APP-5" OR "APP-6"`,
		`org:example-team is:pr archived:false in:title,body "APP-7"`,
	}
	if !slices.Equal(queries, want) {
		t.Fatalf("searches %q", queries)
	}
	for _, item := range items {
		linked := item.Key == "APP-1" || item.Key == "APP-7"
		if got := len(item.Records["github"]); linked && got != 1 || !linked && got != 0 {
			t.Fatalf("%s: %d records", item.Key, got)
		}
	}
}

func TestProviderErrorsDoNotExposeBodies(t *testing.T) {
	for _, status := range []int{301, 400, 401, 403, 404} {
		c := client(t, func(*http.Request) (int, string) { return status, `{"message":"secret-sentinel"}` })
		var result any
		err := c.request(context.Background(), "GET", "https://example.com", "", nil, &result)
		if err == nil || strings.Contains(err.Error(), "secret-sentinel") || !strings.Contains(err.Error(), fmt.Sprint(status)) {
			t.Fatalf("%v", err)
		}
	}
	c := client(t, func(*http.Request) (int, string) { return 200, `<html>secret-sentinel</html>` })
	var result any
	if err := c.request(context.Background(), "GET", "https://example.com", "", nil, &result); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("%v", err)
	}
}

func TestCancellationInterruptsRetryAndWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := client(t, func(*http.Request) (int, string) { cancel(); return 503, `{}` })
	var result any
	start := time.Now()
	if err := c.request(ctx, "GET", "https://example.com", "", nil, &result); err != context.Canceled {
		t.Fatalf("%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not interrupt backoff")
	}
	_, err := parallel(ctx, 100, func(context.Context, int) (int, error) { t.Error("cancelled worker ran"); return 0, nil })
	if err != context.Canceled {
		t.Fatalf("%v", err)
	}
}

func TestBoundedParallelism(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	results, err := parallel(context.Background(), 20, func(_ context.Context, i int) (int, error) {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return i * i, nil
	})
	if err != nil || peak > 6 {
		t.Fatalf("%v, peak %d", err, peak)
	}
	for i, result := range results {
		if result != i*i {
			t.Fatal("results lost their input order")
		}
	}
}

func TestScopeOrderingIgnoresQuotedText(t *testing.T) {
	for _, tc := range []struct{ input, condition, order string }{
		{`project = APP`, `project = APP`, `updated DESC`},
		{`project = APP ORDER BY priority DESC`, `project = APP`, `priority DESC`},
		{`summary ~ "an ORDER BY clause" ORDER BY updated ASC`, `summary ~ "an ORDER BY clause"`, `updated ASC`},
		{`summary ~ 'an ORDER BY clause'`, `summary ~ 'an ORDER BY clause'`, `updated DESC`},
	} {
		condition, order := splitScope(tc.input)
		if condition != tc.condition || order != tc.order {
			t.Fatalf("%q -> %q / %q", tc.input, condition, order)
		}
	}
}

func TestConnectionChecksReadIdentityAndScope(t *testing.T) {
	var paths []string
	c := client(t, func(r *http.Request) (int, string) {
		paths = append(paths, r.URL.Path)
		switch {
		case r.URL.Path == "/_edge/tenant_info":
			return 200, `{"cloudId":"example"}`
		case strings.HasSuffix(r.URL.Path, "/myself"):
			return 200, `{"accountId":"example-bob"}`
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			body := requestBody(t, r)
			if body["maxResults"] != float64(1) {
				t.Error("connection check must read at most one ticket")
			}
			return 200, `{"issues":[{"id":"1","key":"APP-1","fields":{"status":{"name":"To Do"}}}],"isLast":true}`
		case r.URL.Path == "/graphql":
			body := requestBody(t, r)
			if strings.HasPrefix(body["query"].(string), "query { viewer {") {
				return 200, `{"data":{"viewer":{"login":"example-bob"}}}`
			}
			if strings.Contains(body["query"].(string), "organization(login") {
				return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"api"}]}}}}`
			}
			if body["variables"].(map[string]any)["first"] != float64(1) {
				t.Error("connection check must read at most one PR")
			}
			return 200, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
			return 400, `{}`
		}
	})
	for _, name := range []string{"jira", "github"} {
		message, err := c.Test(context.Background(), name)
		if err != nil || !strings.Contains(message, "example-bob") {
			t.Fatalf("%s: %q %v", name, message, err)
		}
	}
	if len(paths) != 6 {
		t.Fatalf("unexpected calls %v", paths)
	}
	for _, path := range paths {
		if strings.Contains(path, "changelog") {
			t.Error("read label history despite no actor rules")
		}
	}
}

func TestUnknownKindDoesNotFallThroughToGitHub(t *testing.T) {
	calls := 0
	c := client(t, func(*http.Request) (int, string) { calls++; return 200, `{}` })
	if _, err := c.Identity(context.Background(), "missing"); err == nil {
		t.Fatal("an unknown connection must be an error")
	}
	p := c.Workspace.Providers["github"]
	p.Kind = "unknown"
	c.Workspace.Providers["github"] = p
	if _, err := c.Identity(context.Background(), "github"); err == nil {
		t.Fatal("an unknown kind must be an error")
	}
	if calls != 0 {
		t.Fatalf("made %d requests", calls)
	}
}

func TestDownloadRefusesLargeImages(t *testing.T) {
	c := client(t, func(*http.Request) (int, string) { return 200, strings.Repeat("x", (16<<20)+1) })
	_, err := c.download(context.Background(), "https://example.com/a.png", "")
	if err == nil || !strings.Contains(err.Error(), "image exceeds 16 MB") {
		t.Fatalf("%v", err)
	}
}

func TestAssigneePrefersTheConfiguredAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c := client(t, func(*http.Request) (int, string) { return 200, `{}` })
	j := jiraTracker{Client: c, name: c.Workspace.Policy.Tracker}
	if who, err := j.assignee(); err != nil || who != "currentUser()" {
		t.Fatalf("got %q, %v", who, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "workfile"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workfile", "config.yml"), []byte("me:\n  jira: \"acc-1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if who, err := j.assignee(); err != nil || who != `"acc-1"` {
		t.Fatalf("got %q, %v", who, err)
	}
}
