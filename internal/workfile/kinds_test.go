package workfile

import (
	"slices"
	"strings"
	"testing"
)

func problems(t *testing.T, w *Workspace) string {
	t.Helper()
	err := w.validate()
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestUnknownKindIsRejected(t *testing.T) {
	w := example(t)
	p := w.Providers["github"]
	p.Kind = "example"
	w.Providers["github"] = p
	if got := problems(t, w); !strings.Contains(got, "must use a known provider") {
		t.Fatalf("got %q", got)
	}
}

func TestSettingOutsideTheRoleIsRejected(t *testing.T) {
	w := example(t)
	p := w.Providers["github"]
	p.Site = "example-team.atlassian.net"
	w.Providers["github"] = p
	got := problems(t, w)
	if !strings.Contains(got, "github") || !strings.Contains(got, "site") {
		t.Fatalf("got %q", got)
	}
}

func TestJiraMustBeTheTracker(t *testing.T) {
	w := example(t)
	w.Providers["jira2"] = w.Providers["jira"]
	if got := problems(t, w); !strings.Contains(got, "jira2") {
		t.Fatalf("got %q", got)
	}
}

func TestKindMustBeAbleToBeTheTracker(t *testing.T) {
	w := example(t)
	w.Policy.Tracker = "github"
	if got := problems(t, w); !strings.Contains(got, "tracker must name") {
		t.Fatalf("got %q", got)
	}
}

func TestOldProvidersFilesStillLoad(t *testing.T) {
	w := example(t)
	if w.Providers["github"].Lookback == "" {
		t.Fatal("example should still set lookback")
	}
	p := w.Providers["github"]
	p.Kind = ""
	w.Providers["github"] = p
	if got := problems(t, w); got != "" {
		t.Fatalf("got %q", got)
	}
	if w.Providers["github"].Kind != "github" {
		t.Fatal("kind should default to the name")
	}
}

func TestEveryRoleListsTheStandardFacts(t *testing.T) {
	for name, kind := range Kinds {
		if kind.Tracker != nil {
			for _, f := range TrackerFacts {
				if !slices.Contains(kind.Tracker.Facts, f) {
					t.Errorf("%s tracker lacks %s", name, f)
				}
			}
		}
		if kind.CodeHost != nil {
			for _, f := range CodeHostFacts {
				if !slices.Contains(kind.CodeHost.Facts, f) {
					t.Errorf("%s code host lacks %s", name, f)
				}
			}
		}
	}
}

func TestRoleOfIsNilSafe(t *testing.T) {
	w := example(t)
	if w.RoleOf("jira") == nil || w.RoleOf("github") == nil {
		t.Fatal("example roles missing")
	}
	if !w.IsTracker("jira") || w.IsTracker("github") || w.TrackerLabel() != "Jira" {
		t.Fatal("tracker helpers wrong")
	}
	w.Providers["bad"] = Provider{Kind: "nope"}
	if w.RoleOf("bad") != nil || w.RoleOf("missing") != nil {
		t.Fatal("want nil")
	}
	w.Policy.Tracker = "missing"
	if w.TrackerLabel() == "" {
		t.Fatal("label should have a fallback")
	}
}
