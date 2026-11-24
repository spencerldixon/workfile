package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"workfile/internal/workfile"
)

func TestNamedTicketUnionsTextAndBranchSearches(t *testing.T) {
	var mu sync.Mutex
	var searches, reads []string
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		vars := body["variables"].(map[string]any)
		mu.Lock()
		defer mu.Unlock()
		if q, ok := vars["q"].(string); ok {
			searches = append(searches, q)
			if strings.Contains(q, `head:"APP-12"`) {
				return 200, `{"data":{"search":{"issueCount":3,"nodes":[
					{"id":"1","title":"Fix form","headRefName":"APP-12-form","state":"OPEN"},
					{"id":"2","title":"Fix preview","headRefName":"APP-12-preview","state":"OPEN"},
					{"id":"3","title":"Other ticket","headRefName":"APP-123-preview","state":"OPEN"}
				]}}}`
			}
			return 200, `{"data":{"search":{"issueCount":1,"nodes":[
				{"id":"1","title":"Fix form","body":"https://example-team.atlassian.net/browse/APP-12","state":"OPEN"}
			]}}}`
		}
		id := vars["id"].(string)
		reads = append(reads, id)
		return 200, fmt.Sprintf(`{"data":{"node":{"id":%q,"number":%s,"state":"OPEN","repository":{"name":"app"},"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]},"reviewThreads":{"nodes":[{"isResolved":%t}]}}}}`, id, id, id == "1")
	})
	tickets, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-12"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(searches)
	slices.Sort(reads)
	if len(searches) != 2 || !strings.Contains(searches[0], `head:"APP-12"`) || !strings.Contains(searches[1], `in:title,body "APP-12"`) || !slices.Equal(reads, []string{"1", "2"}) || len(tickets[0].Records["github"]) != 2 {
		t.Fatalf("searches %v, detail reads %v, tickets %+v", searches, reads, tickets)
	}
	if records := tickets[0].Records["github"]; records[0].UnresolvedReviewThreads || !records[1].UnresolvedReviewThreads {
		t.Fatalf("review threads %+v", records)
	}
}

const refPR = `{"data":{"repository":{"pullRequest":{"id":"pr7","number":7,"title":"Change","state":"OPEN","url":"https://github.com/example-team/app/pull/7","repository":{"name":"app"},"author":{"login":"bob"},"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}}`

// linkServer answers reads by reference and searches, and records every query.
func linkServer(t *testing.T, searchBody string) (*Client, *[]string) {
	var mu sync.Mutex
	var queries []string
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(query, "search(query") {
			queries = append(queries, body["variables"].(map[string]any)["q"].(string))
			return 200, searchBody
		}
		queries = append(queries, query)
		if strings.Contains(query, "repository(owner") {
			return 200, refPR
		}
		return 200, `{"data":{"node":{"id":"pr1","number":1,"title":"APP-2 work","state":"OPEN","repository":{"name":"app"},"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}`
	})
	return c, &queries
}

func searches(queries []string) int {
	n := 0
	for _, q := range queries {
		if strings.HasPrefix(q, "org:") {
			n++
		}
	}
	return n
}

func TestLinkedPRsAreReadWithNoSearch(t *testing.T) {
	c, queries := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := items[0].Records["github"]
	if len(got) != 1 || got[0].ID != "app#7" || got[0].Repository != "example-team/app" {
		t.Fatalf("records %+v", got)
	}
	if searches(*queries) != 0 {
		t.Fatalf("searched: %q", *queries)
	}
}

func TestTicketsWithoutLinksStillSearch(t *testing.T) {
	c, queries := linkServer(t, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"pr1","title":"APP-2 work","state":"OPEN"}]}}}`)
	items, _, err := c.Records(context.Background(), []workfile.Ticket{
		{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}},
		{Key: "APP-2"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, q := range *queries {
		if strings.HasPrefix(q, "org:") {
			found = append(found, q)
		}
	}
	if len(found) != 1 || !strings.Contains(found[0], `"APP-2"`) || strings.Contains(found[0], "APP-1") {
		t.Fatalf("searches %q", found)
	}
	if len(items[0].Records["github"]) != 1 || len(items[1].Records["github"]) != 1 {
		t.Fatalf("records %+v / %+v", items[0].Records, items[1].Records)
	}
}

func TestLinkFromAnotherOrgIsIgnored(t *testing.T) {
	c, queries := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-1", LinkedPRs: []string{"other-org/app#7"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 0 || len(items[0].Records["github"]) != 0 {
		t.Fatalf("queries %q, records %+v", *queries, items[0].Records)
	}
}

func TestLinkIgnoresTheCaseOfTheOrg(t *testing.T) {
	c, queries := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-1", LinkedPRs: []string{"Example-Team/App#7"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if searches(*queries) != 0 || len(items[0].Records["github"]) != 1 {
		t.Fatalf("queries %q, records %+v", *queries, items[0].Records)
	}
}

func TestChangeLinkedToTwoTicketsAppearsUnderBoth(t *testing.T) {
	c, queries := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
	items, _, err := c.Records(context.Background(), []workfile.Ticket{
		{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}},
		{Key: "APP-2", LinkedPRs: []string{"example-team/app#7"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if len(item.Records["github"]) != 1 || item.Records["github"][0].ID != "app#7" {
			t.Fatalf("%s: %+v", item.Key, item.Records)
		}
	}
	if reads := len(*queries); reads != 1 {
		t.Fatalf("a shared change is read once, got %d requests", reads)
	}
}

// hashKeys is a made-up key form that is not upper case: app#42.
type hashKeys struct{}

var hashKey = regexp.MustCompile(`app#\d+`)

func (hashKeys) Parse(input, project string) (string, error) { return input, nil }
func (hashKeys) Search(key string) string                    { return key }
func (hashKeys) Mentions(text string) []string               { return hashKey.FindAllString(text, -1) }
func (hashKeys) Links(body string) []string                  { return hashKey.FindAllString(body, -1) }

func TestKeysAreMatchedAsGiven(t *testing.T) {
	c, _ := linkServer(t, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"pr1","title":"fix app#42","state":"OPEN"}]}}}`)
	host := githubCodeHost{Client: c, name: "github"}
	got, err := host.Records(context.Background(), []workfile.Ticket{{Key: "app#42"}, {Key: "app#43"}}, hashKeys{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["app#42"]) != 1 || len(got["app#43"]) != 0 || len(got) != 1 {
		t.Fatalf("records %+v", got)
	}
}
