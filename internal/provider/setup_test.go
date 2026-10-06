package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

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
