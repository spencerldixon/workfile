package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestGitHubFailedChecksReadsOnePROnly(t *testing.T) {
	var queries []map[string]any
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		if r.Method != http.MethodPost || !strings.Contains(body["query"].(string), "statusCheckRollup { contexts") {
			t.Errorf("unexpected request %s %v", r.Method, body["query"])
		}
		queries = append(queries, body["variables"].(map[string]any))
		return 200, `{"data":{"repository":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
			{"__typename":"CheckRun","name":"test (ubuntu)","conclusion":"FAILURE","detailsUrl":"https://example.com/runs/1"},
			{"__typename":"CheckRun","name":"build","conclusion":"SUCCESS","detailsUrl":"https://example.com/runs/2"},
			{"__typename":"CheckRun","name":"slow","conclusion":"TIMED_OUT","detailsUrl":"https://example.com/runs/3"},
			{"__typename":"CheckRun","name":"skipped","conclusion":"SKIPPED","detailsUrl":"https://example.com/runs/4"},
			{"__typename":"StatusContext","context":"ci/legacy","state":"ERROR","targetUrl":"https://example.com/legacy"},
			{"__typename":"StatusContext","context":"ci/ok","state":"SUCCESS","targetUrl":"https://example.com/ok"}
		]}}}}]}}}}}`
	})
	got, err := c.FailedChecks(context.Background(), "github", workfile.Record{ID: "app#42"})
	if err != nil {
		t.Fatal(err)
	}
	want := []workfile.FailedCheck{
		{Name: "test (ubuntu)", URL: "https://example.com/runs/1"},
		{Name: "slow", URL: "https://example.com/runs/3"},
		{Name: "ci/legacy", URL: "https://example.com/legacy"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	if len(queries) != 1 || queries[0]["owner"] != "example-team" || queries[0]["name"] != "app" || queries[0]["number"] != float64(42) {
		t.Fatalf("expected one read of example-team/app#42, got %v", queries)
	}
}

func TestGitHubFailedChecksRefusesUnlistedRepos(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		t.Error("read a PR outside the connection's repositories")
		return 400, `{}`
	})
	for _, record := range []workfile.Record{{ID: "other#1"}, {ID: "nonsense"}} {
		if _, err := c.FailedChecks(context.Background(), "github", record); err == nil {
			t.Fatalf("%s: expected an error", record.ID)
		}
	}
	if _, err := c.FailedChecks(context.Background(), "jira", workfile.Record{ID: "app#1"}); err == nil {
		t.Fatal("the tracker is not a code host")
	}
}
