package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

// TestWordsNameTheTracker checks that a tracker's own name, not Jira, reaches
// the words people read.
func TestWordsNameTheTracker(t *testing.T) {
	workfile.Kinds["example"] = workfile.Kind{
		Label:   "Example",
		Auth:    []string{"token"},
		Tracker: &workfile.Role{Settings: []string{"site"}},
	}
	t.Cleanup(func() { delete(workfile.Kinds, "example") })

	w := &workfile.Workspace{
		Policy:    workfile.Policy{Tracker: "example", States: []string{"todo", "doing"}},
		Providers: map[string]workfile.Provider{"example": {Kind: "example"}},
	}
	var out, errOut bytes.Buffer
	e := env{
		view:   newView(&out),
		w:      w,
		client: &fakeSource{},
		o:      options{keys: []string{"APP-9"}, to: "doing"},
		errOut: &errOut,
	}
	_, err := checkMove(context.Background(), e)
	if err == nil {
		t.Fatal("want a not-visible error")
	}
	if !strings.Contains(err.Error(), "your Example token") {
		t.Fatalf("error does not name the tracker: %q", err)
	}
	if strings.Contains(err.Error(), "Jira") {
		t.Fatalf("error still says Jira: %q", err)
	}
}
