package provider

import (
	"context"
	"net/http"
	"testing"

	"workfile/internal/workfile"
)

func TestJiraSearch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		p         workfile.Provider
		wantJQL   string
		wantOrder string
	}{
		{
			"one project",
			workfile.Provider{Projects: workfile.Names{"APP"}, KeepDoneFor: "14d"},
			`project in ("APP") AND (statusCategory != Done OR resolved >= -14d)`, "updated DESC",
		},
		{
			"skipped statuses and weeks",
			workfile.Provider{Projects: workfile.Names{"APP", "WEB"}, SkipStatuses: workfile.Names{"Backlog", "On hold"}, KeepDoneFor: "2w"},
			`project in ("APP", "WEB") AND status not in ("Backlog", "On hold") AND (statusCategory != Done OR resolved >= -2w)`, "updated DESC",
		},
		{
			"extra scope with order",
			workfile.Provider{Projects: workfile.Names{"APP"}, KeepDoneFor: "14d", Scope: "labels != spike ORDER BY rank"},
			`project in ("APP") AND (statusCategory != Done OR resolved >= -14d) AND (labels != spike)`, "rank",
		},
		{
			"status with a quote",
			workfile.Provider{Projects: workfile.Names{"APP"}, SkipStatuses: workfile.Names{`Won"t do`}, KeepDoneFor: "14d"},
			`project in ("APP") AND status not in ("Won\"t do") AND (statusCategory != Done OR resolved >= -14d)`, "updated DESC",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := jiraSearch(tc.p); got != tc.wantJQL {
				t.Errorf("jiraSearch = %s\nwant %s", got, tc.wantJQL)
			}
			if got := jiraOrder(tc.p); got != tc.wantOrder {
				t.Errorf("jiraOrder = %s, want %s", got, tc.wantOrder)
			}
		})
	}
}

// Listing and Tickets must read the same slice.
func TestJiraListingAndTicketsUseTheSameSearch(t *testing.T) {
	var jqls []string
	c := client(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/_edge/tenant_info" {
			return 200, `{"cloudId":"cloud-example"}`
		}
		jqls = append(jqls, requestBody(t, r)["jql"].(string))
		return 200, `{"issues":[],"isLast":true}`
	})
	p := c.Workspace.Providers["jira"]
	p.Scope = "labels != spike"
	c.Workspace.Providers["jira"] = p
	tr := trackerOf(t, c)
	if _, err := tr.Tickets(context.Background(), nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Listing(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	want := "(" + jiraSearch(p) + ") ORDER BY updated DESC"
	if len(jqls) != 2 || jqls[0] != want || jqls[1] != want {
		t.Fatalf("got %q\nwant %q twice", jqls, want)
	}
}
