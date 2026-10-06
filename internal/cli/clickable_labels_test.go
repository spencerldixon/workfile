package cli

import (
	"bytes"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestMeLinksTicketTitleRepositoryAndPRSeparately(t *testing.T) {
	w, ticket := detailFixture(t)
	ticket.Title = "Settings"
	ticket.Assignee = "example-bob"
	// Exercise a named tracker and a second GitHub instance.
	w.Policy.Tracker = "jira_other"
	// Keep the compiled policy aligned with the renamed tracker.
	for gate, rules := range w.Policy.Gates {
		for i := range rules {
			if rules[i].Condition.Provider == "jira" {
				rules[i].Condition.Provider = "jira_other"
			}
			if rules[i].Guard != nil && rules[i].Guard.Provider == "jira" {
				rules[i].Guard.Provider = "jira_other"
			}
		}
		w.Policy.Gates[gate] = rules
	}
	w.Providers["jira_other"] = w.Providers["jira"]
	w.Providers["github_other"] = w.Providers["github"]
	ticket.Records["github_other"] = ticket.Records["github"]
	for _, width := range []int{40, 60, 110} {
		var out bytes.Buffer
		v := view{out: &out, width: width, links: true}
		v.report(w, []workfile.Assessment{w.Assess(ticket)}, options{command: "status", me: true}, map[string]string{"jira_other": ticket.Assignee})
		// The title can be truncated on narrow screens, but its cell must
		// still carry the Jira URL independently of the key.
		head := v.ticketHead("", "✗", red, w.Assess(ticket), cell{})
		if head[len(head)-1].text != "Settings" || head[len(head)-1].url != ticket.URL {
			t.Fatal("ticket title is not linked")
		}
		text := out.String()
		if width == 110 && !strings.Contains(text, v.link(ticket.Title, ticket.URL)) {
			t.Fatal("status --me title is not clickable")
		}
		for _, target := range []string{ticket.URL, "https://github.com/example-team/web", "https://github.com/example-team/web/pull/18"} {
			if !strings.Contains(text, "\x1b]8;;"+target+"\x1b\\") {
				t.Fatalf("missing link %q at %d columns:\n%s", target, width, text)
			}
		}
	}
}

func TestDetailsLinksVisibleLabels(t *testing.T) {
	w, ticket := detailFixture(t)
	for _, width := range []int{40, 60, 110} {
		var out bytes.Buffer
		v := view{out: &out, width: width, links: true}
		v.details(w, w.Assess(ticket))
		text := out.String()
		for _, record := range ticket.Records["github"] {
			for _, target := range []string{record.URL, repositoryURL(record.URL)} {
				if !strings.Contains(text, "\x1b]8;;"+target+"\x1b\\") {
					t.Fatalf("missing link %q at %d columns", target, width)
				}
			}
		}
		for _, r := range textRows(ticket.Title, "", "1", ticket.URL, v.innerWidth()) {
			if !strings.Contains(text, v.render(r)) {
				t.Fatal("ticket title is not clickable in details")
			}
		}
	}
}
