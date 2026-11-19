package cli

import (
	"context"
	"net/url"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

const trackerCheckBatch = 12
const trackerCheckWorkers = 2

func actionRank(state workfile.AttentionState) int {
	return slices.Index([]workfile.AttentionState{workfile.NeedsMe, workfile.AttentionUnknown, workfile.Waiting, workfile.ReadyToMove, workfile.AttentionDone}, state)
}

type ticketActionIdentityMsg struct {
	who        map[string]string
	incomplete bool
}

func attentionEnabled(s workfile.TrackerSettings) bool {
	return slices.Contains(s.Columns, "action") || slices.ContainsFunc(s.Views, func(v workfile.TrackerView) bool { return v.Filter.Attention != "" || v.Layout == "board" })
}

func (m *trackerModel) actionIdentity() tea.Cmd {
	if !m.overview || m.whoStarted {
		return nil
	}
	m.whoStarted, m.whoLoading = true, true
	e, ctx, me := m.e, m.ctx, m.me
	return tea.Batch(m.startSpinner(), func() tea.Msg { return readActionIdentities(ctx, e, me) })
}

func readActionIdentities(ctx context.Context, e env, me string) ticketActionIdentityMsg {
	who := map[string]string{}
	overrides, err := workfile.Me()
	if err != nil {
		return ticketActionIdentityMsg{who: who, incomplete: true}
	}
	incomplete := false
	for _, name := range workfile.SortedKeys(e.w.Providers) {
		if name == e.w.Policy.Tracker && me != "" {
			who[name] = me
			continue
		}
		id, err := identityOn(ctx, e.w, e.client, overrides, name)
		if err != nil {
			incomplete = true
			continue
		}
		who[name] = id
	}
	return ticketActionIdentityMsg{who, incomplete}
}

func (m *trackerModel) nextActions(key string) workfile.NextActions {
	r := m.ticketReads.byKey[key]
	if r == nil || !r.policyStarted && !r.policyDone() {
		for _, ticket := range m.all {
			if ticket.Key == key && m.e.w.Policy.Transitions[ticket.State].End {
				return workfile.NextActions{State: workfile.AttentionDone, Summary: "Done"}
			}
		}
		return workfile.NextActions{State: workfile.AttentionUnknown, Summary: "Not checked"}
	}
	if !r.policyDone() {
		return workfile.NextActions{State: workfile.AttentionUnknown, Summary: "Checking…"}
	}
	if r.policyErr != nil || r.policy == nil {
		return workfile.NextActions{State: workfile.AttentionUnknown, Summary: "Cannot check this ticket"}
	}
	if m.whoLoading {
		return workfile.NextActions{State: workfile.AttentionUnknown, Summary: "Identifying action owner…"}
	}
	return m.e.w.NextActions(*r.policy, m.who)
}

func (v ticketView) attention() string {
	if v.custom != nil {
		return v.custom.Filter.Attention
	}
	return ""
}

func (m *trackerModel) baseTickets(view ticketView) []workfile.Listing {
	shown := arrangeTickets(m.all, m.e.w.Policy, view, m.me)
	if origin := m.projectOrigins[view]; origin != "" {
		shown = slices.DeleteFunc(shown, func(t workfile.Listing) bool { return !strings.EqualFold(t.Origin, origin) })
	}
	return shown
}

func (m *trackerModel) shownTickets(view ticketView) []workfile.Listing {
	shown := m.baseTickets(view)
	if view.attention() == "" {
		return shown
	}
	if m.whoLoading {
		return nil
	}
	return slices.DeleteFunc(shown, func(t workfile.Listing) bool {
		r := m.ticketReads.byKey[t.Key]
		return m.e.w.Policy.Transitions[t.State].End || r == nil || !r.policyDone() || string(m.nextActions(t.Key).State) != view.attention()
	})
}

func (m *trackerModel) checkProgress(view ticketView) (checked, total int) {
	for _, ticket := range m.baseTickets(view) {
		if m.e.w.Policy.Transitions[ticket.State].End {
			continue
		}
		total++
		if r := m.ticketReads.byKey[ticket.Key]; r != nil && r.policyDone() {
			checked++
		}
	}
	return checked, total
}

func (m *trackerModel) unclearChecks(view ticketView) int {
	count := 0
	if m.whoLoading {
		return 0
	}
	for _, ticket := range m.baseTickets(view) {
		if m.e.w.Policy.Transitions[ticket.State].End {
			continue
		}
		if r := m.ticketReads.byKey[ticket.Key]; r != nil && r.policyDone() && m.nextActions(ticket.Key).State == workfile.AttentionUnknown {
			count++
		}
	}
	return count
}

// pumpChecks checks a bounded sample, with the visible page first. It never
// fetches descriptions, comments, images, or the whole board's PR details.
func (m *trackerModel) pumpChecks() tea.Cmd {
	if !m.overview || !m.loaded {
		return nil
	}
	active := 0
	for _, read := range m.ticketReads.byKey {
		if read.policyStarted && !read.policyDone() {
			active++
		}
	}
	if active >= trackerCheckWorkers {
		return nil
	}
	view := m.shownView()
	budget := m.checkBudgets[view]
	if budget == 0 {
		budget = trackerCheckBatch
	}
	var candidates []workfile.Listing
	seen := map[string]bool{}
	add := func(ticket workfile.Listing) {
		if seen[ticket.Key] || m.e.w.Policy.Transitions[ticket.State].End || len(candidates) >= budget {
			return
		}
		seen[ticket.Key] = true
		candidates = append(candidates, ticket)
	}
	visible := m.list.VisibleItems()
	start, end := m.list.Paginator.GetSliceBounds(len(visible))
	for _, item := range visible[start:end] {
		add(item.(ticketItem).Listing)
	}
	for _, ticket := range m.baseTickets(view) {
		add(ticket)
	}
	var cmds []tea.Cmd
	for _, ticket := range candidates {
		if active >= trackerCheckWorkers {
			break
		}
		r := m.ticketReads.ensure(m.ctx, ticket.Key)
		if r.policyStarted || r.policyDone() {
			continue
		}
		cmds = append(cmds, r.checkPolicy(m.e, ticket.Key))
		active++
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(append(cmds, m.startSpinner())...)
}

// An empty computed view must still be refreshable after an outage or a change
// elsewhere. Refresh only its bounded sample, not the whole team's board.
func (m *trackerModel) refreshAttentionSample() tea.Cmd {
	view := m.shownView()
	budget := m.checkBudgets[view]
	if budget == 0 {
		budget = trackerCheckBatch
	}
	count := 0
	for _, ticket := range m.baseTickets(view) {
		if m.e.w.Policy.Transitions[ticket.State].End {
			continue
		}
		if count >= budget {
			break
		}
		count++
		if r := m.ticketReads.byKey[ticket.Key]; r != nil && r.policyDone() {
			m.ticketReads.discard(ticket.Key)
		}
	}
	if m.whoIncomplete {
		m.whoStarted = false
	}
	return tea.Batch(m.showTickets(), m.actionIdentity())
}

func trackerActionText(key string, result workfile.NextActions) string {
	icon := attentionIcon(result.State)
	if result.Summary == "Checking…" || result.Summary == "Identifying action owner…" {
		icon = "…"
	}
	summary := result.Summary
	for _, action := range result.Actions {
		if action.State == result.State && action.Text == result.Summary && action.Target != "" && action.Target != key {
			summary += " · " + action.Target
			break
		}
	}
	return icon + " " + clean(summary)
}

func trackerWebURL(target string) bool {
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && clean(target) == target
}

func (m *trackerModel) actionURL(checks bool) string {
	usable := trackerWebURL
	result := m.nextActions(m.current)
	item, _ := m.selected()
	for _, action := range result.Actions {
		if checks && usable(action.ChecksURL) {
			return action.ChecksURL
		}
		if !checks && action.Target != item.Key && usable(action.URL) {
			return action.URL
		}
	}
	if r := m.ticketReads.byKey[m.current]; r != nil && r.policy != nil {
		for _, provider := range workfile.SortedKeys(r.policy.Ticket.Records) {
			for _, record := range r.policy.Ticket.Records[provider] {
				if checks && usable(record.ChecksURL) {
					return record.ChecksURL
				}
				if !checks && usable(record.URL) {
					return record.URL
				}
			}
		}
	}
	return ""
}
