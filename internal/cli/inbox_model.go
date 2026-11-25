package cli

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	runner "workfile/internal/run"
	"workfile/internal/workfile"
)

// inboxModel is the Tracker inbox screen. Clearing is local: it changes only
// inbox.json, never a ticket or PR.
type inboxModel struct {
	ctx context.Context
	e   env
	s   workfile.InboxSettings
	st  workfile.InboxState

	data         *inboxData
	rows         []workfile.InboxRow
	scope        workfile.InboxScope
	prepared     *inboxAgent
	prepareErr   error
	agentPreview *inboxAgent
	sel          int
	loading      bool
	polls        int

	width, height int
	detailOffset  int
	ticket        *inboxTicket
	workflow      workflowPreview
	toast         string
	toastBad      bool
	saveWarned    bool

	now   func() time.Time
	start func(context.Context, []string) error
	open  func(string) error
	save  func(workfile.InboxState) error
}

type agentDoneMsg struct {
	row    workfile.InboxRow
	prompt string
	err    error
}

func newInboxModel(ctx context.Context, e env, s workfile.InboxSettings, st workfile.InboxState) *inboxModel {
	return &inboxModel{
		ctx: ctx, e: e, s: s, st: st, scope: s.DefaultScope,
		now:   time.Now,
		start: runner.Start,
		open:  func(target string) error { return openBrowser(ctx, target) },
		save:  func(s workfile.InboxState) error { return workfile.WriteInboxState(e.w, s) },
	}
}

func (m *inboxModel) busy() bool {
	return m.loading || m.ticket != nil && m.ticket.read.pending()
}

func (m *inboxModel) pollMode() pollMode {
	switch {
	case m.s.FullEvery > 0 && m.polls%m.s.FullEvery == 0:
		return pollFull
	case m.s.RefreshEvery > 0 && m.polls%m.s.RefreshEvery == 0:
		return pollRefresh
	}
	return pollQuick
}

// load reads a poll off the Update loop. It hands the loader its own copy of
// the cursors, so nothing is shared while it runs.
func (m *inboxModel) load(mode pollMode) tea.Cmd {
	return m.loadWithListing(mode, nil)
}

func (m *inboxModel) loadWithListing(mode pollMode, listing []workfile.Listing) tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	var prev *inboxData
	if m.data != nil {
		copied := *m.data
		prev = &copied
	}
	st := workfile.InboxState{Cursors: maps.Clone(m.st.Cursors), Checks: maps.Clone(m.st.Checks)}
	ctx, e, now := m.ctx, m.e, m.now()
	return func() tea.Msg { return inboxLoadedMsg(loadInboxWithListing(ctx, e, st, prev, now, mode, listing)) }
}

func (m *inboxModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		cmd := m.loadTicketWorkflow()
		m.fillTicket()
		return cmd
	case ticketDetailMsg:
		if t := m.ticket; t != nil && t.read == msg.read {
			t.read.detail, t.read.detailErr = &msg.detail, msg.err
			m.fillTicket()
		}
	case inboxTicketPolicyMsg:
		if t := m.ticket; t != nil && t.read == msg.read {
			t.read.policyErr = msg.err
			if msg.err == nil {
				t.read.policy, t.actions = &msg.assessment, msg.actions
			}
			cmd := m.loadTicketWorkflow()
			m.fillTicket()
			return cmd
		}
	case workflowPictureMsg:
		cmd := m.workflow.receive(msg)
		m.fillTicket()
		return cmd
	case inboxTicketOpenedMsg:
		if t := m.ticket; t != nil && t == msg.ticket && msg.err != nil {
			t.notice = "Couldn't open the browser"
			m.fillTicket()
		}
	case inboxLoadedMsg:
		m.loading = false
		d := inboxData(msg)
		m.data = &d
		applyPoll(&m.st, d)
		m.st.Observe(inboxInput(d, m.st), nil)
		rows := inboxRows(d, m.st)
		m.st.Observe(inboxInput(d, m.st), rows)
		m.setRows(rows)
		m.st.Prune(d.at, d.tracksActivity)
		m.persist()
	case inboxTickMsg:
		m.polls++
		return tea.Batch(m.load(m.pollMode()), pollEvery(m.s.Poll.Duration))
	case agentDoneMsg:
		if msg.err != nil {
			m.say("Couldn't start agent: "+agentReason(msg.err), true)
			return nil
		}
		m.st.Agents = append(m.st.Agents, workfile.AgentNote{Row: msg.row.StateKey(), Ticket: msg.row.Ticket, PR: msg.row.PR, Kind: string(msg.row.Kind), Prompt: msg.prompt, At: m.now()})
		m.persist()
		m.say("Agent started", false)
	case tea.KeyPressMsg:
		if m.ticket != nil {
			return m.ticketKey(msg)
		}
		return m.key(msg)
	}
	return nil
}

func (m *inboxModel) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.agentPreview != nil {
		switch msg.String() {
		case "esc":
			m.agentPreview = nil
		case "enter":
			prepared := m.agentPreview
			m.agentPreview = nil
			return m.launchAgent(prepared)
		case "up", "k":
			m.detailOffset = max(0, m.detailOffset-1)
		case "down", "j":
			m.detailOffset++
		}
		return nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "r":
		return m.load(pollFull)
	}
	row, ok := m.current()
	if !ok {
		return nil
	}
	switch msg.String() {
	case "t":
		if row.Ticket != "" {
			return m.openTicket(row)
		}
	case "e":
		m.done(row)
	case "z":
		m.st.Snoozed[row.StateKey()] = row.Snooze(m.now().Add(m.s.Snooze.Duration))
		m.rebuild()
		m.persist()
	case "o":
		target := inboxPRTarget(row)
		if target == "" {
			return nil
		}
		if err := m.open(target); err != nil {
			m.say("Couldn't open the browser", true)
		}
	case "a":
		if m.prepareErr != nil {
			m.say("Couldn't prepare agent: "+agentReason(m.prepareErr), true)
		}
		m.agentPreview = m.prepared
		if m.agentPreview != nil {
			m.detailOffset = 0
		}
	}
	return nil
}

// done clears a row until something new happens: its events are seen, and
// what remains is remembered by fingerprint.
func (m *inboxModel) done(row workfile.InboxRow) {
	m.markSeen(row)
	m.st.Done[row.StateKey()] = row.Fingerprint
	if m.data != nil {
		rows := inboxRows(*m.data, m.st)
		rows = slices.DeleteFunc(rows, func(next workfile.InboxRow) bool {
			if next.StateKey() != row.StateKey() {
				return false
			}
			// Consuming the activity can leave the same current-state alert.
			m.st.Done[row.StateKey()] = next.Fingerprint
			return true
		})
		m.setRows(rows)
	}
	m.persist()
}

type inboxAgent struct {
	row    workfile.InboxRow
	prompt string
	argv   []string
}

func (m *inboxModel) refreshAgent() {
	m.prepared, m.prepareErr = nil, nil
	row, ok := m.current()
	if !ok || m.s.Agent == "" || !row.Action() || row.Kind == workfile.RowCannotCheck {
		return
	}
	prompt, argv, err := agentCommand(m.s, row)
	if err != nil {
		m.prepareErr = err
		return
	}
	m.prepared = &inboxAgent{row: row, prompt: prompt, argv: argv}
}

func (m *inboxModel) launchAgent(prepared *inboxAgent) tea.Cmd {
	ctx, start := m.ctx, m.start
	return func() tea.Msg {
		return agentDoneMsg{row: prepared.row, prompt: prepared.prompt, err: start(ctx, prepared.argv)}
	}
}

// agentCommand fills the row's prompt, then the agent command, word by word.
func agentCommand(s workfile.InboxSettings, row workfile.InboxRow) (string, []string, error) {
	template := s.Prompts[string(row.Kind)]
	if template == "" {
		template = s.Prompts["default"]
	}
	values := map[string]string{
		"key": firstOf(row.Ticket, row.PR), "pr": row.PR, "url": firstOf(row.PRURL, row.URL), "stage": row.Stage,
		"to": row.To, "who": row.Who, "fix": row.Fix,
	}
	if row.Count > 0 {
		values["count"] = strconv.Itoa(row.Count)
	}
	filled, err := runner.Fill([]string{template}, values)
	if err != nil {
		return "", nil, err
	}
	prompt := filled[0] + "\n\nWorkfile context"
	for _, detail := range [][2]string{
		{"Title", row.Title}, {"Ticket", row.Ticket}, {"Ticket URL", row.URL},
		{"PR", row.PR}, {"PR URL", row.PRURL}, {"Thread", row.ThreadURL}, {"Checks", row.ChecksURL},
		{"Current stage", row.Stage}, {"Next stage", row.To}, {"Signal", string(row.Kind)},
		{"Next action", row.Text}, {"Why now", row.ReadyReason}, {"New comments", values["count"]},
	} {
		if value := clean(detail[1]); value != "" {
			prompt += "\n" + detail[0] + ": " + value
		}
	}
	values["prompt"] = prompt
	words, err := runner.Split(s.Agent)
	if err != nil {
		return "", nil, err
	}
	argv, err := runner.Fill(words, values)
	return prompt, argv, err
}

func agentReason(err error) string {
	var missing *runner.MissingError
	if errors.As(err, &missing) {
		return "Needs {" + missing.Name + "}, and this row has none"
	}
	var exit *runner.ExitError
	if errors.As(err, &exit) {
		return exit.Error()
	}
	return clean(err.Error())
}

func (m *inboxModel) move(by int) {
	if len(m.rows) == 0 {
		return
	}
	m.sel = max(0, min(len(m.rows)-1, m.sel+by))
	m.detailOffset = 0
	m.refreshAgent()
}

func (m *inboxModel) markSeen(row workfile.InboxRow) {
	for _, e := range row.Events {
		m.st.Seen[e.Key()] = m.now()
	}
}

func (m *inboxModel) rebuild() {
	if m.data != nil {
		m.setRows(inboxRows(*m.data, m.st))
	}
}

func (m *inboxModel) setRows(rows []workfile.InboxRow) {
	defer m.refreshAgent()
	id := ""
	if row, ok := m.current(); ok {
		id = row.StateKey()
	}
	m.rows = m.s.ScopedRows(rows, m.scope)
	m.sel = min(m.sel, max(0, len(m.rows)-1))
	for i, r := range m.rows {
		if r.StateKey() == id {
			m.sel = i
			return
		}
	}
	m.detailOffset = 0
}

func (m *inboxModel) current() (workfile.InboxRow, bool) {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return workfile.InboxRow{}, false
	}
	return m.rows[m.sel], true
}

func (m *inboxModel) selected() workfile.InboxRow {
	row, _ := m.current()
	return row
}

func (m *inboxModel) persist() {
	if err := m.save(m.st); err != nil && !m.saveWarned {
		m.saveWarned = true
		m.say("Couldn't save inbox state", true)
	}
}

func (m *inboxModel) say(text string, bad bool) { m.toast, m.toastBad = text, bad }

// footerKeys are the keys that do something on the selected row.
func (m *inboxModel) footerKeys() [][2]string {
	keys := [][2]string{{"j/k", "move"}}
	row, ok := m.current()
	if ok {
		if row.Ticket != "" {
			keys = append(keys, [2]string{"t", "open ticket"})
		}
		keys = append(keys, [2]string{"e", "resolve"}, [2]string{"z", "snooze"})
		if m.prepared != nil {
			keys = append(keys, [2]string{"a", "agent"})
		}
		if inboxPRTarget(row) != "" {
			keys = append(keys, [2]string{"o", "open PR"})
		}
	}
	scope := "all signals"
	if m.scope == workfile.InboxScopeAttention {
		scope = "needs attention"
	}
	keys = append(keys, [2]string{"s", "scope: " + scope}, [2]string{"[/]", "view"})
	return append(keys, [2]string{"r", "refresh"}, [2]string{"q", "quit"})
}

func inboxPRTarget(row workfile.InboxRow) string {
	for _, url := range []string{row.ThreadURL, row.PRURL} {
		if trackerWebURL(url) {
			return url
		}
	}
	return ""
}

func inboxOpenTarget(row workfile.InboxRow) (url, label string) {
	if row.Kind == workfile.RowChecksFailed && trackerWebURL(row.ChecksURL) {
		return row.ChecksURL, "checks " + clean(row.PR)
	}
	if trackerWebURL(row.PRURL) {
		return row.PRURL, "open " + clean(row.PR)
	}
	if trackerWebURL(row.URL) {
		return row.URL, "open " + clean(row.Ticket)
	}
	return "", ""
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
