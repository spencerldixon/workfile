package workfile

import (
	"strings"
	"testing"
)

func TestTrackerBoardSettingsAndNeutralOriginFilter(t *testing.T) {
	w := trackerWorkspace(t, "", `tracker:
  views:
    - id: flow
      title: Board
      layout: board
      attention: icons
      filter:
        origins: [example-app, example-api]
`, "")
	s, err := ReadTrackerSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	v := s.Views[0]
	if s.DefaultView != "flow" || v.Layout != "board" || v.Attention != "icons" || len(v.Filter.Origins) != 2 {
		t.Fatalf("settings lost: %+v", s)
	}
}

func TestTrackerBoardRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ field, want string }{
		{"layout: kanbanish", "views.layout"},
		{"attention: rainbow", "views.attention"},
		{"filter: {origins: ['']}", "filter.origins"},
		{"filter: {origins: [example-app, EXAMPLE-APP]}", "filter.origins"},
		{"filter: {origins: [\"bad\\nsource\"]}", "filter.origins"},
	} {
		t.Run(tc.want+tc.field, func(t *testing.T) {
			w := trackerWorkspace(t, "", "tracker:\n  views:\n    - id: flow\n      title: Board\n      "+tc.field+"\n", "")
			_, err := ReadTrackerSettings(w)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %s, got %v", tc.want, err)
			}
		})
	}
}
