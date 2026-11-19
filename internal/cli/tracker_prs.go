package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

// PR choices are a snapshot of cached evidence. Browser commands never consult
// a later selection or provider result, and every destination is validated.
type trackerPR struct {
	id, title, url, summary string
	state                   workfile.AttentionState
	allURLs                 []string
}

func (p trackerPR) FilterValue() string { return p.id + " " + p.title + " " + p.summary }

type trackerPRRow struct{ t theme }

func (trackerPRRow) Height() int                         { return 2 }
func (trackerPRRow) Spacing() int                        { return 1 }
func (trackerPRRow) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d trackerPRRow) Render(w io.Writer, l list.Model, index int, item list.Item) {
	p := item.(trackerPR)
	label := clean(p.id)
	if index == l.Index() {
		label = "[" + label + "]"
	}
	if p.title != "" {
		label += "  " + clean(p.title)
	}
	label = d.t.bold(ansi.Truncate(label, l.Width(), "…"))
	detail := clean(p.summary)
	paint := d.t.dim
	if len(p.allURLs) == 0 {
		detail = attentionIcon(p.state) + " " + detail
		switch p.state {
		case workfile.NeedsMe:
			paint = d.t.bad
		case workfile.ReadyToMove:
			paint = d.t.good
		}
	}
	fmt.Fprint(w, label+"\n"+paint(ansi.Truncate(detail, l.Width(), "…")))
}

// shortRecordID drops the owner and repo prefix when the rest stays unique
// among the ticket's records, so app#1 reads instead of example/app#1.
func shortRecordID(id string, records []workfile.Record) string {
	id = clean(id)
	slash := strings.LastIndex(id, "/")
	if slash < 0 {
		return id
	}
	short := id[slash+1:]
	matches := 0
	for _, other := range records {
		if other.ID == short || strings.HasSuffix(other.ID, "/"+short) {
			matches++
		}
	}
	if matches == 1 {
		return short
	}
	return id
}

func primaryPRAction(actions []workfile.WorkAction, target, url string) (workfile.WorkAction, int) {
	var primary workfile.WorkAction
	count := 0
	for _, action := range actions {
		if action.Target != target || url != "" && action.URL != "" && action.URL != url {
			continue
		}
		if count == 0 || actionRank(action.State) < actionRank(primary.State) {
			primary = action
		}
		count++
	}
	return primary, count
}

func pullRequestStatus(record workfile.Record, action workfile.WorkAction) (icon, label string, colour int) {
	is := func(fact, value string) bool {
		s, _ := record.Facts[fact].(string)
		return record.Unavailable[fact] == "" && s == value
	}
	requests, _ := record.Facts["review_requests"].([]string)
	if record.Unavailable["review_requests"] != "" {
		requests = nil
	}
	changes, _ := record.Facts["changes_requested"].([]string)
	changesNeeded := record.Unavailable["changes_requested"] == "" && slices.ContainsFunc(changes, func(reviewer string) bool {
		return !slices.ContainsFunc(requests, func(request string) bool { return strings.EqualFold(reviewer, request) })
	})
	switch {
	case is("state", "merged"):
		return "", "Merged", 2
	case is("state", "closed"):
		return "󰅖", "Closed", 0
	case !is("state", "open"):
		return "?", "Unknown", 0
	case is("checks", "failure"):
		return "󰅖", "CI failed", 1
	case is("mergeable", "conflicting"):
		return "!", "Conflict", 1
	case changesNeeded:
		return "󰅖", "Changes", 1
	case action.State == workfile.NeedsMe:
		return "!", "Action needed", 1
	case is("checks", "pending"):
		return "󰔛", "CI pending", 3
	case is("merge_queue", "queued"):
		return "󰔛", "Queued", 3
	case len(requests) > 0:
		return "󰆉", "Review pending", 3
	case action.State == workfile.Waiting:
		return "󰔛", "Waiting", 3
	case is("draft", "true"):
		return "󰏫", "Draft", 0
	case is("checks", "success"):
		return "󰄬", "CI passed", 2
	default:
		return "", "Open", 0
	}
}

func (m *trackerModel) pullRequests() []trackerPR {
	r := m.ticketReads.byKey[m.current]
	if r == nil || r.policy == nil || r.policyErr != nil {
		return nil
	}
	actions := m.nextActions(m.current).Actions
	var targets []trackerPR
	add := func(p trackerPR) {
		if !trackerWebURL(p.url) || slices.ContainsFunc(targets, func(existing trackerPR) bool { return existing.url == p.url }) {
			return
		}
		targets = append(targets, p)
	}
	for _, provider := range workfile.SortedKeys(r.policy.Ticket.Records) {
		for _, record := range r.policy.Ticket.Records[provider] {
			p := trackerPR{id: record.ID, title: record.Title, url: record.URL, state: workfile.AttentionUnknown, summary: "No action established"}
			action, count := primaryPRAction(actions, record.ID, record.URL)
			if count > 0 {
				p.state, p.summary = action.State, action.Text
				if count > 1 {
					p.summary += fmt.Sprintf(" +%d more", count-1)
				}
			} else if record.Unavailable["state"] != "" {
				p.summary = "PR state unavailable"
			} else if state, _ := record.Facts["state"].(string); state != "" {
				p.summary = boardStateTitle(state)
				if state == "closed" || state == "merged" {
					p.state = workfile.AttentionDone
				} else {
					p.summary += " · no action established"
				}
			}
			add(p)
		}
	}
	// An assessment may retain a PR action when no record detail is available.
	for _, action := range actions {
		if action.Target != "" && action.Target != m.current {
			add(trackerPR{id: action.Target, url: action.URL, state: action.State, summary: action.Text})
		}
	}
	slices.SortStableFunc(targets, func(a, b trackerPR) int { return actionRank(a.state) - actionRank(b.state) })
	return targets
}

func (m *trackerModel) openPullRequests() tea.Cmd {
	prs := m.pullRequests()
	switch len(prs) {
	case 0:
		m.notice = "No matching PR link is available."
		return nil
	case 1:
		return m.openPRURLs([]string{prs[0].url})
	}
	items := make([]list.Item, 0, len(prs)+1)
	urls := make([]string, 0, len(prs))
	for _, pr := range prs {
		items = append(items, pr)
		urls = append(urls, pr.url)
	}
	items = append(items, trackerPR{id: fmt.Sprintf("Open all %d PRs", len(urls)), summary: "Open each linked PR in your browser", allURLs: urls})
	m.projectPicker = list.New(items, trackerPRRow{m.t}, 0, 0)
	configureTrackerPicker(&m.projectPicker)
	m.projectGeneration++
	m.pickingProject, m.pickingPR = false, true
	m.prTicket = m.current
	m.resizeProjectPicker()
	return nil
}

type trackerPRsOpenedMsg struct{ opened, failed int }

func (m *trackerModel) openPRURLs(urls []string) tea.Cmd {
	open := m.open
	targets := slices.Clone(urls)
	return func() tea.Msg {
		result := trackerPRsOpenedMsg{}
		for _, target := range targets {
			if !trackerWebURL(target) {
				result.failed++
				continue
			}
			if err := open(target); err != nil {
				result.failed++
			} else {
				result.opened++
			}
		}
		return result
	}
}

func (m *trackerModel) choosePR(p trackerPR) tea.Cmd {
	m.pickingPR = false
	if len(p.allURLs) > 0 {
		return m.openPRURLs(p.allURLs)
	}
	return m.openPRURLs([]string{p.url})
}

// Shared list setup keeps the scope and PR modals consistent without coupling
// either picker to a tracker or code host.
func configureTrackerPicker(picker *list.Model) {
	picker.SetShowTitle(false)
	picker.SetShowFilter(false)
	picker.SetShowHelp(false)
	picker.SetShowStatusBar(false)
	picker.SetShowPagination(false)
	picker.DisableQuitKeybindings()
	picker.KeyMap.ShowFullHelp.SetEnabled(false)
	picker.KeyMap.CloseFullHelp.SetEnabled(false)
}

func (m *trackerModel) prOpenNotice(msg trackerPRsOpenedMsg) string {
	if msg.failed == 0 {
		return ""
	}
	return fmt.Sprintf("Opened %d of %d PRs; %d could not be opened.", msg.opened, msg.opened+msg.failed, msg.failed)
}

// Keep all untrusted provider text out of modal headings and captions.
func (m *trackerModel) prPickerCaption() string {
	return fmt.Sprintf("%s · %d linked PRs", clean(m.prTicket), len(m.projectPicker.Items())-1)
}
