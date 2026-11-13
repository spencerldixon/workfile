package board

import (
	"context"
	"errors"
	"testing"

	"workfile/internal/workfile"
)

type fake struct {
	tickets  []workfile.Ticket
	warnings []string
	limit    int
	err      error
}

func (f *fake) Tickets(_ context.Context, keys []string, limit int) ([]workfile.Ticket, error) {
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	var out []workfile.Ticket
	for _, t := range f.tickets {
		if len(keys) == 0 || contains(keys, t.Key) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fake) Records(_ context.Context, tickets []workfile.Ticket) ([]workfile.Ticket, []string, error) {
	return tickets, f.warnings, nil
}

func contains(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func workspace(t *testing.T) *workfile.Workspace {
	t.Helper()
	w, err := workfile.Load("../../example/branching")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func tk(key, assignee string) workfile.Ticket {
	return workfile.Ticket{Key: key, State: "review", Assignee: assignee, Facts: map[string]any{}}
}

func keys(r Result) []string {
	var out []string
	for _, a := range r.Assessments {
		out = append(out, a.Ticket.Key)
	}
	return out
}

func TestReadNamedAndMissing(t *testing.T) {
	w := workspace(t)
	src := &fake{tickets: []workfile.Ticket{tk("EX-1", "a"), tk("EX-2", "b")}, warnings: []string{"slow"}}
	r, err := Read(context.Background(), w, src, Query{Keys: []string{"EX-1", "EX-9"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(r); len(got) != 1 || got[0] != "EX-1" {
		t.Fatalf("assessed %v", got)
	}
	if len(r.Missing) != 1 || r.Missing[0] != "EX-9" {
		t.Fatalf("missing = %v", r.Missing)
	}
	if len(r.Warnings) != 1 || r.Warnings[0] != "slow" {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

func TestReadWhoFilterThenCut(t *testing.T) {
	w := workspace(t)
	who := map[string]string{w.Policy.Tracker: "alice"}
	src := &fake{tickets: []workfile.Ticket{tk("EX-1", "bob"), tk("EX-2", "alice"), tk("EX-3", "alice"), tk("EX-4", "alice")}}
	r, err := Read(context.Background(), w, src, Query{Who: who, Cut: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(r); len(got) != 2 || got[0] != "EX-2" || got[1] != "EX-3" {
		t.Fatalf("assessed %v", got)
	}
	if src.limit != 0 {
		t.Fatalf("read limit = %d, want 0", src.limit)
	}
}

func TestReadLimitPassedToSource(t *testing.T) {
	src := &fake{tickets: []workfile.Ticket{tk("EX-1", "a"), tk("EX-2", "a"), tk("EX-3", "a")}}
	r, err := Read(context.Background(), workspace(t), src, Query{Limit: 2, Cut: 2})
	if err != nil {
		t.Fatal(err)
	}
	if src.limit != 2 || len(r.Assessments) != 2 {
		t.Fatalf("limit = %d, assessed %d", src.limit, len(r.Assessments))
	}
}

func TestReadError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Read(context.Background(), workspace(t), &fake{err: boom}, Query{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestPerson(t *testing.T) {
	w := &workfile.Workspace{
		Providers: map[string]workfile.Provider{"jira": {}, "github": {}},
		People: map[string]map[string]string{
			"alice": {"jira": "a-1", "github": "example-alice", "name": "Alice"},
			"bob":   {"name": "Bob"},
		},
	}
	who, err := Person(w, "alice")
	if err != nil || len(who) != 2 || who["jira"] != "a-1" || who["github"] != "example-alice" {
		t.Fatalf("who = %v, err = %v", who, err)
	}
	if _, err := Person(w, "bob"); err == nil {
		t.Fatal("bob has no identities")
	}
	if _, err := Person(w, "charlie"); err == nil {
		t.Fatal("charlie is not in people.yml")
	}
}
