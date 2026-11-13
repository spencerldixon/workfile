package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

const exampleScope = "Jira · APP · repos app, api"

func TestScopeLineNamesWhatIsRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(w *workfile.Workspace)
		want string
	}{
		{"example", func(w *workfile.Workspace) {}, exampleScope},
		{"all repos", func(w *workfile.Workspace) {
			p := w.Providers["github"]
			p.Repos = workfile.Names{"all"}
			w.Providers["github"] = p
		}, "Jira · APP · all repos in example-team"},
		{"several projects", func(w *workfile.Workspace) {
			p := w.Providers["jira"]
			p.Projects = workfile.Names{"APP", "WEB", "OPS"}
			w.Providers["jira"] = p
		}, "Jira · APP, WEB, OPS · repos app, api"},
		{"jira only", func(w *workfile.Workspace) { delete(w.Providers, "github") }, "Jira · APP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := workfile.Load("../../example")
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(w)
			if got := w.ScopeLine(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitHubIssuesScopeLine(t *testing.T) {
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	w.Providers["issues"] = workfile.Provider{Kind: "github", Org: "example-team", Project: 12, Repos: workfile.Names{"web", "api"}}
	w.Policy.Tracker = "issues"
	if got, want := w.ScopeLine(), "GitHub Issues · project 12 · repos web, api"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestScopeLineFitsNarrowTerminals(t *testing.T) {
	w, err := workfile.Load("../../example")
	if err != nil {
		t.Fatal(err)
	}
	p := w.Providers["jira"]
	p.Projects = workfile.Names{"APP", "WEB", "OPS", "EXAMPLE", "DATA", "INFRA"}
	w.Providers["jira"] = p
	for _, width := range []int{40, 60} {
		line := scopeText(w, width)
		if got := ansi.StringWidth(line); got > width || strings.ContainsRune(line, '\x1b') {
			t.Errorf("%d columns: %q is %d wide", width, line, got)
		}
	}
	if !strings.HasSuffix(scopeText(w, 20), "…") {
		t.Error("a cut line should end with an ellipsis")
	}
}

func TestTrackerScreenShowsTheScopeAtNarrowWidths(t *testing.T) {
	for _, width := range []int{40, 60} {
		d := driveTracker(t, trackerSource(), width, 30)
		first := strings.Split(d.screen(), "\n")[0]
		if !strings.Contains(ansi.Strip(first), "Jira") {
			t.Errorf("%d columns: first line %q", width, first)
		}
		d.check()
	}
}

func TestPlainOutputNamesTheScope(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, args := range [][]string{{"tracker"}, {"status"}, {"status", "--me"}} {
		f := trackerSource()
		f.tickets = []workfile.Ticket{fixtureTicket("APP-1", "doing", "text")}
		_, out, _ := invoke(t, f, args...)
		if !strings.Contains(out, exampleScope) || strings.Contains(out, "\x1b") {
			t.Errorf("%v: no plain scope line:\n%s", args, out)
		}
	}
	_, out, _ := invoke(t, trackerSource(), "tracker")
	if !strings.Contains(out, "jira tickets") {
		t.Errorf("tracker heading does not name the tracker:\n%s", out)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// wf tracker reads the configured scope, not a query of its own.
func TestTrackerAsksJiraForTheConfiguredScope(t *testing.T) {
	t.Setenv("EXAMPLE_JIRA_EMAIL", "reader@example.com")
	t.Setenv("EXAMPLE_JIRA_TOKEN", "test-jira-token")
	t.Setenv("EXAMPLE_GITHUB_TOKEN", "test-github-token")
	var jqls []string
	connect := func(w *workfile.Workspace) source {
		p := w.Providers["jira"]
		p.SkipStatuses = workfile.Names{"Backlog"}
		p.Scope = "labels != spike"
		w.Providers["jira"] = p
		c := provider.New(w)
		c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			body := `{"cloudId":"cloud-example"}`
			if strings.HasSuffix(r.URL.Path, "/search/jql") {
				var in struct {
					JQL string `json:"jql"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				jqls = append(jqls, in.JQL)
				body = `{"issues":[],"isLast":true}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		return c
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"tracker", "--dir", "../../example"}, &out, &errOut, "test", connect)
	if code != 0 {
		t.Fatalf("%d %q", code, errOut.String())
	}
	want := `(project in ("APP") AND status not in ("Backlog") AND (statusCategory != Done OR resolved >= -14d) AND (labels != spike)) ORDER BY updated DESC`
	if len(jqls) != 1 || jqls[0] != want {
		t.Fatalf("got %q\nwant %q", jqls, want)
	}
}
