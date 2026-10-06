package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func TestJSONReportSchemaAndPlainSafety(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tk := fixtureTicket("APP-42", "review", strings.Repeat("x", 120))
	tk.Title = "Settings\x1b\n"
	tk.Records = map[string][]workfile.Record{"github": {{ID: "api#1", URL: "https://github.com/example-team/api/pull/1", Facts: workfile.Facts{"checks": "success", "labels": []string{}, "approvals": []workfile.Actor{}}}}}
	for _, command := range []string{"status", "health"} {
		code, out, errOut := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, command, "--json")
		wantCode := 1
		if command == "health" {
			wantCode = 0
		}
		if code != wantCode || errOut != "" || strings.Contains(out, "\x1b") {
			t.Fatalf("%d %s %s", code, out, errOut)
		}
		var r machineReport
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		if r.SchemaVersion != 1 || r.Command != command || r.ExitCode != code || !strings.HasPrefix(r.PolicyHash, "sha256:") {
			t.Fatalf("bad envelope: %+v", r)
		}
		if _, err := time.Parse(time.RFC3339Nano, r.ObservedAt); err != nil {
			t.Fatal(err)
		}
		if r.Coverage.Scope == "" || len(r.Coverage.Providers) != 2 || r.Coverage.Boundary == "" || r.Coverage.Sample != (command == "status") {
			t.Fatalf("missing coverage: %+v", r.Coverage)
		}
		ticket := r.Tickets[0]
		if ticket.Title != "Settings  " || len(ticket.Routes) == 0 || len(ticket.Unmet) != 2 {
			t.Fatalf("bad ticket: %+v", ticket)
		}
		if ticket.Routes[0].Checks[0].ID == "" || len(ticket.Routes[0].Checks[0].Results) == 0 {
			t.Fatal("no stable evidence IDs")
		}
		for _, secret := range []string{"auth", "EXAMPLE_GITHUB_TOKEN", strings.Repeat("x", 120)} {
			if strings.Contains(out, secret) {
				t.Fatalf("unnecessary raw data %q", secret)
			}
		}
	}
}

func TestJSONErrorsEmptyAndMissingTickets(t *testing.T) {
	for _, args := range [][]string{{"status", "--json", "--limit", "0"}, {"status", "--json", "--unknown"}, {"test", "--json"}, {"status", "--json", "APP-404"}} {
		code, out, _ := invoke(t, &fakeSource{}, args...)
		if code != 2 || !json.Valid([]byte(out)) {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	code, out, _ := invoke(t, &fakeSource{}, "health", "--json")
	var r machineReport
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Tickets == nil || r.Warnings == nil {
		t.Fatalf("empty report: %d %s", code, out)
	}
}

type failingRead struct{ fakeSource }

func (*failingRead) Records(context.Context, []workfile.Ticket) ([]workfile.Ticket, []string, error) {
	return nil, nil, errors.New("provider unavailable")
}
func TestJSONProviderFailureCannotProduceReadiness(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"status", "--json", "--dir", "../../example"}, &out, &errOut, "test", func(*workfile.Workspace) source { return &failingRead{} })
	if code != 2 || !json.Valid(out.Bytes()) || strings.Contains(out.String(), "\"ready\"") {
		t.Fatalf("%d %s", code, out.String())
	}
}

func TestEvidenceAndCoverageFitNarrowPlainTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tk := fixtureTicket("APP-42", "review", strings.Repeat("x", 120))
	tk.Records = map[string][]workfile.Record{"github": {{ID: "api#1", Facts: workfile.Facts{"checks": "success", "labels": []string{}, "approvals": []workfile.Actor{}}}}}
	for _, width := range []int{40, 60} {
		t.Setenv("COLUMNS", fmt.Sprint(width))
		_, out, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "status", "APP-42", "--evidence")
		for _, want := range []string{"COVERAGE", "EVIDENCE", "No approvals", "token", "90d"} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q: %s", want, out)
			}
		}
		if strings.Contains(out, "\x1b") {
			t.Fatal("escapes in plain output")
		}
		for _, line := range strings.Split(out, "\n") {
			if goRunWidth := runewidth.StringWidth(line); goRunWidth > width {
				t.Fatalf("width %d, got %d: %q", width, goRunWidth, line)
			}
		}
	}
}

func TestJSONFilteringAndUnknownEvidence(t *testing.T) {
	tk := fixtureTicket("APP-42", "review", strings.Repeat("x", 120))
	tk.Records = map[string][]workfile.Record{"github": {{ID: "api#1", Facts: workfile.Facts{"checks": "success", "labels": []string{}}, Unavailable: map[string]string{"approvals": "Review evidence unavailable"}}}}
	code, out, _ := invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "health", "--gate", "code_review", "--json")
	var r machineReport
	if code != 2 || json.Unmarshal([]byte(out), &r) != nil || r.Summary["cannot_check"] != 1 || len(r.Tickets) != 1 || r.Tickets[0].Status != "cannot_check" {
		t.Fatalf("%d %s", code, out)
	}
	if r.Tickets[0].Routes[0].Available || !slices.ContainsFunc(r.Tickets[0].Routes[0].Checks, func(c workfile.Check) bool { return c.Outcome == "unknown" }) {
		t.Fatal("unknown readiness")
	}
	_, out, _ = invoke(t, &fakeSource{tickets: []workfile.Ticket{tk}}, "status", "APP-42")
	if !strings.Contains(out, "EVIDENCE") || !strings.Contains(out, "Review evidence unavailable") {
		t.Fatalf("unknown evidence hidden: %s", out)
	}
}
