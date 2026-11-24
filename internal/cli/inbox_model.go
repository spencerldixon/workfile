package cli

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"
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

	data    *inboxData
	rows    []workfile.InboxRow
	sel     int
	loading bool
	polls   int

	width, height int
	detail        bool
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
	argv   []string
	err    error
}

func newInboxModel(ctx context.Context, e env, s workfile.InboxSettings, st workfile.InboxState) *inboxModel {
	return &inboxModel{
		ctx: ctx, e: e, s: s, st: st,
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
		m.rebuild()
		m.st.Observe(inboxInput(d, m.st), m.rows)
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
		m.st.Agents = append(m.st.Agents, workfile.AgentNote{Row: msg.row.ID, Ticket: msg.row.Ticket, PR: msg.row.PR, Kind: string(msg.row.Kind), Prompt: msg.prompt, At: m.now()})
		m.persist()
		m.say("⚙ started: "+strings.Join(msg.argv, " "), false)
	case tea.KeyPressMsg:
		if m.ticket != nil {
			return m.ticketKey(msg)
		}
		return m.key(msg)
	}
	return nil
}

func (m *inboxModel) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "enter":
		if _, ok := m.current(); ok {
			m.detail = true
		}
	case "esc":
		m.detail, m.detailOffset = false, 0
	case "pgup", "pgdown":
		if m.detail && len(m.rows) > 0 {
			t, width := m.theme(), m.renderWidth()
			room := m.bodyHeight(len(m.footer(t, width)))
			if m.toast != "" {
				room = max(1, room-1)
			}
			pane := m.activityViewport(t, width, room)
			if msg.String() == "pgup" {
				pane.PageUp()
			} else {
				pane.PageDown()
			}
			m.detailOffset = pane.YOffset()
		}
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
		m.done(row.ID)
	case "s":
		m.st.Snoozed[row.ID] = row.Snooze(m.now().Add(m.s.Snooze.Duration))
		m.rebuild()
		m.persist()
	case "o":
		target := row.PRURL
		if target == "" {
			target = row.URL
		}
		if target == "" {
			return nil
		}
		if err := m.open(target); err != nil {
			m.say("Couldn't open the browser", true)
		}
	case "a":
		return m.agent(row)
	}
	return nil
}

// done clears a row until something new happens: its events are seen, and
// what remains is remembered by fingerprint.
func (m *inboxModel) done(id string) {
	at := m.sel
	for _, r := range m.rows {
		if r.ID == id {
			m.markSeen(r)
		}
	}
	m.rebuild()
	for _, r := range m.rows {
		if r.ID == id {
			m.st.Done[id] = r.Fingerprint
		}
	}
	m.rebuild()
	// The next row moves up into the cleared one's place, as in a mail inbox.
	m.sel = max(0, min(at, len(m.rows)-1))
	m.detailOffset = 0
	m.persist()
}

// canAgent reports whether a has something to do on this row: an agent is
// set, the row needs the person, and its prompt has every value it uses.
func (m *inboxModel) canAgent(row workfile.InboxRow) bool {
	if m.s.Agent == "" || !row.Action() || row.Kind == workfile.RowCannotCheck {
		return false
	}
	_, _, err := agentCommand(m.s, row)
	return err == nil
}

func (m *inboxModel) agent(row workfile.InboxRow) tea.Cmd {
	if m.s.Agent == "" || !row.Action() || row.Kind == workfile.RowCannotCheck {
		return nil
	}
	prompt, argv, err := agentCommand(m.s, row)
	if err != nil {
		m.say("Couldn't start agent: "+agentReason(err), true)
		return nil
	}
	ctx, start := m.ctx, m.start
	return func() tea.Msg {
		return agentDoneMsg{row: row, prompt: prompt, argv: argv, err: start(ctx, argv)}
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
	values["prompt"] = filled[0]
	words, err := runner.Split(s.Agent)
	if err != nil {
		return "", nil, err
	}
	argv, err := runner.Fill(words, values)
	return filled[0], argv, err
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
}

func (m *inboxModel) markSeen(row workfile.InboxRow) {
	for _, e := range row.Events {
		m.st.Seen[e.Key()] = m.now()
	}
}

func (m *inboxModel) rebuild() {
	if m.data == nil {
		return
	}
	id := ""
	if row, ok := m.current(); ok {
		id = row.ID
	}
	m.rows = inboxRows(*m.data, m.st)
	m.sel = min(m.sel, max(0, len(m.rows)-1))
	for i, r := range m.rows {
		if r.ID == id {
			m.sel = i
			return
		}
	}
	m.detailOffset = 0
	if len(m.rows) == 0 {
		m.detail = false
	}
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
			keys = append(keys, [2]string{"t", "ticket"})
		}
		keys = append(keys, [2]string{"e", "resolve"}, [2]string{"s", "snooze"})
		if m.canAgent(row) {
			keys = append(keys, [2]string{"a", "agent"})
		}
		if target := firstOf(row.PR, row.Ticket); (row.PRURL != "" || row.URL != "") && target != "" {
			keys = append(keys, [2]string{"o", "open " + target})
		}
	}
	keys = append(keys, [2]string{"[/]", "view"})
	return append(keys, [2]string{"r", "refresh"}, [2]string{"q", "quit"})
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
