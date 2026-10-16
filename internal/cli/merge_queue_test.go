package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func TestQueuedPullRequestsKeepGateOutcomesAndFitTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WORKFILE_BACKGROUND", "off")
	for _, outcome := range []string{"pass", "fail", "unknown", "unassessed"} {
		for _, width := range []int{40, 60, 140} {
			t.Run(fmt.Sprintf("%s/%d", outcome, width), func(t *testing.T) {
				w, ticket := detailFixture(t)
				ticket.Records["github"] = ticket.Records["github"][:1]
				record := ticket.Records["github"][0]
				record.Facts["merge_queue"] = "queued"
				a := workfile.Assessment{Ticket: ticket}
				if outcome != "unassessed" {
					a.Current = []workfile.Check{{Results: []workfile.RuleResult{{Provider: "github", Record: record.ID, URL: record.URL, Outcome: outcome}}, Requirement: "Required label"}}
				}
				var out bytes.Buffer
				v := view{out: &out, width: width}
				v.block(muted, plain("PULL REQUESTS"), v.pullRequests(w, a))
				rendered := out.String()
				if strings.Contains(rendered, "→ merge") || strings.Contains(rendered, "Merged") {
					t.Fatal("queued PR presented as unqueued or merged")
				}
				if strings.Contains(rendered, "\x1b") {
					t.Fatal("escapes in plain output")
				}
				words := strings.Join(strings.Fields(strings.ReplaceAll(rendered, "▌", "")), " ")
				want, next := "Queued for merge", "Wait for merge"
				if outcome == "fail" {
					want, next = "Queued; gate failed", "Fix gates"
				}
				if outcome == "unknown" {
					want, next = "Queued; cannot check", "Restore evidence"
				}
				if !strings.Contains(words, want) || !strings.Contains(words, next) {
					t.Fatalf("missing queue or gate status: %s", rendered)
				}
				for _, line := range strings.Split(rendered, "\n") {
					if runewidth.StringWidth(line) > width {
						t.Fatalf("line exceeds %d: %q", width, line)
					}
				}
			})
		}
	}
}

func TestMergedPRIsNotPresentedAsQueued(t *testing.T) {
	w, ticket := detailFixture(t)
	ticket.Records["github"] = ticket.Records["github"][1:]
	ticket.Records["github"][0].Facts["merge_queue"] = "queued"
	var out bytes.Buffer
	v := view{out: &out, width: 100}
	v.block(muted, plain("PULL REQUESTS"), v.pullRequests(w, workfile.Assessment{Ticket: ticket}))
	if !strings.Contains(out.String(), "Merged") || strings.Contains(out.String(), "Queued") {
		t.Fatal("merged state must take precedence")
	}
}
