package workfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func trackerWorkspace(t *testing.T, global, shared, private string) *Workspace {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "personal"))
	t.Setenv("WORKFILE_CONFIG_FILE", filepath.Join(PersonalDir(), "config.yml"))
	write := func(path, text string) {
		t.Helper()
		if text == "" {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", ".workfile", name))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(root, ".workfile", name), string(data))
	}
	write(PersonalPath("config.yml"), global)
	write(filepath.Join(root, ".workfile", "config.yml"), shared)
	write(WorkspaceSettingsPath("example-web"), private)
	w, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestTrackerSettingsNeedNoFiles(t *testing.T) {
	w := trackerWorkspace(t, "", "", "")
	s, err := ReadTrackerSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if s.DefaultView != "all" || !s.ShowComments() || s.Preview.Workflow != "auto" || len(s.Views) != 0 || !reflect.DeepEqual(s.Columns, []string{"key", "title", "status", "assignee", "action"}) {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	for _, path := range []string{PersonalPath("config.yml"), filepath.Join(w.Dir, "config.yml"), WorkspaceSettingsPath(w.Policy.Name)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("reading defaults created %s", path)
		}
	}
}

func TestTrackerSettingsLayerPrivateOverSharedOverGlobal(t *testing.T) {
	w := trackerWorkspace(t, `me:
  jira: example-alice
tracker:
  default_view: me
  columns: [key, title, status]
  preview:
    comments: false
    workflow: text
  views:
    - id: my-work
      title: Global work
      filter:
        assignee: me
        states: [doing]
    - id: review-queue
      title: Global review
      filter:
        states: [review]
`, `tracker:
  default_view: review-queue
  columns: [status, key, title]
  preview:
    workflow: off
  views:
    - id: review-queue
      title: Team review
      filter:
        states: [review]
`, `only_mine: true
tracker:
  default_view: my-work
  columns: [title, key, assignee]
  preview:
    comments: true
  views:
    - id: my-work
      title: My active work
      filter:
        assignee: me
        states: [doing, review]
    - id: unassigned
      title: Unassigned
      filter:
        assignee: unassigned
`)
	s, err := ReadTrackerSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if s.DefaultView != "my-work" || !reflect.DeepEqual(s.Columns, []string{"title", "key", "assignee"}) || !s.ShowComments() || s.Preview.Workflow != "off" {
		t.Fatalf("wrong preferences: %+v", s)
	}
	if len(s.Views) != 3 || s.Views[0].Title != "My active work" || s.Views[1].Title != "Team review" || s.Views[2].ID != "unassigned" {
		t.Fatalf("views did not merge by id in order: %+v", s.Views)
	}
	if !w.Settings.OnlyMine {
		t.Fatal("existing workspace preference was lost")
	}
	p, err := ReadPersonal()
	if err != nil || p.Me["jira"] != "example-alice" {
		t.Fatalf("tracker config broke identity: %+v, %v", p, err)
	}
}

func TestTrackerSettingsPartialOverridesKeepFalseAndInherit(t *testing.T) {
	w := trackerWorkspace(t, "tracker:\n  preview:\n    comments: false\n", "tracker:\n  default_view: me\n", "tracker:\n  columns: [key, title]\n")
	s, err := ReadTrackerSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if s.ShowComments() || s.Preview.Workflow != "auto" || s.DefaultView != "me" || len(s.Columns) != 2 {
		t.Fatalf("partial settings erased inherited values: %+v", s)
	}
}

func TestTrackerSettingsPrivateViewReplacesWholeDefinition(t *testing.T) {
	w := trackerWorkspace(t, "", `tracker:
  views:
    - id: my-work
      title: Team default
      filter:
        states: [doing]
        assignee: me
`, `tracker:
  views:
    - id: my-work
      title: All review
      filter:
        states: [review]
`)
	s, err := ReadTrackerSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Views) != 1 || s.Views[0].Filter.Assignee != "" || !reflect.DeepEqual(s.Views[0].Filter.States, []string{"review"}) {
		t.Fatalf("view inherited a stale filter: %+v", s.Views)
	}
}

func TestTrackerSettingsRejectInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"unknown field", "tracker:\n  colums: [key, title]\n", "invalid YAML"},
		{"unknown filter", "tracker:\n  views: [{id: work, title: Work, filter: {jql: SECRET_VALUE}}]\n", "invalid YAML"},
		{"empty views", "tracker:\n  views: []\n", "views must contain"},
		{"hidden default", "tracker:\n  default_view: all\n  views: [{id: work, title: Work}]\n", "default_view"},
		{"empty columns", "tracker:\n  columns: []\n", "columns must include"},
		{"missing key", "tracker:\n  columns: [title, status]\n", "columns must include"},
		{"missing title", "tracker:\n  columns: [key, status]\n", "columns must include"},
		{"duplicate column", "tracker:\n  columns: [key, title, title]\n", "unique names"},
		{"unknown attention", "tracker:\n  views: [{id: work, title: Work, filter: {attention: someday}}]\n", "filter.attention"},
		{"unknown column", "tracker:\n  columns: [key, title, priority]\n", "unique names"},
		{"workflow mode", "tracker:\n  preview: {workflow: kitty}\n", "preview.workflow"},
		{"comment type", "tracker:\n  preview: {comments: sometimes}\n", "invalid YAML"},
		{"default view", "tracker:\n  default_view: missing\n", "default_view"},
		{"reserved all", "tracker:\n  views: [{id: all, title: Everything}]\n", "conflicts"},
		{"reserved stage", "tracker:\n  views: [{id: doing, title: Doing}]\n", "conflicts"},
		{"duplicate id", "tracker:\n  views: [{id: work, title: Work}, {id: work, title: More}]\n", "unique lowercase"},
		{"empty title", "tracker:\n  views: [{id: work}]\n", "title"},
		{"control title", "tracker:\n  views: [{id: work, title: \"bad\\nline\"}]\n", "title"},
		{"unsafe id", "tracker:\n  views: [{id: ../work, title: Work}]\n", "unique lowercase"},
		{"unknown stage", "tracker:\n  views: [{id: work, title: Work, filter: {states: [missing]}}]\n", "policy.yml"},
		{"duplicate stage", "tracker:\n  views: [{id: work, title: Work, filter: {states: [doing, doing]}}]\n", "unique, nonempty"},
		{"arbitrary assignee", "tracker:\n  views: [{id: work, title: Work, filter: {assignee: example-alice}}]\n", "assignee"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := trackerWorkspace(t, "", tc.yaml, "")
			_, err := ReadTrackerSettings(w)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), "SECRET_VALUE") {
				t.Fatal("config error echoed a value")
			}
		})
	}
}

func TestTrackerSettingsDefaultToFirstConfiguredView(t *testing.T) {
	w := trackerWorkspace(t, "", "tracker:\n  views: [{id: work, title: Work}, {id: other, title: Other}]\n", "")
	s, err := ReadTrackerSettings(w)
	if err != nil || s.DefaultView != "work" {
		t.Fatalf("default must be the first configured view: %+v, %v", s, err)
	}
}

func TestTrackerSettingsUnnamedWorkspaceSkipsPrivateLayer(t *testing.T) {
	w := trackerWorkspace(t, "tracker:\n  default_view: me\n", "", "")
	w.Policy.Name = ""
	var err error
	w.Settings, err = ReadWorkspaceSettings("")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ReadTrackerSettings(w)
	if err != nil || s.DefaultView != "me" {
		t.Fatalf("unnamed workspace lost personal defaults: %+v, %v", s, err)
	}
}
