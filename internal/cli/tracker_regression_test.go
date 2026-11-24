package cli

import (
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestTrackerConfiguredViewsDefineVisibleTabs(t *testing.T) {
	s := customTrackerSettings()
	s.DefaultView = "my-work"
	d := driveWithSettings(t, trackerSource(), 160, 40, graphics{}, options{}, s)
	if got := len(d.m.views()); got != len(s.Views) {
		t.Fatalf("configured %d views but got %d tabs", len(s.Views), got)
	}
	header := d.m.navigation()
	for _, unwanted := range []string{"All", "Assigned to me", "Todo", "Doing", "Review", "Done"} {
		if strings.Contains(header, unwanted) {
			t.Fatalf("unconfigured tab %q is still visible: %s", unwanted, header)
		}
	}
}

func TestTrackerPreferencesWithoutViewsKeepBuiltInTabs(t *testing.T) {
	s := workfile.TrackerSettings{DefaultView: "me", Columns: []string{"key", "title"}}
	d := driveWithSettings(t, trackerSource(), 160, 40, graphics{}, options{}, s)
	if got := len(d.m.views()); got != len(d.m.e.w.Policy.States)+2 {
		t.Fatalf("preferences without views lost built-in tabs: %d", got)
	}
	if !d.m.shownView().me || listKeys(d) != "APP-2" {
		t.Fatal("default_view preference was not applied")
	}
}

// A branching eight-stage workflow with two returns.
func branchingTrackerPolicy() workfile.Policy {
	return workfile.Policy{
		States: []string{"todo", "ready", "blocked", "doing", "review", "queued", "done", "rejected"},
		Transitions: map[string]workfile.Transition{
			"todo":     {To: []string{"ready", "doing", "blocked", "rejected"}},
			"ready":    {To: []string{"doing", "blocked"}},
			"blocked":  {To: []string{"doing"}},
			"doing":    {To: []string{"review", "blocked"}},
			"review":   {To: []string{"queued", "todo"}},
			"queued":   {To: []string{"done"}},
			"done":     {End: true},
			"rejected": {End: true},
		},
	}
}
