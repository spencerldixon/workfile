package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func (v ticketView) board() bool { return v.custom != nil && v.custom.Layout == "board" }

// Plain output retains every ticket and PR in the checked sample, without the
// interactive screen's clipping. It never reads rich ticket details.
func printTrackerBoard(ctx context.Context, e env, all []workfile.Listing, selected ticketView, me string) (int, error) {
	m := newTrackerModel(ctx, e)
	m.all, m.me, m.loaded = all, me, true
	m.who = readActionIdentities(ctx, e, me).who
	base := arrangeTickets(all, e.w.Policy, selected, me)
	checked := 0
	for _, ticket := range base {
		if e.w.Policy.Transitions[ticket.State].End || checked >= trackerCheckBatch {
			continue
		}
		assessment, err := readPolicy(ctx, e, ticket.Key)
		m.ticketReads.byKey[ticket.Key] = &ticketRead{policy: &assessment, policyErr: err, policyStarted: true}
		checked++
	}
	m.showTickets() // only updates local list state; returned UI commands do not run
	e.view.heading(clean(selected.label()))
	e.view.wrap(scopeText(e.w, max(1, e.view.width-2)), "  ")
	e.view.line("")
	if m.total == 0 {
		e.view.wrap(noTickets, "  ")
	}
	for _, lane := range m.boardLanes() {
		e.view.wrap(fmt.Sprintf("%s  %d", boardStateTitle(lane.state), len(lane.indices)), "  ")
		for _, index := range lane.indices {
			item := m.list.VisibleItems()[index].(ticketItem)
			for _, line := range m.boardCard(item, false, max(12, e.view.width)) {
				e.view.line(strings.TrimRight(line, " "))
			}
		}
		e.view.line("")
	}
	e.view.wrap(fmt.Sprintf("Checked %d of %d active tickets; unchecked PRs and actions are unknown. This is a sample when tickets remain unchecked.", checked, lenActiveTickets(e.w, base)), "  ")
	return 0, nil
}

func lenActiveTickets(w *workfile.Workspace, tickets []workfile.Listing) int {
	count := 0
	for _, ticket := range tickets {
		if !w.Policy.Transitions[ticket.State].End {
			count++
		}
	}
	return count
}

// boardLane keeps list indices, so search, selection and cached reads have one
// owner. Origins and provider statuses never determine the columns.
type boardLane struct {
	state   string
	indices []int
}

func (m *trackerModel) boardLanes() []boardLane {
	var lanes []boardLane
	for _, state := range m.e.w.Policy.States {
		if filter := m.shownView().custom.Filter.States; len(filter) == 0 || slices.Contains(filter, state) {
			lanes = append(lanes, boardLane{state: state})
		}
	}
	unknown := boardLane{state: "Unmapped"}
	for i, item := range m.list.VisibleItems() {
		state := item.(ticketItem).State
		at := slices.IndexFunc(lanes, func(l boardLane) bool { return l.state == state })
		if at < 0 {
			unknown.indices = append(unknown.indices, i)
		} else {
			lanes[at].indices = append(lanes[at].indices, i)
		}
	}
	if len(unknown.indices) > 0 {
		lanes = append(lanes, unknown)
	}
	return lanes
}

func (m *trackerModel) moveCard(direction string) tea.Cmd {
	lanes := m.boardLanes()
	lane, row := -1, 0
	for i, l := range lanes {
		if at := slices.Index(l.indices, m.list.Index()); at >= 0 {
			lane, row = i, at
			break
		}
	}
	if lane < 0 {
		return nil
	}
	target := m.list.Index()
	switch direction {
	case "up":
		target = lanes[lane].indices[max(0, row-1)]
	case "down":
		target = lanes[lane].indices[min(len(lanes[lane].indices)-1, row+1)]
	default:
		step := 1
		if direction == "left" {
			step = -1
		}
		for next := lane + step; next >= 0 && next < len(lanes); next += step {
			if len(lanes[next].indices) > 0 {
				target = lanes[next].indices[min(row, len(lanes[next].indices)-1)]
				break
			}
		}
	}
	m.list.Select(target)
	return tea.Batch(m.follow(), m.pumpChecks())
}

// boardCard draws cached evidence only. Unchecked cards never imply success.
// PR actions stay beneath their PR, rather than repeating them per destination.
func (m *trackerModel) boardCard(item ticketItem, selected bool, width int) []string {
	inner := max(1, width-4)
	showAttention := m.shownView().custom.Attention != "off"
	result := m.nextActions(item.Key)
	head := m.t.bold(m.v.link(clean(item.Key), clean(item.URL)))
	if showAttention {
		head = m.boardPaint(result.State, attentionIcon(result.State, m.t.color)) + " " + head
	}
	if selected {
		head += m.t.dim(" selected")
	}
	lines := []string{"  " + ansi.Truncate(head, width-2, "…")}
	lines = append(lines, "  "+ansi.Truncate(clean(item.Title), width-4, "…"))
	if showAttention {
		ticketActions := 0
		for _, action := range result.Actions {
			if action.Target == item.Key {
				lines = append(lines, fit(m.boardPaint(action.State, attentionIcon(action.State, m.t.color)+" "+clean(action.Text)), "  ", "  ", width-2)...)
				ticketActions++
			}
		}
		if ticketActions == 0 {
			label := result.Summary
			if len(result.Actions) > 0 {
				label = map[workfile.AttentionState]string{workfile.NeedsMe: "Needs you", workfile.Waiting: "Waiting", workfile.ReadyToMove: "Ready to move", workfile.AttentionUnknown: "Cannot establish next action", workfile.AttentionDone: "Done"}[result.State]
			}
			lines = append(lines, fit(m.boardPaint(result.State, attentionIcon(result.State, m.t.color)+" "+clean(label)), "  ", "  ", width-2)...)
		}
	}
	r := m.ticketReads.byKey[item.Key]
	if r != nil && r.policy != nil && r.policyErr == nil {
		for _, provider := range workfile.SortedKeys(r.policy.Ticket.Records) {
			for _, record := range r.policy.Ticket.Records[provider] {
				label := m.v.link(clean(record.ID), clean(record.URL))
				lines = append(lines, "  "+ansi.Truncate(m.t.bold(label), inner, "…"))
				if !m.v.hyperlinks() && record.URL != "" {
					lines = append(lines, fit(clean(record.URL), "    ", "    ", width-2)...)
				}
				if showAttention {
					for _, action := range result.Actions {
						if action.Target == record.ID && (action.URL == "" || action.URL == record.URL) {
							lines = append(lines, fit(m.boardPaint(action.State, attentionIcon(action.State, m.t.color)+" "+clean(action.Text)), "    ", "    ", width-2)...)
						}
					}
				}
			}
		}
	} else {
		lines = append(lines, "  "+m.t.dim("PRs not checked"))
	}
	for i, line := range lines {
		lines[i] = m.boardTheme.onPanel(ansi.Truncate(line, width, "…"), width)
	}
	return append(lines, "")
}

const (
	boardMinColumnWidth = 28
	boardColumnGap      = 3
)

func (m *trackerModel) boardLines(height int) []string {
	lines := []string{""}
	if message := m.resultMessage(); message != nil {
		return append(lines, message...)
	}

	lanes := m.boardLanes()
	inner := max(1, m.width-4)
	count := min(len(lanes), max(1, (inner+boardColumnGap)/(boardMinColumnWidth+boardColumnGap)))
	selectedLane := 0
	for i, lane := range lanes {
		if slices.Contains(lane.indices, m.list.Index()) {
			selectedLane = i
		}
	}
	start := min(max(0, selectedLane-count+1), max(0, len(lanes)-count))
	width := (inner - boardColumnGap*(count-1)) / max(1, count)
	// Leave room for the lane headings, the gap below them and bottom padding.
	bodyHeight := max(1, height-len(lines)-3)
	columns := make([][]string, count)
	for i, lane := range lanes[start : start+count] {
		title := alignPair(m.t.bold(boardStateTitle(lane.state)), m.t.dim(fmt.Sprint(len(lane.indices))), width)
		var content []string
		selectedTop, selectedBottom := -1, 0
		for _, index := range lane.indices {
			item := m.list.VisibleItems()[index].(ticketItem)
			selected := index == m.list.Index()
			if selected {
				selectedTop = len(content)
			}
			content = append(content, m.boardTile(item, selected, width)...)
			if selected {
				selectedBottom = len(content) - 1
			}
		}
		pane := viewport.New(viewport.WithWidth(width), viewport.WithHeight(bodyHeight))
		pane.SetContent(strings.Join(content, "\n"))
		if selectedTop >= 0 {
			pane.SetYOffset(max(0, min(selectedTop, selectedBottom-bodyHeight)))
		}
		columns[i] = append([]string{title, ""}, strings.Split(pane.View(), "\n")...)
	}
	for row := 0; row < bodyHeight+2; row++ {
		var parts []string
		for _, column := range columns {
			line := ""
			if row < len(column) {
				line = column[row]
			}
			parts = append(parts, m.t.onPanel(line, width))
		}
		lines = append(lines, "  "+strings.Join(parts, strings.Repeat(" ", boardColumnGap))+"  ")
	}
	return lines
}
