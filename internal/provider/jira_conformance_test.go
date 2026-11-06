package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

// jiraConformResponses is a made-up Jira with three tickets. APP-1 is on the
// board, APP-2 exists but the scope hides it, and APP-3 has an unmapped status.
func jiraConformResponses(t *testing.T) func(*http.Request) (int, string) {
	issue := func(key, id, status string) string {
		return fmt.Sprintf(`{"id":%q,"key":%q,"fields":{"summary":"Ticket %s","description":"Details","status":{"name":%q},"labels":["approved"],"assignee":{"accountId":"example-bob-account-id","displayName":"Bob"},"issuetype":{"name":"Bug"}}}`, id, key, key, status)
	}
	all := map[string]string{
		"APP-1": issue("APP-1", "1", "In Review"),
		"APP-2": issue("APP-2", "2", "To Do"),
		"APP-3": issue("APP-3", "3", "Waiting"),
	}
	return func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/_edge/tenant_info":
			return 200, `{"cloudId":"cloud-example"}`
		case strings.HasSuffix(r.URL.Path, "/myself"):
			return 200, `{"accountId":"example-bob-account-id"}`
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			jql := requestBody(t, r)["jql"].(string)
			var found []string
			for _, key := range []string{"APP-1", "APP-2", "APP-3"} {
				named := strings.Contains(jql, `"`+key+`"`)
				switch {
				case strings.HasPrefix(jql, "key in"):
					// Asked for by key: the scope does not apply.
					if named {
						found = append(found, all[key])
					}
				case strings.Contains(jql, "AND key in"):
					// The scoped check: the board hides APP-2.
					if named && key != "APP-2" {
						found = append(found, all[key])
					}
				default:
					if key != "APP-2" {
						found = append(found, all[key])
					}
				}
			}
			return 200, `{"issues":[` + strings.Join(found, ",") + `],"isLast":true}`
		case strings.Contains(r.URL.Path, "/rest/api/3/issue/APP-1"):
			return 200, `{"fields":{"description":"Details","comment":{"total":0,"comments":[]},"attachment":[]}}`
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		return 500, `{}`
	}
}

func TestJiraConforms(t *testing.T) {
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	conformTracker(t, trackerFixture{
		Provider: w.Providers["jira"],
		Handle:   jiraConformResponses(t),
		InScope:  "APP-1", OutOfScope: "APP-2", Unmapped: "APP-3", Missing: "APP-404",
	})
}
