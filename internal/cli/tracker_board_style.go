package cli

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func boardStateTitle(state string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(clean(state)))
	for i, word := range words {
		letters := []rune(word)
		letters[0] = unicode.ToUpper(letters[0])
		words[i] = string(letters)
	}
	return strings.Join(words, " ")
}

func (m *trackerModel) boardPaint(state workfile.AttentionState, text string) string {
	if !m.t.color || m.shownView().custom.Attention == "icons" {
		return text
	}
	return attentionStyle(state, lipgloss.NewStyle().Faint(true)).Render(text)
}

// boardPRTags fits complete tags on one row and reserves room for the overflow count.
func (m *trackerModel) boardPRTags(records []workfile.Record, actions []workfile.WorkAction, width int) string {
	var tags []string
	remaining := width
	attention := m.shownView().custom.Attention
	for i, record := range records {
		room := remaining
		if hidden := len(records) - i - 1; hidden > 0 {
			room -= len(fmt.Sprintf(" +%d", hidden))
		}
		action, _ := primaryPRAction(actions, record.ID, record.URL)
		icon, label, colour := pullRequestStatus(record, action)
		if !m.t.color {
			icon = label
		}
		prefix := icon + " "
		if attention == "off" {
			prefix = ""
		}
		id := shortRecordID(record.ID, records)
		idRoom := room - ansi.StringWidth(prefix) - 2
		if idRoom < 1 {
			break
		}
		if ansi.StringWidth(id) > idRoom {
			if number := workfile.PRLabel(id); number != id {
				duplicates := 0
				for _, other := range records {
					if workfile.PRLabel(other.ID) == number {
						duplicates++
					}
				}
				if duplicates == 1 {
					id = number
				}
			}
			if ansi.StringWidth(id) > idRoom {
				break
			}
		}
		body := prefix + m.v.link(id, record.URL)
		tag := "[" + body + "]"
		if m.t.color {
			// Rounded Powerline caps use the same colour as the tag's fill.
			fill, ink := lipgloss.ANSIColor(8), lipgloss.ANSIColor(15)
			if colour != 0 && attention != "off" && attention != "icons" {
				fill, ink = lipgloss.ANSIColor(colour), lipgloss.ANSIColor(0)
			}
			tag = m.t.roundedTag(body, fill, ink)
		}
		tags = append(tags, tag)
		remaining -= ansi.StringWidth(tag) + 1
	}
	if hidden := len(records) - len(tags); hidden > 0 {
		tags = append(tags, m.t.dim(fmt.Sprintf("+%d", hidden)))
	}
	return strings.Join(tags, " ")
}

// boardTile is the glanceable screen card, not the full report. Never wrap long
// fixes or URLs into tall cards: enter opens all evidence in the ticket preview.
// Plain reports keep using boardCard, which writes every action and URL.
func (m *trackerModel) boardTile(item ticketItem, selected bool, width int) []string {
	inner := max(1, width-4)
	result := m.nextActions(item.Key)
	showAttention := m.shownView().custom.Attention != "off"
	// No icon on the key: the action row already carries the ticket's state.
	head := m.t.bold(m.v.link(clean(item.Key), clean(item.URL)))
	panel := m.boardTheme
	if selected {
		panel = m.boardSelectedTheme
	}
	lines := []string{"", "  " + ansi.Truncate(head, inner, "…"), "  " + ansi.Truncate(clean(item.Title), inner, "…")}
	add := func(text string) { lines = append(lines, "  "+ansi.Truncate(text, inner, "…")) }
	footer, icon := result.Summary, attentionIcon(result.State, m.t.color)
	var tags string
	var overflow []string
	r := m.ticketReads.byKey[item.Key]
	if r == nil || !r.policyDone() {
		footer = "PRs not checked"
		if r != nil && r.policyStarted {
			footer, icon = "Checking PRs…", attentionIcon(workfile.Waiting, m.t.color)
		}
	} else if r.policyErr != nil || r.policy == nil {
		footer = "Cannot check PRs"
	} else {
		var records []workfile.Record
		for _, provider := range workfile.SortedKeys(r.policy.Ticket.Records) {
			records = append(records, r.policy.Ticket.Records[provider]...)
		}
		// Keep the PR that needs the person visible when a ticket spans repos.
		priority := func(record workfile.Record) int {
			action, count := primaryPRAction(result.Actions, record.ID, record.URL)
			if count == 0 {
				return 5
			}
			return actionRank(action.State)
		}
		slices.SortStableFunc(records, func(a, b workfile.Record) int { return priority(a) - priority(b) })
		tags = m.boardPRTags(records, result.Actions, inner)
		// A single visible tag already names the PR. Keep the target on
		// the action when it could refer to more than one PR.
		for _, action := range result.Actions {
			if action.State == result.State && action.Text == result.Summary {
				if action.Target != item.Key && action.Target != "" {
					if len(records) == 1 && action.Target == records[0].ID && !strings.HasPrefix(ansi.Strip(tags), "+") {
						icon = ""
					} else {
						footer += " on " + shortRecordID(action.Target, records)
					}
				}
				break
			}
		}
	}
	if showAttention && len(result.Actions) > 1 {
		fixes := "fixes"
		if len(result.Actions) == 2 {
			fixes = "fix"
		}
		overflow = append(overflow, fmt.Sprintf("+%d more %s", len(result.Actions)-1, fixes))
	}
	if len(overflow) > 0 {
		add(m.t.dim(strings.Join(overflow, " · ") + " · enter for details"))
	}
	if showAttention {
		// Wrap rather than cut the verb: a truncated fix is not actionable.
		prefix, indent := "", ""
		if icon != "" {
			prefix, indent = icon+" ", "  "
		}
		wrapped := fit(clean(footer), prefix, indent, inner)
		if len(wrapped) > 2 {
			wrapped = []string{wrapped[0], ansi.Truncate(wrapped[1]+" "+strings.TrimSpace(wrapped[2]), inner, "…")}
		}
		for _, line := range wrapped {
			add(m.boardPaint(result.State, line))
		}
	}
	if tags != "" {
		lines = append(lines, "")
		add(tags)
	}
	lines = append(lines, "") // vertical inset inside the card
	contentWidth := width
	if selected {
		contentWidth-- // the border replaces one inset cell, not ticket content
	}
	for i, line := range lines {
		if selected {
			line = strings.TrimPrefix(line, " ")
		}
		lines[i] = (theme{}).onPanel(line, contentWidth)
	}
	if selected {
		// Structural styling only: the terminal supplies the border's text
		// colour, and the same focus indicator remains in NO_COLOR output.
		border := lipgloss.NewStyle().Border(lipgloss.Border{Left: "▌"}, false, false, false, true)
		lines = strings.Split(border.Render(strings.Join(lines, "\n")), "\n")
	}
	// Paint last so the border cell shares the card background instead of
	// exposing a strip of terminal background between the glyph and content.
	for i, line := range lines {
		lines[i] = panel.onPanel(line, width)
	}
	return append(lines, "") // breathing room between cards
}
