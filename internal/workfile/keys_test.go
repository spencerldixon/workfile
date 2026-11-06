package workfile

import (
	"slices"
	"strings"
	"testing"
)

func TestDashKeysParse(t *testing.T) {
	k := dashKeys{site: "example-team.atlassian.net", path: "/browse/"}
	for _, tc := range []struct{ in, project, want string }{
		{"app-5", "", "APP-5"},
		{"02713", "WEB", "WEB-2713"},
		{"0", "WEB", "WEB-0"},
	} {
		got, err := k.Parse(tc.in, tc.project)
		if err != nil || got != tc.want {
			t.Fatalf("Parse(%q, %q) = %q, %v; want %q", tc.in, tc.project, got, err, tc.want)
		}
	}
	if _, err := k.Parse("2713", ""); err == nil || !strings.Contains(err.Error(), "set default_project in providers.yml") {
		t.Fatalf("want default_project error, got %v", err)
	}
	if _, err := k.Parse("app 5", "WEB"); err == nil || !strings.Contains(err.Error(), "is not a ticket key") {
		t.Fatalf("want not a ticket key, got %v", err)
	}
}

func TestDashKeysMentions(t *testing.T) {
	k := dashKeys{}
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"APP-12 fix", []string{"APP-12"}},
		{"feature/app-12-login", []string{"APP-12"}},
		{"MYAPP-12", []string{"MYAPP-12"}},
		{"APP-123", []string{"APP-123"}},
		{"APP-12 and app-12 and APP-7", []string{"APP-12", "APP-7"}},
		{"nothing here", nil},
	} {
		if got := k.Mentions(tc.text); !slices.Equal(got, tc.want) {
			t.Fatalf("Mentions(%q) = %v; want %v", tc.text, got, tc.want)
		}
	}
	if slices.Contains(k.Mentions("MYAPP-12"), "APP-12") {
		t.Fatal("MYAPP-12 must not match APP-12")
	}
}

func TestDashKeysLinks(t *testing.T) {
	k := dashKeys{site: "example-team.atlassian.net", path: "/browse/"}
	for _, tc := range []struct {
		body string
		want int
	}{
		{"Related APP-42", 0},
		{"[Ticket](https://example-team.atlassian.net/browse/APP-42)", 1},
		{"https://other.example.com/browse/APP-42", 0},
		{"https://example-team.atlassian.net.evil.example.com/browse/APP-42", 0},
		{"https://example-team.atlassian.net/browse/APP-42/more", 0},
		{"https://example-team.atlassian.net/browse/APP-42?focusedCommentId=1", 1},
		{"https://example-alice@example-team.atlassian.net/browse/APP-42", 0},
	} {
		if got := k.Links(tc.body); len(got) != tc.want {
			t.Fatalf("%q: %v", tc.body, got)
		}
	}
}

func TestDashKeysSearch(t *testing.T) {
	if got := (dashKeys{}).Search("APP-1"); got != "APP-1" {
		t.Fatalf("got %q", got)
	}
}

func TestTrackerKeysUsesTrackerKind(t *testing.T) {
	w := &Workspace{
		Policy:    Policy{Tracker: "jira"},
		Providers: map[string]Provider{"jira": {Kind: "jira", Site: "example-team.atlassian.net"}},
	}
	k := w.TrackerKeys()
	if got := k.Links("https://example-team.atlassian.net/browse/APP-1"); !slices.Equal(got, []string{"APP-1"}) {
		t.Fatalf("got %v", got)
	}
}
