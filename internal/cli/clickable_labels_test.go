package cli

import (
	"bytes"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestStatusLinksTicketsTitlesAndPRs(t *testing.T) {
	w, ticket := detailFixture(t)
	ticket.Title = "Settings"
	ticket.State = "done"
	ticket.Facts["description"] = strings.Repeat("x", 120)
	ticket.Facts["labels"] = []workfile.Actor{}
	ticket.Records["github"][0].Facts["approvals"] = []workfile.Actor{}
	for _, width := range []int{40, 60, 110} {
		var out bytes.Buffer
		v := view{out: &out, width: width, links: true}
		v.report(w, []workfile.Assessment{w.Assess(ticket)}, options{command: "status"})
		text := out.String()
		if width == 110 && !strings.Contains(text, v.link(ticket.Title, ticket.URL)) {
			t.Fatalf("the ticket title is not clickable:\n%s", text)
		}
		for _, target := range []string{ticket.URL, "https://github.com/example-team/web/pull/18"} {
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
