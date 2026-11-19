package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubPreservesOperationalAttentionWithoutExtraRequests(t *testing.T) {
	var p pullRequest
	if err := json.Unmarshal([]byte(`{
  "id":"example-pr", "number":128, "url":"https://github.example.com/example-org/web/pull/128", "state":"OPEN", "isDraft":false,
  "headRefOid":"example-head", "mergeable":"CONFLICTING", "repository":{"name":"web"}, "author":{"login":"example-author"},
  "labels":{"nodes":[]},
  "latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"example-reviewer"}}]},
  "reviewRequests":{"nodes":[{"requestedReviewer":{"slug":"example-team"}}]},
  "commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}
}`), &p); err != nil {
		t.Fatal(err)
	}
	r := p.record()
	if r.Facts["mergeable"] != "conflicting" || r.Facts["checks"] != "failure" {
		t.Fatalf("PR operational evidence lost: %+v", r.Facts)
	}
	if requests := r.Facts["review_requests"].([]string); len(requests) != 1 || requests[0] != "team:example-team" {
		t.Fatalf("team review request was mistaken for no request: %+v", requests)
	}
	if changes := r.Facts["changes_requested"].([]string); len(changes) != 1 || changes[0] != "example-reviewer" {
		t.Fatalf("requested changes lost: %+v", changes)
	}
	if r.ChecksURL != "https://github.example.com/example-org/web/pull/128/checks" {
		t.Fatalf("wrong check target: %s", r.ChecksURL)
	}
	if !strings.Contains(prFields, "mergeable") || !strings.Contains(prFields, "... on Team { slug }") {
		t.Fatal("normal linked-PR read must contain the needed facts")
	}
}

func TestGitHubDoesNotInventKnownMergeabilityOrReviewerIdentity(t *testing.T) {
	var p pullRequest
	if err := json.Unmarshal([]byte(`{"state":"OPEN","mergeable":"UNKNOWN","author":{"login":"example-author"},"reviewRequests":{"nodes":[{"requestedReviewer":{}}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	r := p.record()
	if r.Unavailable["mergeable"] == "" || r.Unavailable["review_requests"] == "" {
		t.Fatalf("missing evidence became known: %+v", r)
	}
	if r.ChecksURL != "" {
		t.Fatal("an absent PR URL became a local checks path")
	}
}
