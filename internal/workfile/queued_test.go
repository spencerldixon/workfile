package workfile

import "testing"

func TestDefaultProjectMustBeAJiraProjectKey(t *testing.T) {
	for project, valid := range map[string]bool{"": true, "APP": true, "AI": true, "A1_B": true, "app": false, "AP P": false, "1APP": false, "APP-1": false} {
		w := example(t)
		p := w.Providers["jira"]
		p.DefaultProject = project
		w.Providers["jira"] = p
		err := w.validate()
		if valid && err != nil {
			t.Errorf("%q rejected: %v", project, err)
		}
		if !valid && err == nil {
			t.Errorf("%q accepted", project)
		}
	}
}

func TestDefaultProjectIsNotAllowedOnGitHub(t *testing.T) {
	w := example(t)
	p := w.Providers["github"]
	p.DefaultProject = "APP"
	w.Providers["github"] = p
	if err := w.validate(); err == nil {
		t.Fatal("default_project on a GitHub instance was accepted")
	}
}

// queuedWorkflow is todo -> review -> queued -> done.
func queuedWorkflow(t *testing.T) *Workspace {
	t.Helper()
	w := example(t)
	w.Automations = nil // a different workflow: the example's automations do not apply
	w.Policy.States = []string{"todo", "review", "queued", "done"}
	w.Policy.Transitions = map[string]Transition{
		"todo":   {To: []string{"review"}},
		"review": {To: []string{"queued", "todo"}},
		"queued": {To: []string{"done"}},
		"done":   {End: true},
	}
	p := w.Providers["jira"]
	p.Statuses = map[string]Names{"todo": {"To Do"}, "review": {"In Review"}, "queued": {"Queued for Release"}, "done": {"Done"}}
	w.Providers["jira"] = p
	return w
}

func TestQueuedStageSitsBetweenReviewAndDone(t *testing.T) {
	w := queuedWorkflow(t)
	if err := w.validate(); err != nil {
		t.Fatal(err)
	}
	tk := Ticket{Key: "APP-1", State: "queued", Facts: Facts{}}
	a := w.Assess(tk)
	if a.Ticket.Error != "" || len(a.Routes) != 1 || a.Routes[0].To != "done" || !a.Routes[0].Available() {
		t.Fatalf("a queued ticket should be ready to move to done: %+v", a.Routes)
	}
	review := w.Assess(Ticket{Key: "APP-2", State: "review", Facts: Facts{}})
	for _, r := range review.Routes {
		if r.To == "done" {
			t.Fatal("review must not go straight to done")
		}
	}
}

func TestQueuedNeedsAJiraStatus(t *testing.T) {
	w := queuedWorkflow(t)
	p := w.Providers["jira"]
	delete(p.Statuses, "queued")
	w.Providers["jira"] = p
	if err := w.validate(); err == nil {
		t.Fatal("a stage with no Jira status was accepted")
	}
}
