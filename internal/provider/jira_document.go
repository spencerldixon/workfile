package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"workfile/internal/workfile"
)

// docNode is one node of Jira's rich text. Attrs stays raw because each node
// type gives its attributes different types: one odd value must cost that
// attribute, not the whole ticket. description() keeps its own simpler node,
// because policy checks depend on its output.
type docNode struct {
	Type, Text string
	Attrs      json.RawMessage
	Content    []docNode
	Marks      []struct {
		Type  string
		Attrs json.RawMessage
	}
}

// attr reads one attribute, leaving the zero value if it is missing or odd.
func attr[T any](raw json.RawMessage, name string) T {
	var value T
	var attrs map[string]json.RawMessage
	if json.Unmarshal(raw, &attrs) == nil {
		_ = json.Unmarshal(attrs[name], &value)
	}
	return value
}

// document turns a Jira description or comment into blocks, for reading.
func document(raw json.RawMessage) []workfile.Block {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		var blocks []workfile.Block
		for _, text := range strings.Split(plain, "\n\n") {
			if text = strings.TrimSpace(text); text != "" {
				blocks = append(blocks, workfile.Block{Kind: workfile.BlockParagraph, Spans: []workfile.Span{{Text: text}}})
			}
		}
		return blocks
	}
	var root docNode
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	return docBlocks(root.Content, 0, false)
}

func docBlocks(nodes []docNode, level int, quoted bool) []workfile.Block {
	var blocks []workfile.Block
	add := func(b workfile.Block) {
		b.Quoted = quoted
		blocks = append(blocks, b)
	}
	for _, n := range nodes {
		switch n.Type {
		case "paragraph":
			add(workfile.Block{Kind: workfile.BlockParagraph, Spans: docSpans(n.Content)})
		case "heading":
			add(workfile.Block{Kind: workfile.BlockHeading, Level: attr[int](n.Attrs, "level"), Spans: docSpans(n.Content)})
		case "codeBlock":
			add(workfile.Block{Kind: workfile.BlockCode, Spans: []workfile.Span{{Text: docText(n), Code: true}}})
		case "rule":
			add(workfile.Block{Kind: workfile.BlockRule})
		case "blockquote", "panel":
			blocks = append(blocks, docBlocks(n.Content, level, true)...)
		case "bulletList", "orderedList", "taskList", "decisionList":
			blocks = append(blocks, docList(n, level, quoted)...)
		case "table":
			add(docTable(n))
		case "mediaSingle", "mediaGroup":
			for _, media := range n.Content {
				if media.Type == "media" {
					add(workfile.Block{Kind: workfile.BlockImage, Name: attr[string](media.Attrs, "alt"), Attachment: attr[string](media.Attrs, "id")})
				}
			}
		default:
			// An unknown node keeps its text, whatever shape it arrives in.
			if hasBlocks(n.Content) {
				blocks = append(blocks, docBlocks(n.Content, level, quoted)...)
			} else if spans := docSpans([]docNode{n}); len(spans) > 0 {
				add(workfile.Block{Kind: workfile.BlockParagraph, Spans: spans})
			}
		}
	}
	return blocks
}

func hasBlocks(nodes []docNode) bool {
	for _, n := range nodes {
		switch n.Type {
		case "paragraph", "heading", "codeBlock", "bulletList", "orderedList", "table", "blockquote", "panel", "mediaSingle":
			return true
		}
	}
	return false
}

func docList(n docNode, level int, quoted bool) []workfile.Block {
	var blocks []workfile.Block
	first := max(1, attr[int](n.Attrs, "order"))
	for i, item := range n.Content {
		marker := "•"
		switch {
		case n.Type == "orderedList":
			marker = strconv.Itoa(first+i) + "."
		case item.Type == "taskItem" && attr[string](item.Attrs, "state") == "DONE":
			marker = "✓"
		case item.Type == "taskItem":
			marker = "☐"
		}
		children := item.Content
		// Task items hold inline text directly; list items hold paragraphs.
		if len(children) > 0 && !hasBlocks(children) {
			children = []docNode{{Type: "paragraph", Content: children}}
		}
		for j, b := range docBlocks(children, level+1, quoted) {
			if j == 0 && b.Kind == workfile.BlockParagraph {
				b.Kind, b.Level, b.Marker = workfile.BlockList, level, marker
			}
			blocks = append(blocks, b)
		}
	}
	return blocks
}

func docTable(n docNode) workfile.Block {
	b := workfile.Block{Kind: workfile.BlockTable}
	for i, row := range n.Content {
		var cells []string
		for _, cell := range row.Content {
			b.Header = b.Header || i == 0 && cell.Type == "tableHeader"
			cells = append(cells, strings.TrimSpace(docText(cell)))
		}
		b.Rows = append(b.Rows, cells)
	}
	return b
}

// docText is a node's text with no styling.
func docText(n docNode) string {
	var text strings.Builder
	for _, span := range docSpans(n.Content) {
		text.WriteString(span.Text)
	}
	return text.String()
}

func docSpans(nodes []docNode) []workfile.Span {
	var spans []workfile.Span
	for _, n := range nodes {
		switch n.Type {
		case "text":
			span := workfile.Span{Text: n.Text}
			for _, mark := range n.Marks {
				switch mark.Type {
				case "strong":
					span.Bold = true
				case "em":
					span.Italic = true
				case "code":
					span.Code = true
				case "link":
					span.Link = attr[string](mark.Attrs, "href")
				}
			}
			spans = append(spans, span)
		case "hardBreak":
			spans = append(spans, workfile.Span{Text: "\n"})
		case "mention", "emoji", "status", "date":
			if text := attr[string](n.Attrs, "text"); text != "" {
				spans = append(spans, workfile.Span{Text: text})
			}
		case "inlineCard":
			if address := attr[string](n.Attrs, "url"); address != "" {
				spans = append(spans, workfile.Span{Text: address, Link: address})
			}
		default:
			if n.Text != "" {
				spans = append(spans, workfile.Span{Text: n.Text})
			}
			spans = append(spans, docSpans(n.Content)...)
		}
	}
	return spans
}
