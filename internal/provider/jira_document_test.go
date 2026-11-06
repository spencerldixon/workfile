package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func doc(content string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","version":1,"content":[` + content + `]}`)
}

func TestDocumentHeadingsAndMarks(t *testing.T) {
	blocks := document(doc(`
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Why"}]},
		{"type":"paragraph","content":[
			{"type":"text","text":"Read "},
			{"type":"text","text":"this","marks":[{"type":"strong"},{"type":"em"}]},
			{"type":"text","text":" guide","marks":[{"type":"link","attrs":{"href":"https://example.com/guide"}}]},
			{"type":"text","text":" run","marks":[{"type":"code"}]},
			{"type":"hardBreak"},
			{"type":"mention","attrs":{"id":"x","text":"@Alice"}}
		]}`))
	if len(blocks) != 2 || blocks[0].Kind != workfile.BlockHeading || blocks[0].Level != 2 {
		t.Fatalf("%+v", blocks)
	}
	want := []workfile.Span{{Text: "Read "}, {Text: "this", Bold: true, Italic: true}, {Text: " guide", Link: "https://example.com/guide"}, {Text: " run", Code: true}, {Text: "\n"}, {Text: "@Alice"}}
	if !reflect.DeepEqual(blocks[1].Spans, want) {
		t.Fatalf("%+v", blocks[1].Spans)
	}
}

func TestDocumentListsAreFlatWithDepth(t *testing.T) {
	blocks := document(doc(`{"type":"orderedList","content":[
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]},
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]}`))
	got := []string{}
	for _, b := range blocks {
		got = append(got, strings.Repeat(">", b.Level)+b.Marker+spanText(b.Spans))
	}
	if strings.Join(got, "|") != "1.one|>•nested|2.two" {
		t.Fatal(got)
	}
}

func TestDocumentCodeQuoteTableRule(t *testing.T) {
	blocks := document(doc(`
		{"type":"codeBlock","content":[{"type":"text","text":"go test\n./..."}]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"said"}]}]},
		{"type":"panel","attrs":{"panelType":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"note"}]}]},
		{"type":"rule"},
		{"type":"table","content":[
			{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Name"}]}]}]},
			{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Bob"}]}]}]}]}`))
	if len(blocks) != 5 {
		t.Fatalf("%+v", blocks)
	}
	if blocks[0].Kind != workfile.BlockCode || spanText(blocks[0].Spans) != "go test\n./..." {
		t.Errorf("%+v", blocks[0])
	}
	if !blocks[1].Quoted || !blocks[2].Quoted {
		t.Errorf("not quoted: %+v %+v", blocks[1], blocks[2])
	}
	if blocks[3].Kind != workfile.BlockRule {
		t.Errorf("%+v", blocks[3])
	}
	if b := blocks[4]; b.Kind != workfile.BlockTable || !b.Header || !reflect.DeepEqual(b.Rows, [][]string{{"Name"}, {"Bob"}}) {
		t.Errorf("%+v", b)
	}
}

func TestDocumentImages(t *testing.T) {
	blocks := document(doc(`
		{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"media-1","type":"file","alt":"login-mock.png"}}]},
		{"type":"mediaGroup","content":[{"type":"media","attrs":{"id":"media-2","type":"file"}}]}`))
	if len(blocks) != 2 || blocks[0].Kind != workfile.BlockImage || blocks[0].Name != "login-mock.png" || blocks[0].Attachment != "media-1" || blocks[1].Attachment != "media-2" {
		t.Fatalf("%+v", blocks)
	}
}

func TestDocumentNeverLosesTextToAnUnknownNode(t *testing.T) {
	blocks := document(doc(`
		{"type":"somethingNew","attrs":{"id":42,"level":"high"},"content":[{"type":"paragraph","content":[{"type":"text","text":"kept"}]}]},
		{"type":"paragraph","attrs":{"level":"not a number"},"content":[{"type":"futureInline","text":"also kept"}]}`))
	if len(blocks) != 2 || spanText(blocks[0].Spans) != "kept" || spanText(blocks[1].Spans) != "also kept" {
		t.Fatalf("%+v", blocks)
	}
}

func TestDocumentPlainAndBrokenInput(t *testing.T) {
	if blocks := document(json.RawMessage(`"first\n\nsecond"`)); len(blocks) != 2 || spanText(blocks[1].Spans) != "second" {
		t.Fatalf("%+v", blocks)
	}
	for _, raw := range []string{`null`, ``, `{broken`} {
		if blocks := document(json.RawMessage(raw)); len(blocks) != 0 {
			t.Errorf("%q: %+v", raw, blocks)
		}
	}
}

func spanText(spans []workfile.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
