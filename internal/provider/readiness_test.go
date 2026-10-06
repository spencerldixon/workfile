package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func decodePR(t *testing.T, raw string) pullRequest {
	t.Helper()
	var p pullRequest
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestReadinessFacts(t *testing.T) {
	for _, tc := range []struct{ rollup, want string }{{`{"state":"SUCCESS"}`, "success"}, {`{"state":"PENDING"}`, "pending"}, {`{"state":"EXPECTED"}`, "pending"}, {`{"state":"FAILURE"}`, "failure"}, {`{"state":"ERROR"}`, "failure"}, {`null`, "none"}, {`{"state":"NEW_STATE"}`, ""}} {
		t.Run(tc.want+tc.rollup, func(t *testing.T) {
			p := decodePR(t, `{"id":"pr1","number":1,"state":"OPEN","repository":{"name":"api"},"isDraft":false,"headRefOid":"new","commits":{"nodes":[{"commit":{"statusCheckRollup":`+tc.rollup+`}}]},"latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"example-alice"},"commit":{"oid":"old"}},{"state":"APPROVED","author":{"login":"example-bob"},"commit":{"oid":"new"}},{"state":"CHANGES_REQUESTED","author":{"login":"example-charlie"},"commit":{"oid":"new"}}]}}`)
			r := p.record()
			if r.Facts["draft"] != "false" {
				t.Fatal("false draft lost")
			}
			if tc.want == "" {
				if r.Unavailable["checks"] == "" {
					t.Fatal("unknown check state accepted")
				}
			} else if r.Facts["checks"] != tc.want {
				t.Fatalf("checks %v", r.Facts)
			}
			if len(r.Facts["approvals"].([]workfile.Actor)) != 2 {
				t.Fatal("existing approvals changed")
			}
			fresh := r.Facts["fresh_approvals"].([]workfile.Actor)
			if len(fresh) != 1 || fresh[0].ID != "example-bob" {
				t.Fatalf("stale approval counted: %+v", fresh)
			}
		})
	}
	r := pullRequest{}.record()
	for _, fact := range []string{"draft", "checks", "fresh_approvals"} {
		if r.Unavailable[fact] == "" {
			t.Fatalf("missing %s treated as known", fact)
		}
	}
	p := decodePR(t, `{"headRefOid":"new","latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"example-alice"}}]}}`)
	if p.record().Unavailable["fresh_approvals"] == "" {
		t.Fatal("missing review commit counted as fresh")
	}
}

func TestMissingCheckShapeIsNotNoChecks(t *testing.T) {
	for _, raw := range []string{`{}`, `{"commits":{"nodes":[]}}`, `{"commits":{"nodes":[{}]}}`, `{"commits":{"nodes":[{"commit":{}}]}}`, `{"commits":{"nodes":[{"commit":{"statusCheckRollup":{}}}]}}`} {
		p := decodePR(t, raw)
		if status, reason := p.checkStatus(); status != "" || reason == "" {
			t.Fatalf("missing data treated as checks %q: %s", status, raw)
		}
	}
}

func TestJiraMissingAndEmptyEvidence(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		missing bool
	}{
		{`{"id":"1","key":"APP-42","fields":{}}`, true},
		{`{"id":"1","key":"APP-42","fields":{"summary":"Settings","description":null,"labels":[],"assignee":null,"issuetype":{"name":"Bug"}}}`, false},
	} {
		var issue jiraIssue
		if err := json.Unmarshal([]byte(tc.raw), &issue); err != nil {
			t.Fatal(err)
		}
		for _, fact := range []string{"summary", "description", "headings", "labels", "assignee", "type"} {
			if (issue.Unavailable[fact] != "") != tc.missing {
				t.Fatalf("%s: unexpected evidence state %+v", fact, issue.Unavailable)
			}
		}
	}
}

func TestExplicitLinksOnlyUseConfiguredJiraBrowseURLs(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{"Related APP-42", 0},
		{"[Ticket](https://example-team.atlassian.net/browse/APP-42)", 1},
		{"https://other.example.com/browse/APP-42", 0},
		{"https://example-team.atlassian.net.evil.example.com/browse/APP-42", 0},
		{"https://example-team.atlassian.net/browse/APP-42/more", 0},
		{"https://example-team.atlassian.net/browse/APP-42?focusedCommentId=1", 1},
		{"https://example-alice@example-team.atlassian.net/browse/APP-42", 0},
	} {
		if got := explicitKeys(tc.body, "example-team.atlassian.net"); len(got) != tc.want {
			t.Fatalf("%q: %v", tc.body, got)
		}
	}
}

func TestBodyLinkedPRIsFetchedAndDeduplicated(t *testing.T) {
	calls := 0
	c := client(t, func(r *http.Request) (int, string) {
		q := requestBody(t, r)["query"].(string)
		if strings.Contains(q, "organization(login") {
			return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"api"}]}}}}`
		}
		if strings.Contains(q, "search(query") {
			if !strings.Contains(q, "body") {
				t.Error("discovery does not read explicit links")
			}
			return 200, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"pr1","title":"Fix settings","state":"OPEN","body":"https://example-team.atlassian.net/browse/APP-42 https://example-team.atlassian.net/browse/APP-42"}]}}}`
		}
		calls++
		if !strings.Contains(q, "statusCheckRollup") || !strings.Contains(q, "commit { oid }") || !strings.Contains(q, "isDraft") {
			t.Error("missing readiness query fields")
		}
		return 200, `{"data":{"node":{"id":"pr1","number":1,"repository":{"name":"api"},"state":"OPEN","labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}`
	})
	// The helper's site is deliberately replaced with the anonymous link host.
	p := c.Workspace.Providers[c.Workspace.Policy.Tracker]
	p.Site = "example-team.atlassian.net"
	c.Workspace.Providers[c.Workspace.Policy.Tracker] = p
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-42"}})
	if err != nil || calls != 1 || len(items[0].Records["github"]) != 1 {
		t.Fatalf("calls %d, items %+v, err %v", calls, items, err)
	}
}
