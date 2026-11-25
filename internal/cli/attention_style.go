package cli

import (
	"strconv"

	"charm.land/lipgloss/v2"
	"workfile/internal/workfile"
)

type attentionMark struct {
	plain, nerd string
	colour      lipgloss.ANSIColor
}

const (
	statusRed   lipgloss.ANSIColor = 1
	statusGreen lipgloss.ANSIColor = 2
	statusAmber lipgloss.ANSIColor = 3
)

func markForAttention(state workfile.AttentionState) attentionMark {
	switch state {
	case workfile.NeedsMe:
		return attentionMark{"✗", "󰅖", statusRed}
	case workfile.ReadyToMove:
		return attentionMark{"✓", "󰄬", statusGreen}
	case workfile.Waiting:
		return attentionMark{"…", "󰔛", statusAmber}
	case workfile.AttentionUnknown:
		return attentionMark{"!", "󰋼", statusAmber}
	case workfile.AttentionDone:
		return attentionMark{plain: "·"}
	default:
		return attentionMark{plain: "!"}
	}
}

func attentionIcon(state workfile.AttentionState, colour bool) string {
	mark := markForAttention(state)
	if mark.nerd == "" {
		return mark.plain
	}
	return terminalIcon(colour, mark.plain, mark.nerd)
}

func terminalIcon(nerd bool, fallback, glyph string) string {
	if nerd {
		return glyph
	}
	return fallback
}

func attentionSGR(state workfile.AttentionState) string {
	return strconv.Itoa(30 + int(markForAttention(state).colour))
}

func outOfPolicyIcon(colour bool) string {
	return terminalIcon(colour, "‼", "󰀦")
}

func reviewIcon(colour bool) string {
	return terminalIcon(colour, attentionIcon(workfile.Waiting, false), "󰆉")
}

func attentionStyle(state workfile.AttentionState, neutral lipgloss.Style) lipgloss.Style {
	if colour := markForAttention(state).colour; colour != 0 {
		return lipgloss.NewStyle().Foreground(colour)
	}
	return neutral
}

func (t theme) verdictMark(ok bool) string {
	if ok {
		return t.mark(workfile.ReadyToMove)
	}
	return t.mark(workfile.NeedsMe)
}
