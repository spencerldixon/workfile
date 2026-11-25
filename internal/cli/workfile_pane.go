package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

// workfilePane is the shared, read-only view of one ticket's workflow and
// unresolved actions. Callers supply evidence; rendering never fetches it or
// marks an unmet requirement as dismissed.
type workfilePane struct {
	v                      view
	t, panelTheme          theme
	policy                 workfile.Policy
	assessment             *workfile.Assessment
	actions                workfile.NextActions
	width                  int
	workflow               string
	focusPR                string
	checkedAt, now         time.Time
	pending, ownersPending bool
	err                    error
	flow                   *picture // the workflow as an image, when drawn
	failed                 map[string][]workfile.FailedCheck
}

// numbered is the fixes the pane numbers, in its order. The number keys open
// them, so this and sections must agree.
func (p workfilePane) numbered() []workfile.WorkAction {
	if p.pending || p.ownersPending || p.err != nil || p.assessment == nil {
		return nil
	}
	var fixes []workfile.WorkAction
	for _, group := range p.groups() {
		for _, action := range group.actions {
			if action.State == workfile.NeedsMe {
				fixes = append(fixes, action)
			}
		}
	}
	return fixes
}

func (m *trackerModel) paneFor(r *ticketRead, panel int) workfilePane {
	p := workfilePane{
		v: m.v, t: m.t, panelTheme: m.panelTheme, policy: m.e.w.Policy,
		actions: m.nextActions(m.current), width: trackerPaneWidth(panel),
		workflow: m.e.tracker.Preview.Workflow, now: m.now(),
		pending: r == nil || !r.policyDone(), ownersPending: m.whoLoading,
	}
	if r != nil {
		p.assessment, p.err, p.checkedAt, p.failed = r.policy, r.policyErr, r.checkedAt, r.failed
	}
	if spec, ok := m.workflowSpec(r); ok {
		if cached := m.flows[spec]; cached != nil {
			p.flow = &cached.picture
		}
	}
	return p
}

// policyWidth is the text width of the Workfile panel, as ticketText lays it out.
func (m *trackerModel) policyWidth() int {
	left, right := m.columns()
	if right > 0 {
		left = right
	}
	return trackerPaneWidth(left)
}

// workflowSpec is the picture the current ticket needs, when the terminal can
// show one and the person has not asked for text.
func (m *trackerModel) workflowSpec(r *ticketRead) (flowSpec, bool) {
	p := m.e.w.Policy
	if !m.gfx.on || !m.v.color || m.e.tracker.Preview.Workflow != "auto" || r == nil || r.policy == nil || r.policyErr != nil ||
		len(p.States) == 0 || !slices.Contains(p.States, r.policy.Ticket.State) {
		return flowSpec{}, false
	}
	return ticketFlowSpec(p, *r.policy, m.policyWidth()), true
}

// loadWorkflow draws the current ticket's workflow as a picture, off the
// update loop. Until it arrives, and if it cannot be drawn, the text stays.
func (m *trackerModel) loadWorkflow() tea.Cmd {
	if m.shownView().board() && !m.onTicket {
		return nil
	}
	r := m.ticketReads.byKey[m.current]
	spec, ok := m.workflowSpec(r)
	if !ok || spec.width < minImageWidth || m.flowTried[spec] {
		return nil
	}
	m.flowTried[spec] = true
	return drawTicketWorkflow(m.e.w.Policy, *r.policy, m.gfx, spec)
}

func (m *trackerModel) showWorkflow(msg workflowPictureMsg) tea.Cmd {
	if !msg.ok || m.flows[msg.spec] != nil {
		return nil
	}
	p := &ticketPicture{picture: picture{id: m.allocate(), cols: msg.img.Bounds().Dx() / m.gfx.cellW, rows: msg.rows}}
	m.flows[msg.spec] = p
	m.fill(false)
	return sendPicture(msg.img, p.picture)
}

// loadPreviewPictures asks for every picture the ticket view can use.
func (m *trackerModel) loadPreviewPictures() tea.Cmd {
	return tea.Batch(m.loadPreviewHeadings(), m.loadWorkflow(), m.loadFailedChecks())
}

func (p workfilePane) sections() [][]string {
	sections := [][]string{p.where()}
	if summary := p.summary(); len(summary) > 0 {
		sections = append(sections, summary)
	}
	if p.pending || p.err != nil || p.assessment == nil {
		return sections
	}
	groups := p.groups()
	number := 0
	if len(groups) > 0 && groups[0].target == p.assessment.Ticket.Key {
		lines := []string{p.t.bold("Ticket actions")}
		lines = append(lines, p.actionRows(groups[0], &number, p.width)...)
		sections = append(sections, lines)
		groups = groups[1:]
	}
	if len(groups) > 0 {
		var records []workfile.Record
		needsYou := 0
		for _, group := range groups {
			records = append(records, group.record)
			if group.state == workfile.NeedsMe {
				needsYou++
			}
		}
		title := []string{p.t.bold("Pull requests")}
		if needsYou > 0 {
			count := p.t.bad(quantity(needsYou, "PR needs you", "PRs need you"))
			if ansi.StringWidth(title[0])+ansi.StringWidth(count)+2 <= p.width {
				title[0] = alignPair(title[0], count, p.width)
			} else {
				title = append(title, fit(count, "", "", p.width)...)
			}
		}
		sections = append(sections, title)
		for _, group := range groups {
			sections = append(sections, p.prBox(group, records, &number))
		}
	}
	if !p.checkedAt.IsZero() {
		layout := "15:04"
		if p.checkedAt.Format("2006-01-02") != p.now.Format("2006-01-02") {
			layout = "2 Jan 15:04"
		}
		sections = append(sections, fit(p.t.dim("Checked at "+p.checkedAt.Format(layout)+" · r refresh"), "", "", p.width))
	}
	return sections
}

func (p workfilePane) where() []string {
	if p.assessment == nil {
		text := "Checking the policy…"
		if !p.pending {
			text = "! Cannot check the policy."
			if p.err != nil {
				text = "! Cannot check the policy: " + clean(p.err.Error())
			}
		}
		return fit(p.t.dim(text), "", "", p.width)
	}
	current := p.assessment.Ticket.State
	if p.workflow == "off" || len(p.policy.States) == 0 {
		return fit(p.t.bold("Current stage: "+boardStateTitle(current)), "", "", p.width)
	}
	lines := ticketWorkflowLines(p.policy, *p.assessment, p.v, p.width, p.flow)
	if !slices.Contains(p.policy.States, current) {
		return append(lines, fit(p.t.dim("! Current stage is not mapped to this workflow."), "", "", p.width)...)
	}
	if caption := p.stageCaption(); caption != "" {
		lines = append(lines, "", centre([]string{caption}, p.width)[0])
	}
	return lines
}

// stageCaption says in words what the colour of the current stage means, so
// the diagram never relies on colour alone. The stage is named in the diagram.
func (p workfilePane) stageCaption() string {
	switch flowTone(*p.assessment) {
	case green:
		return p.t.mark(workfile.ReadyToMove) + " Can move on"
	case red:
		return p.t.mark(workfile.NeedsMe) + " Blocked"
	}
	return ""
}

func (p workfilePane) summary() []string {
	lines := []string{p.t.bold("Next action")}
	switch {
	case p.pending:
		return append(lines, p.t.dim("Checking…"))
	case p.err != nil || p.assessment == nil:
		return append(lines, fit(p.t.mark(workfile.AttentionUnknown)+" Cannot check this ticket. Press r to retry.", "", "", p.width)...)
	case p.ownersPending:
		return append(lines, fit(p.t.dim("Identifying action owner…"), "", "", p.width)...)
	}
	label := p.actions.Summary
	switch p.actions.State {
	case workfile.NeedsMe:
		lines = nil // Personal actions are visible in the ticket and PR panels.
	case workfile.ReadyToMove:
		if strings.HasPrefix(label, "Move to ") {
			label = "Move to " + boardStateTitle(strings.TrimPrefix(label, "Move to "))
		}
	case workfile.AttentionUnknown:
		label = "Cannot establish next action"
	case workfile.AttentionDone:
		label = "Done. No next action."
	}
	if len(lines) > 0 {
		lines = append(lines, fit(p.t.attention(p.actions.State, attentionIcon(p.actions.State, p.t.color)+" "+clean(label)), "", "", p.width)...)
	}
	if len(p.actions.Actions) > 0 && p.actions.State != workfile.ReadyToMove && p.actions.State != workfile.AttentionDone {
		var allowed []string
		for _, route := range p.assessment.Routes {
			if !route.Backward && route.Available() {
				allowed = append(allowed, boardStateTitle(route.To))
			}
		}
		if len(allowed) > 0 {
			lines = append(lines, fit(p.t.dim("Policy allows → "+strings.Join(allowed, " · → ")+". Unresolved actions remain."), "", "", p.width)...)
		}
	}
	return lines
}

type paneActionGroup struct {
	target, url string
	record      workfile.Record
	state       workfile.AttentionState
	actions     []workfile.WorkAction
}

func (p workfilePane) groups() []paneActionGroup {
	ticket := p.assessment.Ticket
	var groups []paneActionGroup
	type recordKey struct{ id, url string }
	byRecord := map[recordKey]int{}
	for _, provider := range workfile.SortedKeys(ticket.Records) {
		for _, record := range ticket.Records[provider] {
			key := recordKey{record.ID, record.URL}
			if _, seen := byRecord[key]; seen {
				continue
			}
			byRecord[key] = len(groups)
			groups = append(groups, paneActionGroup{target: record.ID, url: record.URL, record: record, state: workfile.AttentionDone})
		}
	}
	if p.ownersPending {
		return groups
	}
	for _, action := range p.actions.Actions {
		if action.State == workfile.ReadyToMove || action.State == workfile.AttentionDone {
			continue
		}
		if action.Target == ticket.Key && ticket.URL != "" {
			action.URL = ticket.URL
		}
		key := recordKey{action.Target, action.URL}
		at, exact := byRecord[key]
		if !exact {
			at = slices.IndexFunc(groups, func(g paneActionGroup) bool {
				return g.target == action.Target && (g.url == "" || action.URL == "")
			})
			if at < 0 {
				at = len(groups)
				groups = append(groups, paneActionGroup{target: action.Target, url: action.URL, state: workfile.AttentionDone})
			}
			byRecord[key] = at
		}
		group := &groups[at]
		if actionRank(action.State) < actionRank(group.state) {
			group.state = action.State
		}
		group.actions = append(group.actions, action)
	}
	for i := range groups {
		slices.SortStableFunc(groups[i].actions, func(a, b workfile.WorkAction) int { return actionRank(a.State) - actionRank(b.State) })
	}
	slices.SortStableFunc(groups, func(a, b paneActionGroup) int {
		if (a.target == ticket.Key) != (b.target == ticket.Key) {
			if a.target == ticket.Key {
				return -1
			}
			return 1
		}
		if p.focusPR != "" && (a.url == p.focusPR) != (b.url == p.focusPR) {
			if a.url == p.focusPR {
				return -1
			}
			return 1
		}
		return actionRank(a.state) - actionRank(b.state)
	})
	return groups
}

func (p workfilePane) prBox(group paneActionGroup, records []workfile.Record, number *int) []string {
	const padding = 4
	width := p.width - padding
	primary, _ := primaryPRAction(group.actions, group.target, group.url)
	_, status, colour := pullRequestStatus(group.record, primary)
	paint := p.t.dim
	switch colour {
	case 1:
		paint = p.t.bad
	case 2:
		paint = p.t.good
	case 3:
		paint = p.t.warn
	}
	id := group.target
	if group.record.ID != "" {
		id = shortRecordID(group.record.ID, records)
	}
	name := p.t.bold(p.v.link(id, group.url))
	tag := paint(status)
	lines := []string{alignPair(name, tag, width)}
	if ansi.StringWidth(name)+ansi.StringWidth(tag)+2 > width {
		lines = append(fit(name, "", "", width), fit(tag, "", "", width)...)
	}
	lines = append(lines, p.actionRows(group, number, width)...)
	if !p.v.hyperlinks() && group.url != "" {
		lines = append(lines, fit(p.t.dim(clean(group.url)), "", "", width)...)
	}
	// A left block starts at the cell edge, unlike a centred box-drawing line.
	border := paint("▎")
	paddingRow := p.panelTheme.onPanel(border, p.width)
	box := []string{paddingRow}
	for _, line := range lines {
		box = append(box, p.panelTheme.onPanel(border+"  "+line, p.width))
	}
	return append(box, paddingRow)
}

func (p workfilePane) actionRows(group paneActionGroup, number *int, width int) []string {
	var lines []string
	for _, action := range group.actions {
		if action.State == workfile.AttentionUnknown && action.Text == "PR mergeable unavailable" &&
			group.state == workfile.NeedsMe && len(action.Destinations) == 0 {
			continue
		}
		prefix := attentionIcon(action.State, p.t.color) + " "
		if action.State == workfile.NeedsMe {
			(*number)++
			prefix = fmt.Sprintf("%d. ", *number)
		}
		text := p.actionText(action, group.record)
		lines = append(lines, fit(p.t.attention(action.State, prefix+text), "", "", width)...)
		if len(action.Destinations) > 1 {
			var names []string
			for _, to := range action.Destinations {
				names = append(names, boardStateTitle(to))
			}
			lines = append(lines, fit(p.t.dim("unblocks → "+strings.Join(names, " · → ")), "  ", "  ", width)...)
		}
	}
	return lines
}

func (p workfilePane) actionText(action workfile.WorkAction, record workfile.Record) string {
	text := strings.TrimSpace(clean(action.Text))
	switch {
	case text == "PR mergeable unavailable":
		return "Cannot tell whether this PR has conflicts"
	case action.Fact == "checks" && text == "Fix failing checks":
		jobs := p.failed[action.Target]
		var names []string
		for _, job := range jobs {
			name := strings.TrimSpace(clean(job.Name))
			if name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			label := "Failing checks"
			if len(names) == 1 {
				label = "Failing check"
			}
			if action.State == workfile.NeedsMe {
				label = "Fix " + strings.ToLower(label)
			}
			if len(names) > 3 {
				names = append(names[:3], fmt.Sprintf("+%d more", len(names)-3))
			}
			return label + " · " + strings.Join(names, " · ")
		}
	case action.State == workfile.Waiting && text == "Waiting for approval":
		if reviewers, _ := record.Facts["review_requests"].([]string); len(reviewers) == 1 && record.Unavailable["review_requests"] == "" {
			return "Waiting for " + clean(reviewers[0]) + " to review"
		}
	}
	return text
}
