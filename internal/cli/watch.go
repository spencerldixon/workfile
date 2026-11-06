package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
	"workfile/internal/automation"
	"workfile/internal/workfile"
)

var watchCommand = command{flags: []string{"dir", "once", "interval", "dry-run"}, args: watchArgs, run: watch}

func watchArgs(o options) error {
	if len(o.keys) > 0 {
		return errors.New("watch takes no positional arguments")
	}
	if o.interval < time.Minute {
		return errors.New("--interval must be at least 1m")
	}
	return nil
}

// watch checks the person's work on a timer and fires their automations. In
// a terminal it is a Bubble Tea screen that can ask before running; piped, or
// with --once, it prints plain blocks and never asks.
func watch(ctx context.Context, e env) (int, error) {
	v, w, client, o := e.view, e.w, e.client, e.o
	s, err := loadAutomations(ctx, w, client, true)
	if err != nil {
		return 2, err
	}
	state, err := s.store.State()
	if err != nil {
		return 2, err
	}
	x := &watcher{w: w, client: client, s: s, dryRun: o.dryRun}
	wv := newWatchView(v, x, o.interval, len(state.Facts) == 0)
	if len(s.set.Entries()) == 0 {
		fmt.Fprint(v.out, newAutomationsModel(v, w, s, state).render())
		return 0, nil
	}
	release, err := s.store.Lock()
	if err != nil {
		return 2, err
	}
	defer release()

	interactive := !o.once && isTerminal(os.Stdin) && isTerminal(v.out)
	if !interactive {
		fmt.Fprint(v.out, wv.header())
		for {
			r := x.check(ctx, false)
			fmt.Fprint(v.out, wv.checkPanel(r))
			for _, e := range r.run {
				fmt.Fprint(v.out, wv.resultPanel(x.run(ctx, e)))
			}
			if o.once {
				if r.err != nil {
					return 2, nil
				}
				return 0, nil
			}
			select {
			case <-ctx.Done():
				return 0, nil
			case <-time.After(o.interval):
			}
		}
	}
	runCtx, stop := context.WithCancel(ctx)
	m := newWatchModel(runCtx, wv)
	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)).Run()
	// Stopping watch stops a running action, and waits for it to be recorded.
	stop()
	x.running.Wait()
	if fm, ok := final.(*watchModel); ok {
		// Questions nobody answered wait for the next wf watch.
		x.requeue(append(fm.questions, fm.queue...))
	}
	if err != nil && ctx.Err() == nil {
		return 2, err
	}
	return 0, nil
}

func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// watcher does the work of each check, outside the screen, so it can run as
// a tea.Cmd and in the plain loop alike.
type watcher struct {
	w       *workfile.Workspace
	client  source
	s       *automationSetup
	dryRun  bool
	mu      sync.Mutex     // guards the saved state between a check and a run
	running sync.WaitGroup // actions still running
}

// checkResult is what one check found.
type checkResult struct {
	at       time.Time
	events   []automation.Event // fired or finished in this check
	run      []automation.Event // to run now: auto, or ask when someone can answer
	waiting  int                // questions waiting for a terminal
	warnings []string
	err      error
}

// check reads every ticket in the search, works out what changed since the
// last check, and returns what to run now.
func (x *watcher) check(ctx context.Context, ask bool) checkResult {
	r := checkResult{at: time.Now()}
	tickets, err := x.client.Tickets(ctx, nil, 0)
	if err == nil {
		tickets, r.warnings, err = x.client.Records(ctx, tickets)
	}
	if err != nil {
		// Nothing could be read: nothing changes and nothing fires.
		r.err = err
		return r
	}
	all := make([]workfile.Assessment, len(tickets))
	for i, t := range tickets {
		all[i] = x.w.Assess(t)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	st, err := x.s.store.State()
	if err != nil {
		r.err = err
		return r
	}
	report := func(a workfile.Assessment) json.RawMessage {
		data, _ := json.Marshal(ticketReport(a))
		return data
	}
	for _, e := range automation.Fire(x.w, x.s.set, st, all, x.s.who, r.at, report) {
		if x.dryRun && e.Outcome == "" {
			e.Outcome, e.Detail = automation.Observed, "dry run"
		}
		if e.Outcome != "" {
			st.Record(e)
			r.events = append(r.events, e)
			continue
		}
		st.Queue = append(st.Queue, e)
	}
	take, expired := automation.Next(st, r.at, ask)
	for _, e := range expired {
		st.Record(e)
		r.events = append(r.events, e)
	}
	for _, e := range take {
		a, ok := x.automation(e)
		if !ok {
			e.Outcome, e.Detail = automation.Skipped, "the automation was removed or cannot run here now"
		} else if _, err := x.s.runner.Command(a, e); err != nil {
			e.Outcome, e.Detail = automation.Failed, err.Error()
		}
		if e.Outcome != "" {
			st.Record(e)
			r.events = append(r.events, e)
			continue
		}
		r.events = append(r.events, e)
		r.run = append(r.run, e)
	}
	for _, e := range st.Queue {
		if e.At.Equal(r.at) {
			r.events = append(r.events, e) // fired now, waiting its turn
		}
		if e.Mode == workfile.ModeAsk {
			r.waiting++
		}
	}
	if err := x.s.store.SaveState(st); err != nil {
		r.err = err
	}
	x.s.store.PruneLogs(r.at)
	return r
}

// automation finds the event's automation, if it can still run.
func (x *watcher) automation(e automation.Event) (workfile.Automation, bool) {
	for _, entry := range x.s.set.Entries() {
		if entry.Automation.ID == e.Automation && entry.File.Private == e.Private && entry.Blocked == "" {
			return entry.Automation, true
		}
	}
	return workfile.Automation{}, false
}

// command is exactly what the event would run, for a question.
func (x *watcher) command(e automation.Event) (automation.Command, error) {
	a, ok := x.automation(e)
	if !ok {
		return automation.Command{}, fmt.Errorf("%s cannot run here now", e.Automation)
	}
	return x.s.runner.Command(a, e)
}

// run starts an event's action and records what happened.
func (x *watcher) run(ctx context.Context, e automation.Event) automation.Event {
	x.running.Add(1)
	defer x.running.Done()
	if a, ok := x.automation(e); ok {
		e = x.s.runner.Run(ctx, a, e)
	} else {
		e.Outcome, e.Detail = automation.Skipped, "the automation was removed or cannot run here now"
	}
	x.record(e)
	return e
}

func (x *watcher) record(e automation.Event) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if st, err := x.s.store.State(); err == nil {
		st.Record(e)
		x.s.store.SaveState(st)
	}
}

func (x *watcher) requeue(events []automation.Event) {
	if len(events) == 0 {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if st, err := x.s.store.State(); err == nil {
		st.Queue = append(events, st.Queue...)
		x.s.store.SaveState(st)
	}
}

// watchView draws the blocks `wf watch` prints: one per check with something
// to report, and one per finished action. The ticket comes first, then the
// automation and what happened.
type watchView struct {
	screen
	x        *watcher
	interval time.Duration
	first    bool // nothing saved yet: the first check is a starting point
}

func newWatchView(v view, x *watcher, interval time.Duration, first bool) watchView {
	return watchView{screen: newScreen(v), x: x, interval: interval, first: first}
}

func (wv watchView) header() string {
	x := wv.x
	name := x.w.Policy.Name
	if name == "" {
		name = "this workspace"
	}
	parts := []string{"every " + duration(wv.interval)}
	if x.s.set.Person != "" {
		parts = append(parts, "for "+x.w.DisplayName(x.s.set.Person))
	}
	if x.dryRun {
		parts = append(parts, "dry run: nothing runs")
	}
	lines := []string{wv.t.bold("Watching "+name) + wv.t.dim(" · "+strings.Join(parts, " · "))}
	entries := x.s.set.Entries()
	ready := 0
	for _, e := range entries {
		if e.Blocked == "" {
			ready++
		}
	}
	lines = append(lines, wv.wrap(fmt.Sprintf("%s · %d can run", quantity(len(entries), "automation", "automations"), ready), wv.t.dim)...)
	if wv.first {
		lines = append(lines, "")
		lines = append(lines, wv.wrap("The first check records a starting point. Nothing fires for what is already true.", plainText)...)
	}
	m := newAutomationsModel(wv.v, x.w, x.s, &automation.State{})
	m.screen = wv.screen
	return panels(wv.t, wv.panelWidth(), [][]string{lines, m.problemPanel()})
}

// checkPanel is one check's block, or "" when there is nothing to report.
func (wv watchView) checkPanel(r checkResult) string {
	clock := r.at.Local().Format("15:04")
	if r.err != nil {
		lines := []string{wv.mark(clock+" · could not check", false)}
		lines = append(lines, wv.wrap(r.err.Error(), wv.t.bad)...)
		lines = append(lines, wv.wrap("Nothing fires until Workfile can read again.", wv.t.dim)...)
		return panels(wv.t, wv.panelWidth(), [][]string{lines})
	}
	if len(r.events) == 0 && len(r.warnings) == 0 {
		return ""
	}
	fired := 0
	ok := true
	for _, e := range r.events {
		if e.Outcome != automation.Skipped || !strings.Contains(e.Detail, "nobody answered") {
			fired++
		}
		if e.Outcome == automation.Failed {
			ok = false
		}
	}
	title := clock + " · " + quantity(fired, "automation fired", "automations fired")
	if fired == 0 {
		title = clock + " · nothing fired"
	}
	// Red when an action failed, green when something fired, dim otherwise.
	lines := []string{wv.t.dim(title)}
	if !ok || fired > 0 {
		lines[0] = wv.mark(title, ok)
	}
	for _, warning := range r.warnings {
		lines = append(lines, wv.wrap(warning, wv.t.dim)...)
	}
	last := ""
	for _, e := range r.events {
		if e.Ticket != last {
			lines = append(lines, "")
			lines = append(lines, wv.ticketLine(e)...)
			last = e.Ticket
		}
		lines = append(lines, wv.eventLines(e, slices.ContainsFunc(r.run, func(run automation.Event) bool {
			return run.Automation == e.Automation && run.Subject() == e.Subject() && run.At.Equal(e.At)
		}))...)
	}
	if r.waiting > 0 {
		lines = append(lines, "")
		lines = append(lines, wv.wrap(quantity(r.waiting, "question waits", "questions wait")+" for an answer. Run wf watch in a terminal to answer.", wv.t.dim)...)
	}
	return panels(wv.t, wv.panelWidth(), [][]string{lines})
}

func (wv watchView) ticketLine(e automation.Event) []string {
	key := wv.v.link(e.Ticket, e.URL)
	title := clean(e.Title)
	room := wv.width() - len(e.Ticket) - 2
	if room >= 8 {
		title = truncate(title, room)
		return []string{wv.t.bold(key) + "  " + title}
	}
	return append([]string{wv.t.bold(key)}, wv.wrap(title, plainText)...)
}

// eventLines say which automation fired, on what, and what became of it.
func (wv watchView) eventLines(e automation.Event, running bool) []string {
	what := e.Automation + " · " + e.Trigger
	if e.Record != "" {
		what += " · " + e.Record
	}
	lines := wv.indented(what, "  ", plainText)
	var status string
	style := wv.t.dim
	switch e.Outcome {
	case automation.Observed:
		status = "· observe: would " + strings.TrimPrefix(wv.actionFor(e), "runs ")
		if e.Detail == "dry run" {
			status = "· dry run: would " + strings.TrimPrefix(wv.actionFor(e), "runs ")
		}
	case automation.Skipped:
		status = "· skipped: " + e.Detail
	case automation.Failed:
		status, style = "✗ failed: "+e.Detail, wv.t.bad
	case "":
		status = "· queued"
		if e.Mode == workfile.ModeAsk {
			status = "? waiting for your answer"
		}
		if running {
			status = "· running"
			if e.Mode == workfile.ModeAsk {
				status = "? asking you"
			}
		}
	}
	return append(lines, wv.indented(status, "  ", style)...)
}

func (wv watchView) actionFor(e automation.Event) string {
	for _, entry := range wv.x.s.set.Entries() {
		if entry.Automation.ID == e.Automation {
			if entry.Automation.Agent != "" {
				return "ask your agent"
			}
			if c, err := wv.x.s.runner.Command(entry.Automation, e); err == nil {
				return "run " + strings.Join(automation.Quote(c.Args), " ")
			}
			return actionText(entry.Automation)
		}
	}
	return "run its action"
}

// resultPanel is the block for an action that finished.
func (wv watchView) resultPanel(e automation.Event) string {
	clock := time.Now().Local().Format("15:04")
	ok := e.Outcome == automation.Finished
	title := clock + " · " + e.Automation + " " + e.Outcome
	if e.Outcome == automation.Declined {
		ok = true
		title = clock + " · " + e.Automation + " skipped"
	}
	lines := []string{wv.mark(title, ok)}
	if e.Outcome == automation.Declined {
		lines[0] = wv.t.dim(title)
	}
	lines = append(lines, wv.ticketLine(e)...)
	detail := ""
	switch e.Outcome {
	case automation.Finished:
		detail = "finished in " + duration(e.Duration)
	case automation.Declined:
		detail = "you chose not to run it"
	default:
		detail = e.Detail
	}
	if e.Log != "" {
		detail += " · log: wf automations --log " + e.Automation
	}
	style := wv.t.dim
	if e.Outcome == automation.Failed {
		style = wv.t.bad
	}
	lines = append(lines, wv.indented(detail, "  ", style)...)
	return panels(wv.t, wv.panelWidth(), [][]string{lines})
}

// questionLines show exactly what would run, and ask.
func (wv watchView) questionLines(e automation.Event) []string {
	lines := []string{wv.t.bold("? Run " + e.Automation + "?")}
	lines = append(lines, wv.ticketLine(e)...)
	what := e.Trigger
	if e.Record != "" {
		what += " · " + e.Record
	}
	lines = append(lines, wv.indented(what, "  ", wv.t.dim)...)
	lines = append(lines, "")
	c, err := wv.x.command(e)
	switch {
	case err != nil:
		lines = append(lines, wv.wrap(err.Error(), wv.t.bad)...)
	case c.Prompt != "":
		lines = append(lines, wv.wrap("Your agent gets this prompt:", wv.t.dim)...)
		// Paragraphs are kept; single line breaks are the file's, so they wrap.
		for i, paragraph := range strings.Split(c.Prompt, "\n\n") {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, wv.indented(paragraph, "  ", plainText)...)
		}
		lines = append(lines, wv.wrap("with: "+strings.Join(automation.Quote(c.Args[:len(c.Args)-1]), " ")+" · in "+home(c.Dir), wv.t.dim)...)
	default:
		lines = append(lines, wv.indented(strings.Join(automation.Quote(c.Args), " "), "  ", plainText)...)
		lines = append(lines, wv.wrap("in "+home(c.Dir), wv.t.dim)...)
	}
	lines = append(lines, "")
	return append(lines, wv.t.good("y")+" run  ·  "+wv.t.bad("n")+" skip  ·  "+wv.t.dim("q stop watching"))
}

func truncate(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// watchModel is the `wf watch` screen. Blocks for checks and finished actions
// are printed above it and stay in scrollback; the screen itself is one line
// of status, or the question being asked.
type watchModel struct {
	wv  watchView
	ctx context.Context

	spin      spinner.Model
	checking  bool
	running   *automation.Event
	queue     []automation.Event // actions waiting to run, one at a time
	questions []automation.Event // asks waiting for y or n
	next, now time.Time
	printed   []string // every block printed, for tests
}

type checkDoneMsg checkResult
type ranMsg automation.Event
type clockMsg time.Time

func newWatchModel(ctx context.Context, wv watchView) *watchModel {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &watchModel{wv: wv, ctx: ctx, spin: sp, checking: true, now: time.Now()}
}

func (m *watchModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.print(m.wv.header()), m.checkCmd(), m.clock()}
	if m.wv.t.color {
		cmds = append(cmds, m.spin.Tick)
	}
	return tea.Batch(cmds...)
}

func (m *watchModel) print(block string) tea.Cmd {
	if block == "" {
		return nil
	}
	m.printed = append(m.printed, block)
	return tea.Println(strings.TrimSuffix(block, "\n"))
}

func (m *watchModel) checkCmd() tea.Cmd {
	return func() tea.Msg { return checkDoneMsg(m.wv.x.check(m.ctx, true)) }
}

func (m *watchModel) clock() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return clockMsg(t) })
}

func (m *watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wv.v.width = max(20, min(msg.Width, 140))
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case clockMsg:
		m.now = time.Time(msg)
		cmds := []tea.Cmd{m.clock()}
		if !m.checking && !m.next.IsZero() && !m.now.Before(m.next) {
			m.checking = true
			cmds = append(cmds, m.checkCmd())
		}
		return m, tea.Batch(cmds...)
	case checkDoneMsg:
		r := checkResult(msg)
		m.checking = false
		m.next = r.at.Add(m.wv.interval)
		for _, e := range r.run {
			if e.Mode == workfile.ModeAsk {
				m.questions = append(m.questions, e)
			} else {
				m.queue = append(m.queue, e)
			}
		}
		return m, tea.Batch(m.print(m.wv.checkPanel(r)), m.startNext())
	case ranMsg:
		m.running = nil
		return m, tea.Batch(m.print(m.wv.resultPanel(automation.Event(msg))), m.startNext())
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *watchModel) key(key string) tea.Cmd {
	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	}
	if len(m.questions) == 0 {
		return nil
	}
	e := m.questions[0]
	switch key {
	case "y":
		m.questions = m.questions[1:]
		m.queue = append(m.queue, e)
		return m.startNext()
	case "n", "esc":
		m.questions = m.questions[1:]
		e.Outcome, e.Detail = automation.Declined, "you chose not to run it"
		m.wv.x.record(e)
		return m.print(m.wv.resultPanel(e))
	}
	return nil
}

// startNext runs the next waiting action. Actions run one at a time.
func (m *watchModel) startNext() tea.Cmd {
	if m.running != nil || len(m.queue) == 0 {
		return nil
	}
	e := m.queue[0]
	m.queue = m.queue[1:]
	m.running = &e
	return func() tea.Msg { return ranMsg(m.wv.x.run(m.ctx, e)) }
}

func (m *watchModel) View() tea.View {
	return tea.NewView(m.render())
}

func (m *watchModel) render() string {
	var lines []string
	if len(m.questions) > 0 {
		lines = m.wv.questionLines(m.questions[0])
		if more := len(m.questions) - 1; more > 0 {
			lines = append(lines, m.wv.wrap(quantity(more, "more question waits", "more questions wait")+" after this one.", m.wv.t.dim)...)
		}
		return panels(m.wv.t, m.wv.panelWidth(), [][]string{lines})
	}
	status := m.status()
	return ticketIndent + strings.Join(status, "\n"+ticketIndent) + "\n"
}

// status is one line saying what the watch is doing now.
func (m *watchModel) status() []string {
	busy := func(text string) []string {
		mark := "…"
		if m.wv.t.color {
			mark = m.spin.View()
		}
		return m.wv.wrap(mark+" "+text, plainText)
	}
	switch {
	case m.running != nil:
		return busy("Running " + m.running.Automation + " for " + m.running.Subject())
	case m.checking:
		return busy("Checking your work")
	}
	wait := m.next.Sub(m.now).Round(time.Second)
	if wait < 0 {
		wait = 0
	}
	return m.wv.wrap("Next check in "+duration(wait)+" · q to stop", m.wv.t.dim)
}
