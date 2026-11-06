package provider

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func plain(text string) []workfile.Span { return []workfile.Span{{Text: text}} }

func TestMarkdownBlocks(t *testing.T) {
	para := func(spans ...workfile.Span) workfile.Block {
		return workfile.Block{Kind: workfile.BlockParagraph, Spans: spans}
	}
	list := func(level int, marker, text string) workfile.Block {
		return workfile.Block{Kind: workfile.BlockList, Level: level, Marker: marker, Spans: plain(text)}
	}
	cases := []struct {
		name, in string
		want     []workfile.Block
	}{
		{"headings", "# One\n## Two\n###### Six\n####### Seven", []workfile.Block{
			{Kind: workfile.BlockHeading, Level: 1, Spans: plain("One")},
			{Kind: workfile.BlockHeading, Level: 2, Spans: plain("Two")},
			{Kind: workfile.BlockHeading, Level: 6, Spans: plain("Six")},
			para(workfile.Span{Text: "####### Seven"}),
		}},
		{"heading closing hashes", "## Steps ##\n# C#\n# C# #", []workfile.Block{
			{Kind: workfile.BlockHeading, Level: 2, Spans: plain("Steps")},
			{Kind: workfile.BlockHeading, Level: 1, Spans: plain("C#")},
			{Kind: workfile.BlockHeading, Level: 1, Spans: plain("C#")},
		}},
		{"paragraphs", "One\nstill one\n\nTwo\r\n", []workfile.Block{para(workfile.Span{Text: "One\nstill one"}), para(workfile.Span{Text: "Two"})}},
		{"bold", "a **b** and __c__", []workfile.Block{para(
			workfile.Span{Text: "a "}, workfile.Span{Text: "b", Bold: true}, workfile.Span{Text: " and "}, workfile.Span{Text: "c", Bold: true})}},
		{"italic", "a *b* and _c_ and snake_case_name", []workfile.Block{para(
			workfile.Span{Text: "a "}, workfile.Span{Text: "b", Italic: true}, workfile.Span{Text: " and "}, workfile.Span{Text: "c", Italic: true}, workfile.Span{Text: " and snake_case_name"})}},
		{"bold italic", "***x***", []workfile.Block{para(workfile.Span{Text: "x", Bold: true, Italic: true})}},
		{"code span", "run `wf status` now, ``a ` b``", []workfile.Block{para(
			workfile.Span{Text: "run "}, workfile.Span{Text: "wf status", Code: true}, workfile.Span{Text: " now, "}, workfile.Span{Text: "a ` b", Code: true})}},
		{"link", "see [the **docs**](https://example.com/a_(b)) now", []workfile.Block{para(
			workfile.Span{Text: "see "},
			workfile.Span{Text: "the ", Link: "https://example.com/a_(b)"}, workfile.Span{Text: "docs", Bold: true, Link: "https://example.com/a_(b)"},
			workfile.Span{Text: " now"})}},
		{"escapes and stray marks", `a \*b\* 2 * 3 [not a link]`, []workfile.Block{para(workfile.Span{Text: "a *b* 2 * 3 [not a link]"})}},
		{"fenced code", "```go\nx := 1\n# not a heading\n```\nafter\n~~~~\n```\n~~~~", []workfile.Block{
			{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: "x := 1\n# not a heading", Code: true}}},
			para(workfile.Span{Text: "after"}),
			{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: "```", Code: true}}},
		}},
		{"unclosed fence", "```\nabc", []workfile.Block{{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: "abc", Code: true}}}}},
		{"bullets and numbers", "- a\n* b\n  - nested\n    - deeper\n- c\n\n1. one\n2) two", []workfile.Block{
			list(0, "•", "a"), list(0, "•", "b"), list(1, "•", "nested"), list(2, "•", "deeper"), list(0, "•", "c"),
			list(0, "1.", "one"), list(0, "2.", "two"),
		}},
		{"task list and continuation", "- [ ] todo\n- [x] done\n- wraps\n  here", []workfile.Block{
			list(0, "☐", "todo"), list(0, "✓", "done"), list(0, "•", "wraps\nhere"),
		}},
		{"quote", "> a\n> # b\n> - c\n\nafter", []workfile.Block{
			{Kind: workfile.BlockParagraph, Quoted: true, Spans: plain("a")},
			{Kind: workfile.BlockHeading, Quoted: true, Level: 1, Spans: plain("b")},
			{Kind: workfile.BlockList, Quoted: true, Marker: "•", Spans: plain("c")},
			para(workfile.Span{Text: "after"}),
		}},
		{"rule", "a\n\n---\n***\n* * *\n\nb", []workfile.Block{
			para(workfile.Span{Text: "a"}), {Kind: workfile.BlockRule}, {Kind: workfile.BlockRule}, {Kind: workfile.BlockRule}, para(workfile.Span{Text: "b"}),
		}},
		{"table", "| Name | Count |\n| --- | :-: |\n| **a** | 1 |\n| b \\| c | 2 |\n\nafter", []workfile.Block{
			{Kind: workfile.BlockTable, Header: true, Rows: [][]string{{"Name", "Count"}, {"a", "1"}, {"b | c", "2"}}},
			para(workfile.Span{Text: "after"}),
		}},
		{"a pipe alone is text", "a | b\nc", []workfile.Block{para(workfile.Span{Text: "a | b\nc"})}},
		{"images", "![Login mock](https://github.com/user-attachments/assets/a.png)\n\n![](https://github.com/user-attachments/assets/b.png) ![two](https://github.com/user-attachments/assets/c.png)", []workfile.Block{
			{Kind: workfile.BlockImage, Name: "Login mock", Attachment: "https://github.com/user-attachments/assets/a.png"},
			{Kind: workfile.BlockImage, Name: "b.png", Attachment: "https://github.com/user-attachments/assets/b.png"},
			{Kind: workfile.BlockImage, Name: "two", Attachment: "https://github.com/user-attachments/assets/c.png"},
		}},
		{"image inside text keeps its words", "See ![shot](https://example.com/s.png) here", []workfile.Block{para(
			workfile.Span{Text: "See "}, workfile.Span{Text: "shot", Link: "https://example.com/s.png"}, workfile.Span{Text: " here"})}},
		{"unknown syntax stays text", "<details>\n<summary>x</summary>\n</details>\n\n<!-- note -->\n\n    indented", []workfile.Block{
			para(workfile.Span{Text: "<details>\n<summary>x</summary>\n</details>"}),
			para(workfile.Span{Text: "<!-- note -->"}),
			para(workfile.Span{Text: "indented"}),
		}},
		{"blank line inside a fence", "```\na\n\nb\n```", []workfile.Block{{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: "a\n\nb", Code: true}}}}},
		{"unclosed fence with newline", "```\na\n", []workfile.Block{{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: "a\n", Code: true}}}}},
		{"indented rule is text", "    - - -", []workfile.Block{para(workfile.Span{Text: "- - -"})}},
		{"lone star then italic", "2 * 3 = 6 and *this* matters", []workfile.Block{para(workfile.Span{Text: "2 * 3 = 6 and "}, workfile.Span{Text: "this", Italic: true}, workfile.Span{Text: " matters"})}},
		{"wildcard star then italic", "Note: * is a wildcard. *Do not* use it.", []workfile.Block{para(workfile.Span{Text: "Note: * is a wildcard. "}, workfile.Span{Text: "Do not", Italic: true}, workfile.Span{Text: " use it."})}},
		{"spaced bold marks then bold", "a ** b and **bold** here", []workfile.Block{para(workfile.Span{Text: "a ** b and "}, workfile.Span{Text: "bold", Bold: true}, workfile.Span{Text: " here"})}},
		{"spaced underscore then italic", "snake _ case and _italic_ here", []workfile.Block{para(workfile.Span{Text: "snake _ case and "}, workfile.Span{Text: "italic", Italic: true}, workfile.Span{Text: " here"})}},
		{"star then bold and italic", "Use a * b, then **bold** and *italic*", []workfile.Block{para(workfile.Span{Text: "Use a * b, then "}, workfile.Span{Text: "bold", Bold: true}, workfile.Span{Text: " and "}, workfile.Span{Text: "italic", Italic: true})}},
		{"stray backtick after code", "*a `x` ` y", []workfile.Block{para(workfile.Span{Text: "*a "}, workfile.Span{Text: "x", Code: true}, workfile.Span{Text: " ` y"})}},
		{"unclosed star then code", "unclosed *star then `code` and `more` and ` tail", []workfile.Block{para(workfile.Span{Text: "unclosed *star then "}, workfile.Span{Text: "code", Code: true}, workfile.Span{Text: " and "}, workfile.Span{Text: "more", Code: true}, workfile.Span{Text: " and ` tail"})}},
		{"empty", " \n\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownBlocks(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("markdownBlocks(%q)\n got %+v\nwant %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMarkdownText(t *testing.T) {
	body := "# Goal\nUse **bold** and `code` and [a link](https://example.com).\n\n- one\n  - two\n\n```\n# not a heading\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n![shot](https://example.com/s.png)"
	text, headings := markdownText(body)
	want := "Goal\nUse bold and code and a link.\n• one\n  • two\n# not a heading\nA | B\n1 | 2\nshot"
	if text != want {
		t.Errorf("text %q\nwant %q", text, want)
	}
	if !reflect.DeepEqual(headings, []string{"Goal"}) {
		t.Errorf("headings %q", headings)
	}
	if _, headings := markdownText(""); headings == nil || len(headings) != 0 {
		t.Errorf("an empty body has headings %#v, want an empty list", headings)
	}
}

const githubDetailBody = `{"data":{"repository":{"issue":{"body":"# Goal\nSee ![shot](https://github.com/user-attachments/assets/a.png)"}}}}`

func githubDetailHandler(comments func() (int, string)) func(*http.Request) (int, string) {
	return func(r *http.Request) (int, string) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "comments(last: 100)") {
			return comments()
		}
		return 200, githubDetailBody
	}
}

func TestGitHubDetail(t *testing.T) {
	c, rec := conformClient(t, "", issuesProvider(), githubDetailHandler(func() (int, string) {
		return 200, `{"data":{"repository":{"issue":{"comments":{"totalCount":250,"nodes":[
			{"author":{"login":"example-bob"},"createdAt":"2026-10-01T10:00:00Z","body":"First **one**"},
			{"author":null,"createdAt":"nonsense","body":"Second"}]}}}}}`
	}))
	d, err := trackerOf(t, c).Detail(context.Background(), "app#1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Key != "app#1" || d.CommentTotal != 250 || len(d.Comments) != 2 {
		t.Fatalf("%+v", d)
	}
	if len(d.Body) != 2 || d.Body[0].Kind != workfile.BlockHeading || d.Body[1].Spans[1].Link != "https://github.com/user-attachments/assets/a.png" {
		t.Errorf("body %+v", d.Body)
	}
	first := d.Comments[0]
	if first.Author != "example-bob" || !first.Created.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) || len(first.Body) != 1 || !first.Body[0].Spans[1].Bold {
		t.Errorf("first %+v", first)
	}
	if second := d.Comments[1]; second.Author != "" || !second.Created.IsZero() {
		t.Errorf("second %+v", second)
	}
	if rec.count() != 2 {
		t.Errorf("requests %q", rec.requests)
	}
	last100 := false
	for _, r := range rec.requests {
		last100 = last100 || strings.Contains(r, "comments(last: 100)")
	}
	if !last100 {
		t.Error("the newest 100 comments were not asked for")
	}
}

func TestGitHubDetailKeepsTheBodyWhenCommentsFail(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), githubDetailHandler(func() (int, string) { return 500, `{}` }))
	d, err := trackerOf(t, c).Detail(context.Background(), "app#1")
	if err != nil || len(d.Body) == 0 || len(d.Comments) != 0 || d.CommentTotal != 0 {
		t.Fatalf("%+v, %v", d, err)
	}
}

func TestGitHubDetailOfAMissingIssue(t *testing.T) {
	c, _ := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) {
		return 200, `{"data":{"repository":{"issue":null}},"errors":[{"type":"NOT_FOUND","path":["repository","issue"],"message":"x"}]}`
	})
	if _, err := trackerOf(t, c).Detail(context.Background(), "app#404"); err == nil {
		t.Error("a missing issue was not an error")
	}
}

func TestImageOnlyGoesToGitHub(t *testing.T) {
	allowed := []string{
		"https://github.com/user-attachments/assets/a.png",
		"https://user-images.githubusercontent.com/1/a.png",
		"https://private-user-images.githubusercontent.com/1/a.png?jwt=abc",
	}
	for _, address := range allowed {
		var host, auth string
		c, _ := conformClient(t, "", issuesProvider(), func(r *http.Request) (int, string) {
			host, auth = r.URL.Host, r.Header.Get("Authorization")
			return 200, "PNG"
		})
		data, err := trackerOf(t, c).Image(context.Background(), address)
		if err != nil || string(data) != "PNG" || !strings.HasPrefix(address, "https://"+host+"/") || auth != "Bearer test-github-token" {
			t.Errorf("%s: %q %v host %q auth %q", address, data, err, host, auth)
		}
	}
	refused := []string{
		"", "a.png", "../secret", "a b",
		"https://github.com.example.com/a.png",
		"https://example.github.com/a.png",
		"https://notgithub.com/a.png",
		"https://github.com./a.png",
		"https://githubusercontent.com/a.png",
		"https://evilusercontent.com/user-images.githubusercontent.com",
		"http://github.com/a.png",
		"https://example.com/a.png",
		"https://github.com@example.com/a.png",
		"https://user:pass@github.com/a.png",
		"https://github.com:8443/a.png",
		"https://github.com:443/a.png",
		"https://github.com:/a.png",
		"https://github.com\\@example.com/a.png",
		"//github.com/a.png",
		"https:///a.png",
		"https://github.com/a\nb.png",
		"https://[github.com/a.png",
		"ftp://github.com/a.png",
		"file:///etc/passwd",
		"https://g\u0130thub.com/a.png",
		"https://G\u0130THUB.COM/a.png",
		"https://user-\u0130mages.githubusercontent.com/a.png",
		"https://github.com/\u00e9.png",
		"https://github.com/a\x7f.png",
		"https://GITHUB.com/a.png",
	}
	for _, address := range refused {
		c, rec := conformClient(t, "", issuesProvider(), func(*http.Request) (int, string) { return 200, "PNG" })
		if _, err := trackerOf(t, c).Image(context.Background(), address); err == nil {
			t.Errorf("%q was not refused", address)
		}
		if rec.count() != 0 {
			t.Errorf("%q caused a request: %q", address, rec.requests)
		}
	}
}

func TestMarkdownBlocksSurvivesAwkwardInput(t *testing.T) {
	inputs := []string{
		"- - - ", "- - -\f", "- - -\v", "* * * ", "    - - - ", "a\n- - - ", "> - - - ", "- - -\u00a0", "- - -\u2003", "- - -\u0085", "a_" + "b_",
		"    - - -", "      * * *", ">     - - -", "```\n\n", "```", "~~~\n\n\n~~~", "- ", "-", "1.", "> ", ">", "|", "|\n|", "| a |\n| - |",
		"![", "![](", "[](", "[a](", "[a]()", "***", "**", "* ", "_", "`", "``", "\\", "# ", "#", "\x00\xff", "- a\n\n\n- b", "- a\n    - - -\n- c",
		strings.Repeat("- ", 1000), strings.Repeat("> ", 1000), strings.Repeat("[", 5000), strings.Repeat("*", 5001),
	}
	for _, in := range inputs {
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("markdownBlocks(%.40q) panicked: %v", in, r)
				}
			}()
			markdownText(in)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("markdownBlocks(%.40q) did not return", in)
		}
	}
}

func TestMarkdownBlocksHostileInputIsBounded(t *testing.T) {
	var unique strings.Builder
	for n := 1; n <= 250; n++ {
		unique.WriteString(strings.Repeat("`", n) + " ")
	}
	cases := map[string]string{
		"unclosed emphasis and code": strings.Repeat("*a ", 10922) + unique.String(),
		"deep quote":                 strings.Repeat(">", 262144),
		"deep spaced quote":          strings.Repeat("> ", 100000),
		"unmatched brackets":         strings.Repeat("[", 65000),
		"nested emphasis":            strings.Repeat("*a ", 20000) + strings.Repeat("*", 20000),
		"intraword underscores":      strings.Repeat("a_", 32767),
		"underscores in a cell":      "| a |\n| - |\n| " + strings.Repeat("a_", 20000) + " |",
		"word underscores":           strings.Repeat("x_a ", 16383) + "b_",
		"long link":                  "[a](" + strings.Repeat("b", 7000) + ") ![c](" + strings.Repeat("d", 7000) + ")",
		"many list lines":            "- a\n" + strings.Repeat("b\n", 30000),
	}
	for name, in := range cases {
		start := time.Now()
		markdownText(in)
		// The unbounded version took tens of seconds, or crashed.
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s took %v", name, took)
		} else {
			t.Logf("%s: %v", name, took)
		}
	}
}

func TestMarkdownBlocksKeepsLongAddresses(t *testing.T) {
	address := "https://github.com/user-attachments/assets/" + strings.Repeat("a", 5000)
	blocks := markdownBlocks("![shot](" + address + ")")
	if len(blocks) != 1 || blocks[0].Kind != workfile.BlockImage || blocks[0].Attachment != address {
		t.Errorf("%+v", blocks)
	}
}

func TestMarkdownBlocksRandomInput(t *testing.T) {
	tokens := []string{"*", "**", "_", "__", "`", "``", "```", "~~~", "[", "]", "(", ")", "!", "#", "##", ">", "-", "+", "1.", "|", "---", "\\", "a", "b", "x_y", " ", "  ", "    ", "\t", "\n", "\n\n", "\u00a0", "\f", "\v", "\u2003", "\u3000", "\u0085", "\r", "[ ]", "[x]", "<", "&", "\u00e9"}
	rng := rand.New(rand.NewSource(1))
	start := time.Now()
	for n := 0; n < 3000; n++ {
		var in strings.Builder
		for k := rng.Intn(40); k >= 0; k-- {
			in.WriteString(tokens[rng.Intn(len(tokens))])
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("markdownText(%q) panicked: %v", in.String(), r)
				}
			}()
			markdownText(in.String())
		}()
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v", took)
	}
}
