package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestCodeHostDropsPRsFromUnlistedRepos(t *testing.T) {
	c, _ := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
	p := c.Workspace.Providers["github"]
	p.Repos = workfile.Names{"api"}
	c.Workspace.Providers["github"] = p
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].Records["github"]; len(got) != 0 {
		t.Fatalf("a PR in an unlisted repo was kept: %+v", got)
	}
}

func TestCodeHostDropsSearchedPRsFromUnlistedReposAfterReading(t *testing.T) {
	// The search result names no repository, so the PR is read, then dropped.
	c, _ := linkServer(t, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"pr1","title":"APP-2 work","state":"OPEN"}]}}}`)
	p := c.Workspace.Providers["github"]
	p.Repos = workfile.Names{"api"}
	c.Workspace.Providers["github"] = p
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].Records["github"]; len(got) != 0 {
		t.Fatalf("a PR in an unlisted repo was kept: %+v", got)
	}
}

func TestCodeHostKeepsListedAndAllRepos(t *testing.T) {
	for _, repos := range []workfile.Names{{"APP"}, {"all"}} {
		c, _ := linkServer(t, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`)
		p := c.Workspace.Providers["github"]
		p.Repos = repos
		c.Workspace.Providers["github"] = p
		items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-1", LinkedPRs: []string{"example-team/app#7"}}})
		if err != nil || len(items[0].Records["github"]) != 1 {
			t.Fatalf("%v: %+v, %v", repos, items[0].Records, err)
		}
	}
}

func TestCodeHostTestWarnsAboutMissingRepos(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		switch {
		case strings.HasPrefix(query, "query { viewer {"):
			return 200, `{"data":{"viewer":{"login":"example-bob"}}}`
		case strings.Contains(query, "organization(login"):
			return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"app"},{"name":"old","isArchived":true},{"name":"other"}],"pageInfo":{"hasNextPage":false}}}}}`
		}
		return 200, `{"data":{"search":{"issueCount":0,"nodes":[]}}}`
	})
	p := c.Workspace.Providers["github"]
	p.Repos = workfile.Names{"app", "api", "old"}
	c.Workspace.Providers["github"] = p
	message, err := c.Test(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "1 visible active repositories") {
		t.Errorf("unlisted repos were counted: %q", message)
	}
	if !strings.Contains(message, "github.repos lists api, old, which are not in example-team or are archived") {
		t.Errorf("no warning: %q", message)
	}
}
