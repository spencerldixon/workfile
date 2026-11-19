package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestSetupGitHubTrackerDiscoversProjectsAndStatusOptions(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(request.Query, "projectsV2("):
			switch request.Variables["after"] {
			case nil:
				return 200, `{"data":{"organization":{"projectsV2":{"nodes":[{"number":4,"title":"Example project"},{"number":3,"title":"Old project","closed":true}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`
			case "next":
				return 200, `{"data":{"organization":{"projectsV2":{"nodes":[{"number":2,"title":"Example web"}],"pageInfo":{"hasNextPage":false}}}}}`
			}
			t.Fatalf("unexpected cursor: %v", request.Variables)
		case strings.Contains(request.Query, `field(name: "Status")`):
			if request.Variables["project"] != float64(4) {
				t.Fatalf("unexpected project: %v", request.Variables)
			}
			return 200, `{"data":{"organization":{"projectV2":{"field":{"options":[{"name":"Backlog"},{"name":"Review"},{"name":"Done"}]}}}}}`
		}
		t.Fatal("unexpected discovery query")
		return 500, `{}`
	})
	c.Workspace.Policy.Tracker = "github"
	ctx := context.Background()
	projects, err := c.SetupProjects(ctx)
	if err != nil || len(projects) != 2 || projects[0].Key != "4" || projects[1].Key != "2" {
		t.Fatalf("projects %v: %v", projects, err)
	}
	statuses, err := c.SetupStatuses(ctx, "4")
	if err != nil || strings.Join(statuses, ",") != "Backlog,Review,Done" {
		t.Fatalf("statuses %v: %v", statuses, err)
	}
}

func TestSetupDiscovery(t *testing.T) {
	pages := 0
	c := client(t, func(r *http.Request) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "tenant_info"):
			return 200, `{"cloudId":"example-cloud"}`
		case strings.HasSuffix(r.URL.Path, "project/search"):
			pages++
			if r.URL.Query().Get("startAt") == "0" {
				return 200, `{"values":[{"key":"APP","name":"Example project"}],"total":2}`
			}
			return 200, `{"values":[{"key":"WEB","name":"Example web"}],"isLast":true,"total":2}`
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			return 200, `[{"statuses":[{"name":"Done","statusCategory":{"key":"done"}},{"name":"In Progress","statusCategory":{"key":"indeterminate"}},{"name":"To Do","statusCategory":{"key":"new"}}]},{"statuses":[{"name":"Done","statusCategory":{"key":"done"}}]}]`
		case strings.HasSuffix(r.URL.Path, "user/assignable/search"):
			if r.URL.Query().Get("project") != "APP" {
				t.Fatal("people are not project scoped")
			}
			return 200, `[{"accountId":"example-alice","displayName":"Alice","active":true,"accountType":"atlassian"},{"accountId":"example-bob","displayName":"Bob","active":false},{"accountId":"example-app","active":true,"accountType":"app"}]`
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return 500, `{}`
	})
	projects, err := c.SetupProjects(context.Background())
	if err != nil || len(projects) != 2 || pages != 2 {
		t.Fatalf("projects %v: %v", projects, err)
	}
	statuses, err := c.SetupStatuses(context.Background(), "APP")
	if err != nil || strings.Join(statuses, ",") != "To Do,In Progress,Done" {
		t.Fatalf("statuses %v: %v", statuses, err)
	}
	people, err := c.SetupPeople(context.Background(), "APP")
	if err != nil || len(people) != 1 || people[0].AccountID != "example-alice" {
		t.Fatalf("people %v: %v", people, err)
	}
}

func TestSetupReposListsUnarchivedRepositories(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"web"},{"name":"old","isArchived":true},{"name":"api"}],"pageInfo":{"hasNextPage":false}}}}}`
	})
	p := c.Workspace.Providers["github"]
	p.Repos = workfile.Names{"api"} // the choice made so far must not narrow the list
	c.Workspace.Providers["github"] = p
	repos, err := c.SetupRepos(context.Background(), "github")
	if err != nil || len(repos) != 2 || repos[0].Name != "api" || repos[1].Name != "web" {
		t.Fatalf("repos %v: %v", repos, err)
	}
}

func TestSetupTokenValidation(t *testing.T) {
	for _, bad := range []string{"", " ", "a b", "a\nb", `"x"`, "x'y"} {
		if _, err := setupToken(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if got, err := setupToken("  example-token  "); err != nil || got != "example-token" {
		t.Fatal("rejected a good token")
	}
}
