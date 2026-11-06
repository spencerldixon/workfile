package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

// githubConformResponses is a made-up GitHub. Change app#7 is linked to two
// tickets and read by reference. Searching for APP-3 finds its change and also
// a loose result that only mentions APP-33.
func githubConformResponses(t *testing.T) func(*http.Request) (int, string) {
	const (
		detail = `"number":%d,"url":"https://github.com/example-team/app/pull/%d","repository":{"name":"app"},"author":{"login":"example-bob"},"labels":{"nodes":[{"name":"low-risk"}]},"latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"example-alice"},"commit":{"oid":"abc"}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"example-charlie"}}]},"headRefOid":"abc"`
	)
	open := `{"data":{"repository":{"pullRequest":{"id":"pr7","title":"Change","state":"OPEN","isDraft":false,"mergeQueueEntry":null,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]},` + fmt.Sprintf(detail, 7, 7) + `}}}}`
	merged := `{"data":{"node":{"id":"pr1","title":"APP-3 work","state":"MERGED","isDraft":true,"mergeQueueEntry":{"id":"example-entry"},"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]},` + fmt.Sprintf(detail, 1, 1) + `}}}`
	return func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		variables, _ := body["variables"].(map[string]any)
		switch {
		case strings.HasPrefix(query, "query { viewer {"):
			return 200, `{"data":{"viewer":{"login":"example-bob"}}}`
		case strings.Contains(query, "organization(login"):
			return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"app"}]}}}}`
		case strings.Contains(query, "search(query"):
			if variables["first"] == float64(1) {
				return 200, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`
			}
			return 200, `{"data":{"search":{"issueCount":2,"nodes":[{"id":"pr1","title":"APP-3 work","state":"MERGED"},{"id":"pr2","title":"APP-33 is another ticket","state":"OPEN"}]}}}`
		case strings.Contains(query, "repository(owner"):
			return 200, open
		case strings.Contains(query, "node(id"):
			if variables["id"] != "pr1" {
				t.Errorf("read a change that was not matched: %v", variables["id"])
			}
			return 200, merged
		}
		t.Errorf("unexpected query %s", query)
		return 400, `{}`
	}
}

func TestGitHubHostConforms(t *testing.T) {
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	conformCodeHost(t, hostFixture{
		Provider: w.Providers["github"],
		Handle:   githubConformResponses(t),
		Tickets: []workfile.Ticket{
			{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}},
			{Key: "APP-2", LinkedPRs: []string{"example-team/app#7"}},
			{Key: "APP-3"},
			{Key: "APP-4"},
		},
	})
}
