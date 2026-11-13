package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestTogglePick(t *testing.T) {
	order := []string{pickAll, "api", "web"}
	set := map[string]bool{}
	togglePick(set, "api")
	togglePick(set, "web")
	if got := strings.Join(chosenPicks(order, set), ","); got != "api,web" {
		t.Fatalf("picked %q", got)
	}
	togglePick(set, "api")
	if got := strings.Join(chosenPicks(order, set), ","); got != "web" {
		t.Fatalf("untick left %q", got)
	}
	togglePick(set, pickAll)
	if !set[pickAll] || set["web"] || len(chosenPicks(order, set)) != 0 {
		t.Fatalf("all did not clear the others: %v", set)
	}
	togglePick(set, "web")
	if set[pickAll] || !set["web"] {
		t.Fatalf("a repo did not clear all: %v", set)
	}
	togglePick(set, pickAll)
	togglePick(set, pickAll)
	if anyPicked(set) {
		t.Fatalf("all did not untick: %v", set)
	}
}

func TestMergeStatusesKeepsFirstSeenOrder(t *testing.T) {
	got := mergeStatuses([][]string{{"To Do", "Done"}, {"To Do", "Review", "Done"}})
	if strings.Join(got, ",") != "To Do,Done,Review" {
		t.Fatalf("merged %v", got)
	}
}

func TestSetupPickersFilterThenToggle(t *testing.T) {
	d, _ := newDriver(t, 60, setupFake{})
	d.skipGitHub()
	d.want("Which projects does your team work in?")
	d.press("/", "w", "e", "b", "enter")
	d.press("space")
	d.want("[✓] WEB")
	d.press("esc")
	d.want("[ ] APP")
	if got := strings.Join(d.m.projectPick.chosen(), ","); got != "WEB" {
		t.Fatalf("picked %q", got)
	}
	d.want("1 project picked")
}

func TestSetupProjectsNeedOne(t *testing.T) {
	for _, width := range []int{40, 60} {
		d, _ := newDriver(t, width, setupFake{})
		d.skipGitHub()
		d.want("Which projects does your team work in?")
		d.want("Nothing picked yet")
		d.press("enter")
		d.want("Pick at least one project")
		if d.m.step != stepProjects {
			t.Fatal("moved on with no project")
		}
	}
}

func TestSetupReposNeedOneAndAllClearsTheRest(t *testing.T) {
	for _, width := range []int{40, 60} {
		d, _ := newDriver(t, width, setupFake{})
		d.toGitHubChoice()
		d.press("y", "enter")
		d.typeText("example-team")
		d.press("enter")
		d.typeText("example-github-token")
		d.press("enter")
		d.want("Which repositories does your team ship from?")
		d.want("All repositories in")
		d.press("enter")
		d.want("Pick at least one repository")
		d.press("down", "space", "down", "space")
		d.want("2 repositories picked")
		d.press("esc")
		d.want("Create a GitHub token")
	}
}

func TestSetupWritesExplicitScope(t *testing.T) {
	for _, width := range []int{40, 60} {
		d, root := newDriver(t, width, setupFake{})
		d.toGitHubChoice()
		d.press("y", "enter")
		d.typeText("example-team")
		d.press("enter")
		d.typeText("example-github-token")
		d.press("enter")
		d.want("Which repositories does your team ship from?")
		d.press("down", "space", "down", "space", "enter")
		d.want("Which projects does your team work in?")
		d.press("space", "down", "space", "enter")
		d.want("Which statuses should Workfile track?")
		d.press("down", "space", "enter")
		d.finish()

		data, err := os.ReadFile(filepath.Join(root, ".workfile", "providers.yml"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{"projects:\n        - APP\n        - WEB", "keep_done_for: 14d", "skip_statuses:\n        - In Progress", "repos:\n        - api\n        - web"} {
			if !strings.Contains(strings.ReplaceAll(text, "    ", "  "), strings.ReplaceAll(want, "    ", "  ")) {
				t.Fatalf("missing %q in:\n%s", want, text)
			}
		}
		if strings.Contains(text, "scope") {
			t.Fatalf("scope was written:\n%s", text)
		}
		w, err := workfile.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(w.Providers["github"].Repos, ","); got != "api,web" {
			t.Fatalf("repos %q", got)
		}
	}
}

func TestSetupWritesEmptySkipStatusesAndAllRepos(t *testing.T) {
	d, root := newDriver(t, 60, setupFake{})
	d.toGitHubChoice()
	d.press("y", "enter")
	d.typeText("example-team")
	d.press("enter")
	d.typeText("example-github-token")
	d.press("enter")
	d.want("Which repositories does your team ship from?")
	d.press("space", "enter")
	d.want("Which projects does your team work in?")
	d.press("space", "enter", "enter")
	d.finish()
	data, err := os.ReadFile(filepath.Join(root, ".workfile", "providers.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "skip_statuses: []") || !strings.Contains(text, "repos: all") || strings.Contains(text, "scope") {
		t.Fatalf("settings not explicit:\n%s", text)
	}
	if _, err := workfile.Load(root); err != nil {
		t.Fatal(err)
	}
}
