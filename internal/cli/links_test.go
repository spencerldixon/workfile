package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

func TestLinksAcrossViewsAndInstances(t *testing.T) {
	w, ticket := detailFixture(t)
	ticket.Assignee = "example-bob-account-id"
	// The record's own URL must win, even for another provider instance.
	ticket.Records["github_other"] = ticket.Records["github"]
	w.Providers["github_other"] = w.Providers["github"]
	all := []workfile.Assessment{w.Assess(ticket)}
	done := ticket
	done.State = "done"
	for _, o := range []options{
		{command: "status"},
		{command: "status", keys: []string{ticket.Key}},
		{command: "status", me: true},
		{command: "health", stage: ticket.State},
		{command: "health", gate: "code_review"},
	} {
		for _, width := range []int{40, 60, 110} {
			var out bytes.Buffer
			v := view{out: &out, width: width, links: true}
			shown := all
			if o.gate != "" {
				shown = []workfile.Assessment{w.Assess(done)}
			}
			v.report(w, shown, o, map[string]string{"jira": ticket.Assignee, "github_other": "example-bob"})
			text := out.String()
			if !strings.Contains(text, "\x1b]8;;"+ticket.URL+"\x1b\\") {
				t.Fatalf("missing ticket hyperlink for %+v at %d:\n%s", o, width, text)
			}
			if strings.Contains(text, "example-team/web") && !strings.Contains(text, "\x1b]8;;"+ticket.Records["github_other"][0].URL+"\x1b\\") {
				t.Fatalf("missing PR hyperlink for %+v at %d", o, width)
			}
			if strings.Contains(text, "\x1b[") {
				t.Fatal("hyperlinks should not enable colours")
			}
		}
	}
}

func TestPlainBlocksShowURLsOnceAndPreserveNarrowLayout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 110} {
		var out bytes.Buffer
		v := newView(&out)
		v.width = width
		url := "https://example.com/APP-42"
		prURL := "https://example.com/pr/1"
		v.block(green, cell{"APP-42", "1", url}, []row{
			{{"APP-42", "1", url}},
			{{"api #1", "1", prURL}},
			{{"api #1", "1", prURL}},
		})
		text := out.String()
		if strings.Contains(text, "\x1b") {
			t.Fatal("plain output contains terminal escapes")
		}
		for _, target := range []string{url, prURL} {
			if strings.Count(text, target) != 1 {
				t.Fatalf("expected one URL %q:\n%s", target, text)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if runewidth.StringWidth(line) > width {
				t.Fatalf("line exceeds %d columns: %q", width, line)
			}
		}
	}
}
