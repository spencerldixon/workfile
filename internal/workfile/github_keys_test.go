package workfile

import (
	"slices"
	"strings"
	"testing"
)

func TestGitHubKeys(t *testing.T) {
	k := githubKeys{org: "example-team"}
	for _, tc := range []struct{ in, project, want string }{
		{"App#42", "", "app#42"},
		{"app#042", "", "app#42"},
		{"my.app-2_x#7", "", "my.app-2_x#7"},
		{"42", "app", "app#42"},
		{"#42", "app", "app#42"},
		{"042", "app", "app#42"},
	} {
		if got, err := k.Parse(tc.in, tc.project); err != nil || got != tc.want {
			t.Errorf("Parse(%q, %q) = %q, %v; want %q", tc.in, tc.project, got, err, tc.want)
		}
	}
	for _, in := range []string{"42", "#42"} {
		if _, err := k.Parse(in, ""); err == nil || !strings.Contains(err.Error(), "default_project") {
			t.Errorf("Parse(%q) with no project: %v", in, err)
		}
	}
	for _, in := range []string{"APP-42", "app#", "app#x", "a b#1", "app#0", "#0", "app#99999999999"} {
		if got, err := k.Parse(in, "app"); err == nil {
			t.Errorf("Parse(%q) = %q; want an error", in, got)
		}
	}
	if got := k.Search("app#42"); got != "app#42" {
		t.Errorf("Search = %q", got)
	}
}

func TestGitHubKeysMentions(t *testing.T) {
	k := githubKeys{org: "example-team"}
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"see app#42 and app#42x", []string{"app#42"}},
		{"App#42 and app#42 and web#7", []string{"app#42", "web#7"}},
		{"feature/app#12-login", []string{"app#12"}},
		{"Resolves #42", nil},
		{"app#123", []string{"app#123"}},
		{"nothing here", nil},
	} {
		if got := k.Mentions(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("Mentions(%q) = %v; want %v", tc.text, got, tc.want)
		}
	}
}

func TestGitHubKeysLinks(t *testing.T) {
	k := githubKeys{org: "example-team"}
	for _, tc := range []struct {
		body string
		want []string
	}{
		{"https://github.com/example-team/app/issues/42", []string{"app#42"}},
		{"(https://github.com/example-team/App/issues/42).", []string{"app#42"}},
		{"https://github.com/Example-Team/app/issues/42", []string{"app#42"}},
		{"https://github.com/example-team/app/issues/42#issuecomment-1", []string{"app#42"}},
		{"https://github.com/other-team/app/issues/42", nil},
		{"https://github.com/example-team/app/pull/42", nil},
		{"https://github.com/example-team/app/issues/42/more", nil},
		{"https://github.com.example.com/example-team/app/issues/42", nil},
		{"https://example-alice@github.com/example-team/app/issues/42", nil},
		{"app#42", nil},
	} {
		if got := k.Links(tc.body); !slices.Equal(got, tc.want) {
			t.Errorf("Links(%q) = %v; want %v", tc.body, got, tc.want)
		}
	}
}

func TestGitHubCanBeTheTracker(t *testing.T) {
	w := example(t)
	p := Provider{Kind: "github", Org: "example-team", Project: 7, View: 3, DefaultProject: "app", Statuses: w.Providers["jira"].Statuses, Auth: w.Providers["github"].Auth}
	delete(w.Providers, "jira")
	w.Providers["issues"] = p
	w.Policy.Tracker = "issues"
	got := problems(t, w)
	if strings.Contains(got, "issues.") || strings.Contains(got, "tracker must name") {
		t.Errorf("a sound GitHub tracker was rejected: %q", got)
	}
	if _, ok := w.TrackerKeys().(githubKeys); !ok {
		t.Errorf("tracker keys are %T", w.TrackerKeys())
	}
}

func TestGitHubTrackerSettingsAreChecked(t *testing.T) {
	for _, tc := range []struct {
		p    Provider
		want string
	}{
		{Provider{Org: "example-team"}, "issues.project"},
		{Provider{Org: "example-team", Project: -1}, "issues.project"},
		{Provider{Org: "example-team", Project: 7, DefaultProject: "APP"}, "issues.default_project"},
		{Provider{Org: "not an org", Project: 7}, "issues.org"},
	} {
		var got []string
		checkGitHubTracker("issues", tc.p, func(format string, args ...any) { got = append(got, format) })
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, strings.TrimPrefix(tc.want, "issues")) }) {
			t.Errorf("%+v: got %q, want %s", tc.p, got, tc.want)
		}
	}
	var got []string
	checkGitHubTracker("issues", Provider{Org: "example-team", Project: 7, DefaultProject: "my.app-2_x"}, func(format string, args ...any) { got = append(got, format) })
	if len(got) != 0 {
		t.Errorf("sound settings rejected: %q", got)
	}
}
