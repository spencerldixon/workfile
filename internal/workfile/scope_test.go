package workfile

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func messages(check func(string, Provider, func(string, ...any)), name string, p Provider) []string {
	var got []string
	check(name, p, func(format string, args ...any) { got = append(got, fmt.Sprintf(format, args...)) })
	return got
}

func TestScopeSettingsAreChecked(t *testing.T) {
	jira := Provider{Kind: "jira", Site: "example-team.atlassian.net", Projects: Names{"APP"}, KeepDoneFor: "14d"}
	tracker := Provider{Kind: "github", Org: "example-team", Project: 7, Repos: Names{"web"}, KeepDoneFor: "2w"}
	host := Provider{Kind: "github", Org: "example-team", Repos: Names{"web", "api"}}
	for _, tc := range []struct {
		name  string
		check func(string, Provider, func(string, ...any))
		edit  func(p *Provider)
		base  Provider
		want  string
	}{
		{"jira sound", checkJira, func(p *Provider) {}, jira, ""},
		{"jira no projects", checkJira, func(p *Provider) { p.Projects = nil }, jira, "X.projects is required, such as projects: [APP]. scope is now an extra filter on top."},
		{"jira bad project", checkJira, func(p *Provider) { p.Projects = Names{"app"} }, jira, "X.projects must be Jira project keys"},
		{"jira no keep", checkJira, func(p *Provider) { p.KeepDoneFor = "" }, jira, "X.keep_done_for is required, such as keep_done_for: 14d"},
		{"jira bad keep", checkJira, func(p *Provider) { p.KeepDoneFor = "14" }, jira, "X.keep_done_for is required"},
		{"jira zero keep", checkJira, func(p *Provider) { p.KeepDoneFor = "0d" }, jira, "X.keep_done_for is required"},
		{"tracker sound", checkGitHubTracker, func(p *Provider) {}, tracker, ""},
		{"tracker all", checkGitHubTracker, func(p *Provider) { p.Repos = Names{"all"} }, tracker, ""},
		{"tracker no repos", checkGitHubTracker, func(p *Provider) { p.Repos = nil }, tracker, "X.repos is required"},
		{"tracker no keep", checkGitHubTracker, func(p *Provider) { p.KeepDoneFor = "" }, tracker, "X.keep_done_for is required"},
		{"tracker view", checkGitHubTracker, func(p *Provider) { p.View = 3 }, tracker, "X.view is no longer read. Write the filter in providers.yml: repos, skip_statuses, keep_done_for and scope."},
		{"tracker bad repo", checkGitHubTracker, func(p *Provider) { p.Repos = Names{"we b"} }, tracker, "X.repos must be repository names"},
		{"host sound", checkGitHub, func(p *Provider) {}, host, ""},
		{"host all", checkGitHub, func(p *Provider) { p.Repos = Names{"all"} }, host, ""},
		{"host no repos", checkGitHub, func(p *Provider) { p.Repos = nil }, host, "X.repos is required: list the team's repositories, such as repos: [web, api], or write repos: all"},
		{"host all and more", checkGitHub, func(p *Provider) { p.Repos = Names{"all", "web"} }, host, "X.repos can say all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.base
			tc.edit(&p)
			got := messages(tc.check, "X", p)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("sound settings rejected: %q", got)
				}
				return
			}
			if !slices.ContainsFunc(got, func(m string) bool { return strings.Contains(m, tc.want) }) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAllRepos(t *testing.T) {
	for repos, want := range map[string]bool{"all": true, "web": false, "": false} {
		var p Provider
		if repos != "" {
			p.Repos = Names{repos}
		}
		if p.AllRepos() != want {
			t.Errorf("AllRepos(%q) = %v", repos, !want)
		}
	}
	if (Provider{Repos: Names{"all", "web"}}).AllRepos() {
		t.Error("all with another repo is not all")
	}
}

func TestKeyProject(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Provider
		want string
	}{
		{"default wins", Provider{Kind: "jira", DefaultProject: "WEB", Projects: Names{"APP", "WEB"}}, "WEB"},
		{"first project", Provider{Kind: "jira", Projects: Names{"APP", "WEB"}}, "APP"},
		{"github unaffected", Provider{Kind: "github", Projects: Names{"APP"}, Repos: Names{"web"}}, ""},
		{"github default", Provider{Kind: "github", DefaultProject: "app"}, "app"},
	} {
		if got := tc.p.KeyProject(); got != tc.want {
			t.Errorf("%s: KeyProject = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestEveryExampleDeclaresItsScope(t *testing.T) {
	dirs, err := filepath.Glob("../../example/*/.workfile")
	if err != nil {
		t.Fatal(err)
	}
	dirs = append(dirs, "../../example/.workfile")
	if len(dirs) < 4 {
		t.Fatalf("found only %v", dirs)
	}
	for _, dir := range dirs {
		if _, err := Load(filepath.Dir(dir)); err != nil {
			t.Errorf("%s does not load: %v", dir, err)
		}
	}
}
