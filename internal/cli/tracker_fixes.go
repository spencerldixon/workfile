package cli

import (
	"fmt"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

// maxFailedCheckReads bounds the extra reads for one ticket. A ticket with
// more failing PRs than this still links each fix to its checks page.
const maxFailedCheckReads = 4

type failedChecksMsg struct {
	read   *ticketRead
	failed map[string][]workfile.FailedCheck
}

// loadFailedChecks names the failing jobs of the open ticket's PRs, one read
// per PR with failing checks. Board cards never trigger it, and a failed read
// leaves the fix linked to the checks page.
func (m *trackerModel) loadFailedChecks() tea.Cmd {
	if m.showingBoard() {
		return nil
	}
	r := m.ticketReads.byKey[m.current]
	if r == nil || r.policy == nil || r.policyErr != nil || r.failedStarted {
		return nil
	}
	r.failedStarted = true
	type target struct {
		connection string
		record     workfile.Record
	}
	var targets []target
	for _, connection := range workfile.SortedKeys(r.policy.Ticket.Records) {
		for _, record := range r.policy.Ticket.Records[connection] {
			state, _ := record.Facts["state"].(string)
			checks, _ := record.Facts["checks"].(string)
			if state == "open" && checks == "failure" && record.Unavailable["checks"] == "" && len(targets) < maxFailedCheckReads {
				targets = append(targets, target{connection, record})
			}
		}
	}
	if len(targets) == 0 {
		r.failed = map[string][]workfile.FailedCheck{}
		return nil
	}
	ctx, client := r.ctx, m.e.client
	if ctx == nil {
		ctx = m.ctx
	}
	return func() tea.Msg {
		failed := map[string][]workfile.FailedCheck{}
		for _, t := range targets {
			if checks, err := client.FailedChecks(ctx, t.connection, t.record); err == nil {
				failed[t.record.ID] = checks
			}
		}
		return failedChecksMsg{r, failed}
	}
}

func (m *trackerModel) showFailedChecks(msg failedChecksMsg) {
	if !m.ticketReads.contains(msg.read) {
		return
	}
	msg.read.failed = msg.failed
	if msg.read == m.ticketReads.byKey[m.current] {
		m.fill(false)
	}
}

// fixes are the numbered fixes in the open ticket's Workfile pane, in the
// order the pane numbers them.
func (m *trackerModel) fixes() []workfile.WorkAction {
	r := m.ticketReads.byKey[m.current]
	if r == nil || r.policy == nil || r.policyErr != nil || m.whoLoading {
		return nil
	}
	return m.paneFor(r, m.policyWidth()+4).numbered()
}

// fixURL is where a fix is done: the failing job's log when there is exactly
// one, the checks page for several, otherwise the PR or ticket it names.
func fixURL(action workfile.WorkAction, failed map[string][]workfile.FailedCheck) string {
	if action.Fact == "checks" {
		if jobs := failed[action.Target]; len(jobs) == 1 && trackerWebURL(jobs[0].URL) {
			return jobs[0].URL
		}
		if trackerWebURL(action.ChecksURL) {
			return action.ChecksURL
		}
	}
	if trackerWebURL(action.URL) {
		return action.URL
	}
	return ""
}

// openFix opens fix n, counting from 1, in the browser.
func (m *trackerModel) openFix(n int) tea.Cmd {
	fixes := m.fixes()
	if n > len(fixes) {
		if len(fixes) == 0 {
			m.notice = "This ticket has no numbered fixes."
		} else {
			m.notice = fmt.Sprintf("There is no fix %d.", n)
		}
		return nil
	}
	var failed map[string][]workfile.FailedCheck
	if r := m.ticketReads.byKey[m.current]; r != nil {
		failed = r.failed
	}
	target := fixURL(fixes[n-1], failed)
	if target == "" {
		m.notice = "No safe link is available for this fix."
		return nil
	}
	open := m.open
	return func() tea.Msg { return ticketOpenedMsg{open(target)} }
}

// fixKey reports which fix a key press names, if any.
func fixKey(msg tea.KeyPressMsg) (int, bool) {
	s := msg.String()
	if len(s) != 1 || s < "1" || s > "9" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// fixHelp is the help entry for the number keys, sized to the fixes shown.
func (m *trackerModel) fixHelp() []key.Binding {
	count := len(m.fixes())
	if count == 0 {
		return nil
	}
	label := "1"
	if count > 1 {
		label = fmt.Sprintf("1-%d", min(count, 9))
	}
	return []key.Binding{key.NewBinding(key.WithKeys(label), key.WithHelp(label, "open fix"))}
}
