package provider

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

type adfNode struct {
	Type, Text string
	Attrs      struct{ Text string }
	Content    []adfNode
	Marks      []struct{ Type string }
}

func description(raw json.RawMessage) (string, []string) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	var root adfNode
	if json.Unmarshal(raw, &root) != nil {
		return "", nil
	}
	var headings []string
	var visit func(adfNode) string
	visit = func(n adfNode) string {
		text := ""
		switch n.Type {
		case "text":
			text = n.Text
		case "hardBreak":
			text = "\n"
		case "mention", "emoji":
			text = n.Attrs.Text
		default:
			for _, child := range n.Content {
				text += visit(child)
			}
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" && (n.Type == "heading" || n.Type == "paragraph" && allBold(n) && utf8.RuneCountInString(trimmed) <= 80) {
			headings = append(headings, trimmed)
		}
		if slices.Contains([]string{"paragraph", "heading", "blockquote", "codeBlock", "listItem", "tableRow", "panel", "rule"}, n.Type) {
			text += "\n"
		}
		return text
	}
	text := strings.TrimSpace(visit(root))
	return text, headings
}

func allBold(n adfNode) bool {
	found := false
	for _, child := range n.Content {
		if child.Type == "hardBreak" {
			continue
		}
		if child.Type != "text" {
			return false
		}
		bold := false
		for _, mark := range child.Marks {
			bold = bold || mark.Type == "strong"
		}
		if !bold {
			return false
		}
		found = true
	}
	return found
}
