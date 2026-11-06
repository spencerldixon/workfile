package provider

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func jiraClient(t *testing.T, handle func(*http.Request) (int, string)) *Client {
	t.Helper()
	return client(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/_edge/tenant_info" {
			return 200, `{"cloudId":"cloud-example"}`
		}
		return handle(r)
	})
}

func TestListingAsksOnlyForWhatItShows(t *testing.T) {
	calls := 0
	c := jiraClient(t, func(r *http.Request) (int, string) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/rest/api/3/search/jql") {
			t.Errorf("unexpected read %s", r.URL.Path)
		}
		body := requestBody(t, r)
		var fields []string
		for _, f := range body["fields"].([]any) {
			fields = append(fields, f.(string))
		}
		slices.Sort(fields)
		if strings.Join(fields, " ") != "assignee issuetype status summary" {
			t.Errorf("fields %v", fields)
		}
		return 200, `{"isLast":true,"issues":[
			{"id":"1","key":"APP-1","fields":{"summary":"First","status":{"name":"In Review"},"assignee":{"accountId":"example-alice","displayName":"Alice"},"issuetype":{"name":"Bug"}}},
			{"id":"2","key":"APP-2","fields":{"summary":"Second","status":{"name":"To Do"},"assignee":null,"issuetype":{"name":"Story"}}}]}`
	})
	got, err := c.Listing(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got) != 2 {
		t.Fatalf("%d calls, %+v", calls, got)
	}
	want := workfile.Listing{Key: "APP-1", Title: "First", Status: "In Review", State: "review", Assignee: "Alice", AssigneeID: "example-alice", Type: "Bug", URL: "https://" + c.Workspace.Providers[c.Workspace.Policy.Tracker].Site + "/browse/APP-1"}
	if got[0] != want {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Assignee != "" {
		t.Fatalf("%+v", got[1])
	}
}

func TestDetailReadsTextCommentsAndMatchesImages(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/rest/api/3/issue/APP-7/comment") {
			return 200, `{"total":3,"comments":[{"author":{"displayName":"Alice"},"created":"2026-10-01T10:00:00.000+0000",
				"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good"}]}]}}]}`
		}
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/rest/api/3/issue/APP-7") {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if fields := r.URL.Query().Get("fields"); fields != "description,comment,attachment" {
			t.Errorf("fields %q", fields)
		}
		return 200, `{"key":"APP-7","fields":{
			"description":{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"Hello"}]},
				{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"media-1","alt":"login-mock.png"}}]},
				{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"media-2","alt":"gone.png"}}]}]},
			"comment":{"total":3,"comments":[{"author":{"displayName":"Alice"},"created":"2026-10-01T10:00:00.000+0000",
				"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good"}]}]}}]},
			"attachment":[{"id":"10042","filename":"login-mock.png","mimeType":"image/png","size":2048}]}}`
	})
	d, err := c.Detail(context.Background(), "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Body) != 3 || d.Body[1].Attachment != "10042" || d.Body[2].Attachment != "" || d.Body[2].Name != "gone.png" {
		t.Fatalf("%+v", d.Body)
	}
	if d.CommentTotal != 3 || len(d.Comments) != 1 || d.Comments[0].Author != "Alice" || spanText(d.Comments[0].Body[0].Spans) != "Looks good" {
		t.Fatalf("%+v", d)
	}
	if want := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !d.Comments[0].Created.Equal(want) {
		t.Fatalf("%v", d.Comments[0].Created)
	}
}

func TestDetailOfAnEmptyTicket(t *testing.T) {
	c := jiraClient(t, func(*http.Request) (int, string) {
		return 200, `{"key":"APP-8","fields":{"description":null,"comment":{"total":0,"comments":[]},"attachment":[]}}`
	})
	d, err := c.Detail(context.Background(), "APP-8")
	if err != nil || len(d.Body) != 0 || len(d.Comments) != 0 {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestImageIsBuiltFromTheIDAlone(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/rest/api/3/attachment/content/10042") {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("redirect") != "false" {
			t.Errorf("Jira would redirect, and this client follows no redirects: %q", r.URL.RawQuery)
		}
		return 200, "image bytes"
	})
	data, err := c.Image(context.Background(), "10042")
	if err != nil || string(data) != "image bytes" {
		t.Fatalf("%q %v", data, err)
	}
	for _, id := range []string{"", "../../myself", "https://example.com/x", "1/2"} {
		if _, err := c.Image(context.Background(), id); err == nil {
			t.Errorf("%q: want an error", id)
		}
	}
}

func TestImageReportsAMissingFileAndRefusesAHugeOne(t *testing.T) {
	status, body := 404, ""
	c := jiraClient(t, func(*http.Request) (int, string) { return status, body })
	if _, err := c.Image(context.Background(), "1"); err == nil {
		t.Fatal("want an error for a missing file")
	}
	status, body = 200, strings.Repeat("x", (16<<20)+1)
	if _, err := c.Image(context.Background(), "1"); err == nil || !strings.Contains(err.Error(), "16 MB") {
		t.Fatalf("%v", err)
	}
}

func TestImageNeverFollowsARedirect(t *testing.T) {
	c := jiraClient(t, func(*http.Request) (int, string) { return 200, "" })
	inner := c.HTTP.Transport
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/attachment/content/") {
			return &http.Response{StatusCode: 303, Header: http.Header{"Location": {"https://media.example.com/file"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.URL.Host == "media.example.com" {
			t.Error("the download followed a redirect to another host")
		}
		return inner.RoundTrip(r)
	})
	if _, err := c.Image(context.Background(), "1"); err == nil {
		t.Fatal("want an error when Jira redirects")
	}
}

func TestListingReadsEveryoneEvenWithOnlyMine(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if jql := requestBody(t, r)["jql"].(string); strings.Contains(jql, "assignee") {
			t.Errorf("the list needs other people's and unassigned tickets: %q", jql)
		}
		return 200, `{"isLast":true,"issues":[]}`
	})
	c.Workspace.Settings.OnlyMine = true
	if _, err := c.Listing(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestListingTakesAScope(t *testing.T) {
	var jql string
	c := jiraClient(t, func(r *http.Request) (int, string) {
		jql = requestBody(t, r)["jql"].(string)
		return 200, `{"isLast":true,"issues":[]}`
	})
	if _, err := c.Listing(context.Background(), "project = EXAMPLE ORDER BY rank"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jql, "project = EXAMPLE") || !strings.HasSuffix(jql, "ORDER BY rank") {
		t.Fatalf("%q", jql)
	}
	if _, err := c.Listing(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jql, "project = EXAMPLE ") {
		t.Fatalf("%q", jql)
	}
}

func TestAnUnknownTrackerSaysSo(t *testing.T) {
	c := jiraClient(t, func(*http.Request) (int, string) {
		t.Error("nothing should be read")
		return 200, ""
	})
	p := c.Workspace.Providers[c.Workspace.Policy.Tracker]
	p.Kind = "linear"
	c.Workspace.Providers[c.Workspace.Policy.Tracker] = p
	if _, err := c.Listing(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "linear") {
		t.Fatalf("%v", err)
	}
}

func jiraComment(who, created string) string {
	return `{"author":{"displayName":"` + who + `"},"created":"` + created + `","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"from ` + who + `"}]}]}}`
}

func commentAuthors(comments []workfile.Comment) string {
	var names []string
	for _, c := range comments {
		names = append(names, c.Author)
	}
	return strings.Join(names, " ")
}

func TestDetailPutsTheNewestCommentFirst(t *testing.T) {
	reads := 0
	c := jiraClient(t, func(*http.Request) (int, string) {
		reads++
		return 200, `{"fields":{"comment":{"total":3,"comments":[` + jiraComment("Alice", "2026-10-01T10:00:00.000+0000") + `,` + jiraComment("Charlie", "not a date") + `,` + jiraComment("Bob", "2026-10-03T10:00:00.000+0000") + `]}}}`
	})
	d, err := c.Detail(context.Background(), "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	if got := commentAuthors(d.Comments); got != "Bob Alice Charlie" {
		t.Fatal(got)
	}
	if reads != 1 {
		t.Fatalf("all the comments were there, but %d reads", reads)
	}
}

func TestDetailFetchesTheNewestWhenJiraReturnsOnlySome(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/rest/api/3/issue/APP-7/comment") {
			if r.Method != "GET" || r.URL.Query().Get("orderBy") != "-created" {
				t.Errorf("%s %s", r.Method, r.URL.RawQuery)
			}
			return 200, `{"total":3,"comments":[` + jiraComment("Charlie", "2026-10-05T10:00:00.000+0000") + `,` + jiraComment("Bob", "2026-10-03T10:00:00.000+0000") + `]}`
		}
		return 200, `{"fields":{"comment":{"total":3,"comments":[` + jiraComment("Alice", "2026-10-01T10:00:00.000+0000") + `,` + jiraComment("Bob", "2026-10-03T10:00:00.000+0000") + `]}}}`
	})
	d, err := c.Detail(context.Background(), "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	if got := commentAuthors(d.Comments); got != "Charlie Bob" || d.CommentTotal != 3 {
		t.Fatalf("%s of %d", got, d.CommentTotal)
	}
}

func TestDetailKeepsWhatItHasWhenTheNewestCannotBeRead(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/comment") {
			return 500, ""
		}
		return 200, `{"fields":{"description":"Still here","comment":{"total":2,"comments":[` + jiraComment("Alice", "2026-10-01T10:00:00.000+0000") + `]}}}`
	})
	d, err := c.Detail(context.Background(), "APP-7")
	if err != nil || len(d.Body) != 1 || commentAuthors(d.Comments) != "Alice" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestDetailKeepsWhatItHasWhenTheNewestComeBackEmpty(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/comment") {
			return 200, `{"total":2,"comments":[]}`
		}
		return 200, `{"fields":{"comment":{"total":2,"comments":[` + jiraComment("Alice", "2026-10-01T10:00:00.000+0000") + `]}}}`
	})
	d, err := c.Detail(context.Background(), "APP-7")
	if err != nil || commentAuthors(d.Comments) != "Alice" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestRepliesStayInConversationOrder(t *testing.T) {
	old, late := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	d := workfile.Detail{Comments: []workfile.Comment{
		{Author: "Alice", Created: old, Replies: []workfile.Comment{{Author: "Bob", Created: old}, {Author: "Charlie", Created: late}}},
		{Author: "Bob", Created: late},
	}}
	newestFirst(d.Comments)
	if got := commentAuthors(d.Comments); got != "Bob Alice" {
		t.Fatal(got)
	}
	if got := commentAuthors(d.Comments[1].Replies); got != "Bob Charlie" {
		t.Fatal(got)
	}
}
