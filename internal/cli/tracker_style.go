package cli

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func (t theme) roundedTag(body string, fill, ink lipgloss.ANSIColor) string {
	cap := t.style(func(st lipgloss.Style) lipgloss.Style { return st.Foreground(fill) })
	middle := t.style(func(st lipgloss.Style) lipgloss.Style {
		return st.Foreground(ink).Background(fill)
	})
	return cap.Render("") + middle.Render(body) + cap.Render("")
}

// alignPair fits two styled labels and keeps the second against the right edge.
func alignPair(left, right string, width int) string {
	right = ansi.Truncate(right, max(0, width), "")
	room := max(0, width-ansi.StringWidth(right))
	if right != "" {
		room = max(0, room-2)
	}
	left = ansi.Truncate(left, room, "…")
	return left + strings.Repeat(" ", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))) + right
}

func attentionIcon(state workfile.AttentionState) string {
	switch state {
	case workfile.NeedsMe:
		return "✗"
	case workfile.ReadyToMove:
		return "✓"
	case workfile.Waiting:
		return "…"
	case workfile.AttentionDone:
		return "·"
	default:
		return "!"
	}
}

func trackerAttentionIcon(state workfile.AttentionState, colour bool) string {
	switch state {
	case workfile.NeedsMe:
		return setupIcon(colour, attentionIcon(state), "󰅖")
	case workfile.ReadyToMove:
		return setupIcon(colour, attentionIcon(state), "󰄬")
	case workfile.Waiting:
		return setupIcon(colour, attentionIcon(state), "󰔛")
	case workfile.AttentionUnknown:
		return setupIcon(colour, attentionIcon(state), "󰋼")
	default:
		return attentionIcon(state)
	}
}

func attentionStyle(state workfile.AttentionState, neutral lipgloss.Style) lipgloss.Style {
	switch state {
	case workfile.NeedsMe:
		return lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(1))
	case workfile.ReadyToMove:
		return lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(2))
	case workfile.Waiting, workfile.AttentionUnknown:
		return lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(3))
	default:
		return neutral
	}
}
