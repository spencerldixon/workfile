package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"workfile/internal/workfile"
)

const githubText = "github-own-words"

func issuesProvider() workfile.Provider {
	return workfile.Provider{
		Kind: "github", Org: "example-team", Project: 7, DefaultProject: "app",
		Statuses: map[string]workfile.Names{
			"todo": {"Backlog"}, "doing": {"In progress"}, "review": {"In review"}, "done": {"Done"},
		},
		Auth: map[string]string{"token": "${EXAMPLE_GITHUB_TOKEN}"},
	}
}

// issue is one made-up issue's JSON. Fields in extra replace the usual ones.
func issue(repo string, number int, extra string) string {
	body := fmt.Sprintf(`"number":%d,"title":"Ticket %d","url":"https://github.com/example-team/%s/issues/%d","repository":{"name":%q}`, number, number, repo, number, repo)
	for _, field := range []struct{ name, value string }{
		{"assignees", `{"nodes":[{"login":"example-bob","name":"Bob"}]}`},
		{"issueType", `{"name":"Bug"}`},
		{"body", `"# Goal\ntext"`},
		{"labels", `{"nodes":[{"name":"blocked"}]}`},
		{"closedByPullRequestsReferences", `{"nodes":[]}`},
	} {
		if !strings.Contains(extra, `"`+field.name+`"`) {
			body += fmt.Sprintf(`,%q:%s`, field.name, field.value)
		}
	}
	if extra != "" {
		body += "," + extra
	}
	return "{" + body + "}"
}

// boardItem is one item of a project's items read.
func boardItem(kind, status string, archived bool, content string) string {
	field := "null"
	if status != "" {
		field = fmt.Sprintf(`{"name":%q}`, status)
	}
	return fmt.Sprintf(`{"type":%q,"isArchived":%v,"fieldValueByName":%s,"content":%s}`, kind, archived, field, content)
}

func page(items []string, cursor string) string {
	info := `{"hasNextPage":false,"endCursor":null}`
	if cursor != "" {
		info = fmt.Sprintf(`{"hasNextPage":true,"endCursor":%q}`, cursor)
	}
	return `{"data":{"organization":{"projectV2":{"items":{"pageInfo":` + info + `,"nodes":[` + strings.Join(items, ",") + `]}}}}}`
}

// projectItem is one entry of an issue's projectItems.
func projectItem(number int, owner, status string, archived bool) string {
	field := "null"
	if status != "" {
		field = fmt.Sprintf(`{"name":%q}`, status)
	}
	return fmt.Sprintf(`{"isArchived":%v,"project":{"number":%d,"owner":{"login":%q}},"fieldValueByName":%s}`, archived, number, owner, field)
}

func projectItems(more bool, items ...string) string {
	return fmt.Sprintf(`"projectItems":{"pageInfo":{"hasNextPage":%v},"nodes":[%s]}`, more, strings.Join(items, ","))
}

// keyRead answers an aliased read of issues. A key with no entry is answered
// the way GitHub does: NOT_FOUND beside partial data, on the issue when its
// repository exists and on the repository when it does not.
func keyRead(t *testing.T, r map[string]any, issues map[string]string) string {
	t.Helper()
	variables := r["variables"].(map[string]any)
	var parts, errs []string
	for i := 0; ; i++ {
		repo, ok := variables[fmt.Sprintf("r%d", i)].(string)
		if !ok {
			break
		}
		number := int(variables[fmt.Sprintf("n%d", i)].(float64))
		found, ok := issues[fmt.Sprintf("%s#%d", repo, number)]
		switch {
		case ok:
			parts = append(parts, fmt.Sprintf(`"i%d":{"issue":%s}`, i, found))
		case repo == "gone":
			parts = append(parts, fmt.Sprintf(`"i%d":null`, i))
			errs = append(errs, fmt.Sprintf(`{"type":"NOT_FOUND","path":["i%d"],"message":%q}`, i, githubText))
		default:
			parts = append(parts, fmt.Sprintf(`"i%d":{"issue":null}`, i))
			errs = append(errs, fmt.Sprintf(`{"type":"NOT_FOUND","path":["i%d","issue"],"message":%q}`, i, githubText))
		}
	}
	response := `{"data":{` + strings.Join(parts, ",") + `}`
	if len(errs) > 0 {
		response += `,"errors":[` + strings.Join(errs, ",") + `]`
	}
	return response + "}"
}

func isKeyRead(query string) bool { return strings.Contains(query, "issue(number") }

// githubConformTrackerResponses is a made-up project 7 with view 3, which shows
// label:blocked. app#1 is in the view, app#2 is on the project but the view
// hides it, and app#3 has a status the policy does not map.
func githubConformTrackerResponses(t *testing.T) func(*http.Request) (int, string) {
	on := func(status string) string { return projectItems(false, projectItem(7, "example-team", status, false)) }
	issues := map[string]string{
		"app#1": issue("app", 1, on("In review")),
		"app#2": issue("app", 2, on("Backlog")),
		"app#3": issue("app", 3, on("Waiting")),
	}
	return func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		variables, _ := body["variables"].(map[string]any)
		switch {
		case strings.HasPrefix(query, "query { viewer {"):
			return 200, `{"data":{"viewer":{"login":"example-bob"}}}`
		case strings.Contains(query, "comments(last: 100)"):
			return 200, `{"data":{"repository":{"issue":{"comments":{"totalCount":0,"nodes":[]}}}}}`
		case strings.Contains(query, "$repo: String!"):
			return 200, githubDetailBody
		case strings.Contains(query, "totalCount"):
			return 200, `{"data":{"organization":{"projectV2":{"title":"Example board","items":{"totalCount":3}}}}}`
		case strings.Contains(query, "view(number"):
			return 200, `{"data":{"organization":{"projectV2":{"view":{"filter":"label:blocked"}}}}}`
		case isKeyRead(query):
			return 200, keyRead(t, body, issues)
		case strings.Contains(query, "items("):
			if !strings.Contains(fmt.Sprint(variables["query"]), "label:blocked") {
				return 200, page(nil, "")
			}
			return 200, page([]string{
				boardItem("ISSUE", "In review", false, issue("app", 1, "")),
				boardItem("ISSUE", "Waiting", false, issue("app", 3, "")),
			}, "")
		}
		t.Errorf("unexpected query %s", query)
		return 400, `{}`
	}
}

func TestGitHubTrackerConforms(t *testing.T) {
	p := issuesProvider()
	p.View = 3
	conformTracker(t, trackerFixture{
		Provider: p,
		Handle:   githubConformTrackerResponses(t),
		InScope:  "app#1", OutOfScope: "app#2", Unmapped: "app#3", Missing: "app#404",
		ConfiguredScope: "label:blocked",
	})
}

func TestStateIgnoresCaseAndSymbols(t *testing.T) {
	p := workfile.Provider{Statuses: map[string]workfile.Names{
		"todo":   {"🕰 Backlog"},
		"review": {"In Review"},
		"done":   {"Done"},
	}}
	for status, want := range map[string]string{
		"☠️ Done":    "done",
		"☠ Done":     "done",
		"Done":       "done",
		"DONE":       "done",
		"backlog":    "todo",
		"🕰 Backlog":  "todo",
		"🕰️ Backlog": "todo",
		"in review":  "review",
		"Waiting":    "",
		"":           "",
		"🕰":          "",
	} {
		if got := stateFor(p, status); got != want {
			t.Errorf("stateFor(%q) = %q; want %q", status, got, want)
		}
	}
	withSymbol := workfile.Provider{Statuses: map[string]workfile.Names{"todo": {"🕰️ Backlog"}}}
	if got := stateFor(withSymbol, "🕰 Backlog"); got != "todo" {
		t.Errorf("a name holding the invisible character gave %q", got)
	}
}

func TestStatePrefersAnExactMatch(t *testing.T) {
	p := workfile.Provider{Statuses: map[string]workfile.Names{
		"a": {"Done"},
		"b": {"🕰 Done"},
		"c": {"☠️ Done"},
	}}
	for range 50 {
		for status, want := range map[string]string{"done": "a", "🕰 done": "b", "☠️ DONE": "c"} {
			if got := stateFor(p, status); got != want {
				t.Fatalf("stateFor(%q) = %q; want %q", status, got, want)
			}
		}
	}
}

func TestStateNeverPicksBetweenTwoLooseMatches(t *testing.T) {
	p := workfile.Provider{Statuses: map[string]workfile.Names{"a": {"🕰 Done"}, "b": {"☠️ Done"}}}
	for range 50 {
		if got := stateFor(p, "Done"); got != "" {
			t.Fatalf("got %q", got)
		}
	}
	same := workfile.Provider{Statuses: map[string]workfile.Names{"a": {"🕰 Done", "☠️ Done"}}}
	if got := stateFor(same, "Done"); got != "a" {
		t.Fatalf("two names of one state gave %q", got)
	}
}

func TestStateComesFromTheProjectNotTheIssue(t *testing.T) {
	c, rec := conformClient(t, "", issuesProvider(), func(r *http.Request) (int, string) {
		return 200, page([]string{boardItem("ISSUE", "Done", false, issue("app", 1, `"state":"OPEN"`))}, "")
	})
	tickets, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].State != "done" {
		t.Fatalf("%+v", tickets)
	}
	if strings.Contains(rec.requests[0], " state ") {
		t.Errorf("read the issue's open or closed state: %s", rec.requests[0])
	}
}

func TestGitHubListingReadsEveryPage(t *testing.T) {
	var after []any
	c, rec := conformClient(t, "", issuesProvider(), func(r *http.Request) (int, string) {
		variables := requestBody(t, r)["variables"].(map[string]any)
		after = append(after, variables["after"])
		first, count, cursor := 1, 100, "page-2"
		if variables["after"] != nil {
			first, count, cursor = 101, 50, ""
		}
		var items []string
		for n := first; n < first+count; n++ {
			items = append(items, boardItem("ISSUE", "In review", false, issue("app", n, "")))
		}
		return 200, page(items, cursor)
	})
	listing, err := trackerOf(t, c).Listing(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 150 || rec.count() != 2 || after[1] != "page-2" {
		t.Fatalf("%d listings in %d requests, cursors %v", len(listing), rec.count(), after)
	}
	first := listing[0]
	want := workfile.Listing{Key: "app#1", Title: "Ticket 1", Status: "In review", State: "review", Assignee: "Bob", AssigneeID: "example-bob", Type: "Bug", URL: "https://github.com/example-team/app/issues/1"}
	if first != want {
		t.Fatalf("got %+v", first)
	}
}

func TestGitHubListingAsksOnlyForWhatItShows(t *testing.T) {
	c, rec := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) { return 200, page(nil, "") })
	if _, err := trackerOf(t, c).Listing(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	for _, costly := range []string{"body", "closedByPullRequestsReferences", "labels"} {
		if strings.Contains(rec.requests[0], costly) {
			t.Errorf("listing asked for %s: %s", costly, rec.requests[0])
		}
	}
}

func TestRepeatedCursorStops(t *testing.T) {
	calls := 0
	repeat := func(*http.Request) (int, string) {
		calls++
		return 200, page([]string{boardItem("ISSUE", "Done", false, issue("app", calls, ""))}, "same")
	}
	c, _ := conformClient(t, "", issuesProvider(), repeat)
	_, err := trackerOf(t, c).Listing(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Errorf("got %v; want an error about the cursor", err)
	}
	if calls != 2 {
		t.Errorf("called %d times; want 2", calls)
	}
}

func TestLimitStopsPaging(t *testing.T) {
	c, rec := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, page([]string{
			boardItem("DRAFT_ISSUE", "", false, `{}`),
			boardItem("ISSUE", "Done", false, issue("app", 1, "")),
			boardItem("ISSUE", "Done", false, issue("app", 2, "")),
		}, "more")
	})
	tickets, err := trackerOf(t, c).Tickets(context.Background(), nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 || rec.count() != 1 {
		t.Fatalf("%d tickets in %d requests", len(tickets), rec.count())
	}
}

func TestDraftsAndPRsOnTheBoardAreSkipped(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, page([]string{
			boardItem("DRAFT_ISSUE", "Done", false, `{"title":"An idea"}`),
			boardItem("PULL_REQUEST", "Done", false, `{}`),
			boardItem("REDACTED", "Done", false, `null`),
			`null`,
			boardItem("ISSUE", "Done", false, issue("app", 1, "")),
		}, "")
	})
	tr := trackerOf(t, c)
	tickets, err := tr.Tickets(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := tr.Listing(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Key != "app#1" || len(listing) != 1 {
		t.Fatalf("tickets %+v, listing %+v", tickets, listing)
	}
}

func TestViewFilterIsTheScope(t *testing.T) {
	for _, tc := range []struct {
		name, filter string
		want         any
	}{
		{"filter", `"label:blocked"`, "label:blocked"},
		{"empty", `""`, nil},
		{"null", `null`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := issuesProvider()
			p.View = 3
			views, reads := 0, 0
			c, _ := conformClient(t, "", p, func(r *http.Request) (int, string) {
				body := requestBody(t, r)
				variables := body["variables"].(map[string]any)
				if strings.Contains(body["query"].(string), "view(number") {
					views++
					if variables["view"] != float64(3) {
						t.Errorf("read view %v", variables["view"])
					}
					return 200, `{"data":{"organization":{"projectV2":{"view":{"filter":` + tc.filter + `}}}}}`
				}
				reads++
				query, sent := variables["query"]
				if sent != (tc.want != nil) || query != tc.want {
					t.Errorf("query %v, sent %v; want %v", query, sent, tc.want)
				}
				return 200, page(nil, "")
			})
			tr := trackerOf(t, c)
			if _, err := tr.Tickets(context.Background(), nil, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := trackerOf(t, c).Listing(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			if views != 1 || reads != 2 {
				t.Fatalf("%d view reads, %d item reads", views, reads)
			}
		})
	}
}

func TestMissingViewSaysWhichSetting(t *testing.T) {
	p := issuesProvider()
	p.View = 3
	c, _ := conformClient(t, "", p, func(*http.Request) (int, string) {
		return 200, `{"data":{"organization":{"projectV2":{"view":null}}},"errors":[{"type":"NOT_FOUND","path":["organization","projectV2","view"],"message":"` + githubText + `"}]}`
	})
	_, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err == nil || strings.Contains(err.Error(), githubText) || !strings.Contains(err.Error(), "view 3") || !strings.Contains(err.Error(), "check the view setting") {
		t.Fatalf("got %v", err)
	}
}

// viewServer is a made-up project 7 whose view 3 shows label:blocked. Tests can
// change what the view shows, and fail either of its two reads.
type viewServer struct {
	mu                     sync.Mutex
	shown                  []int
	filterReads, itemReads int
	failFilter, failItems  bool
}

func (v *viewServer) handle(t *testing.T) func(*http.Request) (int, string) {
	on := projectItems(false, projectItem(7, "example-team", "Done", false))
	return func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		v.mu.Lock()
		defer v.mu.Unlock()
		switch {
		case isKeyRead(query):
			return 200, keyRead(t, body, map[string]string{"app#1": issue("app", 1, on), "app#2": issue("app", 2, on)})
		case strings.Contains(query, "view(number"):
			v.filterReads++
			if v.failFilter {
				return 400, `{}`
			}
			return 200, `{"data":{"organization":{"projectV2":{"view":{"filter":"label:blocked"}}}}}`
		}
		v.itemReads++
		if v.failItems {
			return 400, `{}`
		}
		var items []string
		for _, n := range v.shown {
			items = append(items, boardItem("ISSUE", "Done", false, issue("app", n, "")))
		}
		return 200, page(items, "")
	}
}

func (v *viewServer) reads() (int, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.filterReads, v.itemReads
}

func viewClient(t *testing.T, v *viewServer) (*Client, tracker) {
	t.Helper()
	p := issuesProvider()
	p.View = 3
	c, _ := conformClient(t, "", p, v.handle(t))
	return c, trackerOf(t, c)
}

func hidden(t *testing.T, tr tracker, key string) bool {
	t.Helper()
	tickets, err := tr.Tickets(context.Background(), []string{key}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return one(t, tickets).OutOfScope
}

func TestKeyReadsReadTheViewOnce(t *testing.T) {
	v := &viewServer{shown: []int{1}}
	_, tr := viewClient(t, v)
	if hidden(t, tr, "app#1") {
		t.Error("app#1 is in the view")
	}
	if !hidden(t, tr, "app#2") {
		t.Error("app#2 is not in the view")
	}
	if filters, items := v.reads(); filters != 1 || items != 1 {
		t.Fatalf("%d filter reads and %d item reads; want 1 of each", filters, items)
	}
}

func TestKeyReadsAtOnceReadTheViewOnce(t *testing.T) {
	v := &viewServer{shown: []int{1}}
	c, _ := viewClient(t, v)
	var reads sync.WaitGroup
	for range 8 {
		reads.Go(func() {
			tickets, err := trackerOf(t, c).Tickets(context.Background(), []string{"app#1"}, 0)
			if err != nil || len(tickets) != 1 || tickets[0].OutOfScope {
				t.Errorf("%+v, %v", tickets, err)
			}
		})
	}
	reads.Wait()
	if filters, items := v.reads(); filters != 1 || items != 1 {
		t.Fatalf("%d filter reads and %d item reads; want 1 of each", filters, items)
	}
}

func TestRememberedViewExpires(t *testing.T) {
	v := &viewServer{shown: []int{1}}
	c, tr := viewClient(t, v)
	start := c.Now()
	if hidden(t, tr, "app#1") {
		t.Fatal("app#1 is in the view")
	}
	v.mu.Lock()
	v.shown = nil
	v.mu.Unlock()
	c.Now = func() time.Time { return start.Add(viewMemory - time.Second) }
	if hidden(t, tr, "app#1") {
		t.Error("the view was read again too soon")
	}
	if filters, items := v.reads(); filters != 1 || items != 1 {
		t.Fatalf("before expiry: %d filter reads and %d item reads", filters, items)
	}
	c.Now = func() time.Time { return start.Add(viewMemory) }
	if !hidden(t, tr, "app#1") {
		t.Error("a ticket that left the view was still shown after expiry")
	}
	if filters, items := v.reads(); filters != 2 || items != 2 {
		t.Fatalf("after expiry: %d filter reads and %d item reads", filters, items)
	}
}

func TestFailedViewReadIsNotRemembered(t *testing.T) {
	ctx := context.Background()
	t.Run("filter", func(t *testing.T) {
		v := &viewServer{shown: []int{1}, failFilter: true}
		_, tr := viewClient(t, v)
		if _, err := tr.Tickets(ctx, []string{"app#1"}, 0); err == nil {
			t.Fatal("a failed view read gave no error")
		}
		v.mu.Lock()
		v.failFilter = false
		v.mu.Unlock()
		if hidden(t, tr, "app#1") {
			t.Error("app#1 is in the view")
		}
		if filters, _ := v.reads(); filters != 2 {
			t.Fatalf("%d filter reads; want a retry", filters)
		}
	})
	t.Run("shown tickets", func(t *testing.T) {
		v := &viewServer{shown: []int{1}, failItems: true}
		_, tr := viewClient(t, v)
		if _, err := tr.Tickets(ctx, []string{"app#1"}, 0); err == nil {
			t.Fatal("a failed view read gave no error")
		}
		v.mu.Lock()
		v.failItems = false
		v.mu.Unlock()
		if hidden(t, tr, "app#1") {
			t.Error("app#1 is in the view")
		}
		if _, items := v.reads(); items != 2 {
			t.Fatalf("%d item reads; want a retry", items)
		}
	})
}

func TestListingScopeReplacesTheView(t *testing.T) {
	p := issuesProvider()
	p.View = 3
	c, rec := conformClient(t, "", p, func(r *http.Request) (int, string) {
		variables := requestBody(t, r)["variables"].(map[string]any)
		if variables["query"] != `status:"🕰 Backlog"` {
			t.Errorf("sent %v", variables["query"])
		}
		return 200, page(nil, "")
	})
	if _, err := trackerOf(t, c).Listing(context.Background(), `status:"🕰 Backlog"`); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Errorf("%d requests; a given scope needs no view", rec.count())
	}
}

func TestOnlyMineAddsAssignee(t *testing.T) {
	asked := func(t *testing.T, p workfile.Provider, me string, listing bool) any {
		t.Helper()
		var sent any
		c, _ := conformClient(t, "", p, func(r *http.Request) (int, string) {
			body := requestBody(t, r)
			variables := body["variables"].(map[string]any)
			if strings.Contains(body["query"].(string), "view(number") {
				return 200, `{"data":{"organization":{"projectV2":{"view":{"filter":"label:blocked"}}}}}`
			}
			sent = variables["query"]
			return 200, page(nil, "")
		})
		if me != "" {
			dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "workfile")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("me:\n  "+me+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		c.Workspace.Settings.OnlyMine = true
		c.Workspace.Providers["issues"] = c.Workspace.Providers[c.Workspace.Policy.Tracker]
		delete(c.Workspace.Providers, c.Workspace.Policy.Tracker)
		c.Workspace.Policy.Tracker = "issues"
		var err error
		if listing {
			_, err = trackerOf(t, c).Listing(context.Background(), "")
		} else {
			_, err = trackerOf(t, c).Tickets(context.Background(), nil, 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		return sent
	}
	viewed := issuesProvider()
	viewed.View = 3
	for _, tc := range []struct {
		name, me string
		p        workfile.Provider
		want     string
	}{
		{"token owner", "", issuesProvider(), "assignee:@me"},
		{"with a view", "", viewed, "label:blocked assignee:@me"},
		{"named connection", "issues: alice", issuesProvider(), "assignee:alice"},
		{"github default", "github: bob", issuesProvider(), "assignee:bob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := asked(t, tc.p, tc.me, false); got != tc.want {
				t.Errorf("Tickets sent %v; want %s", got, tc.want)
			}
		})
	}
	t.Run("listing", func(t *testing.T) {
		if got := asked(t, issuesProvider(), "", true); got != nil {
			t.Errorf("Listing sent %v", got)
		}
	})
}

// byKey reads keys from a made-up GitHub holding issues.
func byKey(t *testing.T, p workfile.Provider, issues map[string]string, keys ...string) ([]workfile.Ticket, *recorder) {
	t.Helper()
	c, rec := conformClient(t, "", p, func(r *http.Request) (int, string) {
		return 200, keyRead(t, requestBody(t, r), issues)
	})
	tickets, err := trackerOf(t, c).Tickets(context.Background(), keys, 0)
	if err != nil {
		t.Fatal(err)
	}
	return tickets, rec
}

func one(t *testing.T, tickets []workfile.Ticket) workfile.Ticket {
	t.Helper()
	if len(tickets) != 1 {
		t.Fatalf("want one ticket, got %+v", tickets)
	}
	return tickets[0]
}

func TestTicketsByKeyPicksThisProject(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false,
			projectItem(3, "example-team", "Backlog", false),
			projectItem(7, "other-team", "Backlog", false),
			projectItem(7, "Example-Team", "Done", false),
		)),
	}, "app#1")
	if got := one(t, tickets); got.State != "done" || got.Error != "" || got.OutOfScope {
		t.Fatalf("%+v", got)
	}
}

func TestProjectOfAnotherOwnerIsNotThisProject(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false, projectItem(7, "other-team", "Done", false))),
	}, "app#1")
	if got := one(t, tickets); got.State != "" || got.Error == "" {
		t.Fatalf("%+v", got)
	}
}

func TestIssueOnMoreProjectsThanOnePageIsNotGuessed(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(true, projectItem(3, "example-team", "Done", false))),
		"app#2": issue("app", 2, projectItems(true, projectItem(7, "example-team", "Done", false))),
	}, "app#1", "app#2")
	if len(tickets) != 2 {
		t.Fatalf("%+v", tickets)
	}
	if got := tickets[0]; got.State != "" || !strings.Contains(got.Error, "too many projects") || got.OutOfScope {
		t.Errorf("%+v", got)
	}
	if got := tickets[1]; got.State != "done" || got.Error != "" {
		t.Errorf("an item on the first page still counts: %+v", got)
	}
}

func TestIssueWithNoStatusHasAnError(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false, projectItem(7, "example-team", "", false))),
	}, "app#1")
	if got := one(t, tickets); got.State != "" || !strings.Contains(got.Error, "no Status") {
		t.Fatalf("%+v", got)
	}
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, page([]string{boardItem("ISSUE", "", false, issue("app", 2, ""))}, "")
	})
	scoped, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := one(t, scoped); got.State != "" || !strings.Contains(got.Error, "no Status") {
		t.Fatalf("%+v", got)
	}
}

func TestIssueNotOnTheProjectHasAnError(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false, projectItem(3, "example-team", "Done", false))),
	}, "app#1")
	if got := one(t, tickets); got.State != "" || !strings.Contains(got.Error, "not on project 7") || !got.OutOfScope {
		t.Fatalf("%+v", got)
	}
}

func TestArchivedItemIsOutOfScope(t *testing.T) {
	tickets, rec := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false, projectItem(7, "example-team", "Done", true))),
	}, "app#1")
	if got := one(t, tickets); got.State != "done" || got.Error != "" || !got.OutOfScope {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(rec.requests[0], "includeArchived") {
		t.Error("includeArchived should stay at its default")
	}
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, page([]string{boardItem("ISSUE", "Done", true, issue("app", 1, ""))}, "")
	})
	scoped, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 0 {
		t.Fatalf("a scope read returned an archived item: %+v", scoped)
	}
}

func TestLinkedPRsComeWithTheTicket(t *testing.T) {
	const linked = `"closedByPullRequestsReferences":{"nodes":[{"number":7,"repository":{"name":"app","owner":{"login":"example-team"}}},{"number":8,"repository":{"name":"web","owner":{"login":"example-team"}}}]}`
	want := []string{"example-team/app#7", "example-team/web#8"}
	tickets, rec := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, linked+","+projectItems(false, projectItem(7, "example-team", "Done", false))),
	}, "app#1")
	if got := one(t, tickets); !slices.Equal(got.LinkedPRs, want) || rec.count() != 1 {
		t.Fatalf("%+v in %d requests", got, rec.count())
	}
	if strings.Contains(rec.requests[0], "includeClosedPrs") {
		t.Error("includeClosedPrs should stay at its default")
	}
	c, rec := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, page([]string{boardItem("ISSUE", "Done", false, issue("app", 1, linked))}, "")
	})
	scoped, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := one(t, scoped); !slices.Equal(got.LinkedPRs, want) || rec.count() != 1 {
		t.Fatalf("%+v in %d requests", got, rec.count())
	}
}

func TestKeysAreReadTogether(t *testing.T) {
	on := projectItems(false, projectItem(7, "example-team", "Done", false))
	tickets, rec := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, on), "app#2": issue("app", 2, on), "web#3": issue("web", 3, on),
	}, "app#1", "app#2", "web#3", "app#1")
	if len(tickets) != 3 || rec.count() != 1 {
		t.Fatalf("%d tickets in %d requests", len(tickets), rec.count())
	}
	if tickets[2].Key != "web#3" || tickets[2].Facts["project"] != "web" {
		t.Fatalf("%+v", tickets[2])
	}
}

func TestMissingKeysAreLeftOut(t *testing.T) {
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("app", 1, projectItems(false, projectItem(7, "example-team", "Done", false))),
	}, "app#404", "app#1", "gone#1")
	if got := one(t, tickets); got.Key != "app#1" {
		t.Fatalf("%+v", got)
	}
	none, _ := byKey(t, issuesProvider(), nil, "gone#1")
	if len(none) != 0 {
		t.Fatalf("%+v", none)
	}
}

func TestOtherGraphQLErrorsFailTheRead(t *testing.T) {
	for _, kind := range []string{"FORBIDDEN", "RATE_LIMITED", ""} {
		c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
			return 200, `{"data":{"i0":{"issue":null}},"errors":[{"type":"NOT_FOUND","path":["i0","issue"],"message":"` + githubText + `"},{"type":"` + kind + `","path":["i1"],"message":"` + githubText + `"}]}`
		})
		tr := trackerOf(t, c)
		if _, err := tr.Tickets(context.Background(), []string{"app#1", "app#2"}, 0); err == nil || strings.Contains(err.Error(), githubText) {
			t.Errorf("%q: got %v", kind, err)
		}
	}
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, `{"data":{"i0":{"issue":{"number":1,"projectItems":null}}},"errors":[{"type":"NOT_FOUND","path":["i0","issue","projectItems"],"message":"` + githubText + `"}]}`
	})
	if _, err := trackerOf(t, c).Tickets(context.Background(), []string{"app#1"}, 0); err == nil {
		t.Error("a NOT_FOUND inside an issue is not a missing key")
	}
}

func TestMissingProjectSaysWhichSetting(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, `{"data":{"organization":{"projectV2":null}},"errors":[{"type":"NOT_FOUND","path":["organization","projectV2"],"message":"` + githubText + `"}]}`
	})
	_, err := trackerOf(t, c).Tickets(context.Background(), nil, 0)
	if err == nil || strings.Contains(err.Error(), githubText) || !strings.Contains(err.Error(), "project") {
		t.Fatalf("got %v", err)
	}
}

func TestMissingScopeNamesReadProject(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), func(r *http.Request) (int, string) {
		if strings.HasPrefix(requestBody(t, r)["query"].(string), "query { viewer {") {
			return 200, `{"data":{"viewer":{"login":"example-bob"}}}`
		}
		return 200, `{"data":{"organization":{"projectV2":null}},"errors":[{"type":"INSUFFICIENT_SCOPES","path":["organization","projectV2"],"message":"` + githubText + `"}]}`
	})
	_, err := trackerOf(t, c).Test(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read:project") || strings.Contains(err.Error(), githubText) {
		t.Fatalf("got %v", err)
	}
}

func TestGitHubTrackerTestDescribesTheProject(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), func(r *http.Request) (int, string) {
		if strings.HasPrefix(requestBody(t, r)["query"].(string), "query { viewer {") {
			return 200, `{"data":{"viewer":{"login":"alice"}}}`
		}
		return 200, `{"data":{"organization":{"projectV2":{"title":"Example board","items":{"totalCount":12}}}}}`
	})
	got, err := trackerOf(t, c).Test(context.Background())
	if err != nil || got != "alice · Example board · 12 items" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestGitHubFacts(t *testing.T) {
	body, _ := json.Marshal("Intro\r\n# Goal\r\n```\n# not a heading\n```\n## Steps ##\n~~~~\n# nor this\n~~~\n~~~~\n   ### Done\n#nospace\n")
	const noLabels = `"labels":null`
	tickets, _ := byKey(t, issuesProvider(), map[string]string{
		"app#1": issue("App", 1, `"body":`+string(body)+`,`+projectItems(false, projectItem(7, "example-team", "In review", false))),
		"app#2": issue("app", 2, `"issueType":null,"assignees":{"nodes":[]},`+noLabels+`,`+projectItems(false, projectItem(7, "example-team", "Done", false))),
		"app#3": issue("app", 3, `"assignees":{"nodes":[{"login":"example-charlie","name":null}]},`+projectItems(false, projectItem(7, "example-team", "Done", false))),
	}, "app#1", "app#2", "app#3")
	if len(tickets) != 3 {
		t.Fatalf("%+v", tickets)
	}
	full := tickets[0]
	if full.Key != "app#1" || full.Title != "Ticket 1" || full.URL != "https://github.com/example-team/App/issues/1" || full.Assignee != "example-bob" || full.Summary != "In review · assigned to Bob" {
		t.Errorf("%+v", full)
	}
	if got := full.Facts["headings"].([]string); !slices.Equal(got, []string{"Goal", "Steps", "Done"}) {
		t.Errorf("headings %q", got)
	}
	labels := full.Facts["labels"].([]workfile.Actor)
	if len(labels) != 1 || labels[0].Value != "blocked" {
		t.Errorf("labels %+v", labels)
	}
	if full.Facts["summary"] != "Ticket 1" || !strings.Contains(full.Facts["description"].(string), "Intro") || full.Facts["assignee"] != "Bob" || full.Facts["type"] != "Bug" || full.Facts["project"] != "App" {
		t.Errorf("facts %+v", full.Facts)
	}
	bare := tickets[1]
	if _, has := bare.Facts["type"]; has || bare.Unavailable["type"] == "" {
		t.Errorf("no issue type: %+v, %+v", bare.Facts, bare.Unavailable)
	}
	if _, has := bare.Facts["labels"]; has || bare.Unavailable["labels"] == "" {
		t.Errorf("no labels: %+v, %+v", bare.Facts, bare.Unavailable)
	}
	if bare.Assignee != "" || bare.Facts["assignee"] != "" || bare.Summary != "Done · unassigned" {
		t.Errorf("unassigned: %+v", bare)
	}
	if got := tickets[2]; got.Assignee != "example-charlie" || got.Facts["assignee"] != "example-charlie" {
		t.Errorf("no display name: %+v", got)
	}
}
