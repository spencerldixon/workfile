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

// withKind adds a test kind for one test.
func withKind(t *testing.T, name string, kind Kind) {
	t.Helper()
	Kinds[name] = kind
	t.Cleanup(func() { delete(Kinds, name) })
}

func TestFactsFollowTheRole(t *testing.T) {
	both := Kind{
		Tracker:  &Role{Facts: slices.Clone(TrackerFacts)},
		CodeHost: &Role{Facts: slices.Clone(CodeHostFacts)},
	}
	withKind(t, "both", both)
	w := example(t)
	w.Providers["issues"] = Provider{Kind: "both"}
	w.Policy.Tracker = "issues"
	e, err := w.parseExpression("issues.summary > 0")
	if err != nil || e.Record() {
		t.Fatalf("tracker summary: %v, record %v", err, e.Record())
	}
	if _, err := w.parseExpression("issues.approvals >= 1"); err == nil || !strings.Contains(err.Error(), "has no attribute") {
		t.Fatalf("tracker approvals: %v", err)
	}
	w.Policy.Tracker = "jira"
	e, err = w.parseExpression("issues.approvals >= 1")
	if err != nil || !e.Record() {
		t.Fatalf("host approvals: %v, record %v", err, e.Record())
	}
	if _, err := w.parseExpression("issues.summary > 0"); err == nil || !strings.Contains(err.Error(), "has no attribute") {
		t.Fatalf("host summary: %v", err)
	}
	if e, err := w.parseExpression("issues.prs > 0"); err != nil || e.Record() {
		t.Fatalf("prs: %v, record %v", err, e.Record())
	}
}

func TestParseExpressionSurvivesABadKind(t *testing.T) {
	w := example(t)
	w.Providers["bad"] = Provider{Kind: "nope"}
	if _, err := w.parseExpression("bad.summary > 0"); err == nil {
		t.Fatal("want an error")
	}
}

func byProblems(t *testing.T, w *Workspace, expr string) string {
	t.Helper()
	w.Policy.Gates = map[string][]Rule{"review": {{If: expr, By: []string{"alice"}}}}
	return problems(t, w)
}

func TestByFollowsTheRole(t *testing.T) {
	w := example(t)
	if got := byProblems(t, w, "github.approvals >= 1"); strings.Contains(got, "by is not supported") {
		t.Fatalf("approvals: %q", got)
	}
	if got := byProblems(t, w, "jira.labels contains bug"); strings.Contains(got, "by is not supported") {
		t.Fatalf("labels: %q", got)
	}
	if got := byProblems(t, w, "github.checks == passing"); !strings.Contains(got, "by is not supported for github.checks") {
		t.Fatalf("checks: %q", got)
	}
}

func TestOptionalFactNeedsTheKind(t *testing.T) {
	host := Kind{CodeHost: &Role{Facts: slices.Clone(CodeHostFacts)}}
	withKind(t, "plainhost", host)
	w := example(t)
	w.Providers["plain"] = Provider{Kind: "plainhost"}
	if _, err := w.parseExpression("plain.merge_queue == true"); err == nil || !strings.Contains(err.Error(), "has no attribute merge_queue") {
		t.Fatalf("got %v", err)
	}
	if _, err := w.parseExpression("plain.state == open"); err != nil {
		t.Fatal(err)
	}
}
