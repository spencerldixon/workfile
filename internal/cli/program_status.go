package cli

import (
	"context"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
)

const (
	programWorking = "\x1b]7501;state=working:app=workfile\x1b\\"
	programIdle    = "\x1b]7501;state=idle:app=workfile\x1b\\"
)

func (v view) canReportProgramStatus() bool {
	return v.color && isTerminal(v.out)
}

// startProgramWork reports synchronous provider reads that run outside a TUI.
func (e env) startProgramWork() func() {
	if e.o.json || !e.view.canReportProgramStatus() {
		return func() {}
	}
	io.WriteString(e.view.out, programWorking)
	return sync.OnceFunc(func() { io.WriteString(e.view.out, programIdle) })
}

type syncingModel interface {
	tea.Model
	busy() bool
}

type programStatusMsg struct{ busy bool }

// programStatusModel shares the terminal status lifecycle across sync screens.
// Bubble Tea handles raw output so it stays ordered with the screen renderer.
type programStatusModel struct {
	tea.Model
	busy     func() bool
	working  bool
	reported bool
}

func (m *programStatusModel) Init() tea.Cmd {
	return m.track(m.Model.Init())
}

func (m *programStatusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	return m, m.track(cmd)
}

func (m *programStatusModel) track(cmd tea.Cmd) tea.Cmd {
	busy := m.busy()
	if busy == m.working {
		return cmd
	}
	m.working = busy
	return tea.Sequence(func() tea.Msg { return programStatusMsg{busy} }, cmd)
}

func (m *programStatusModel) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	status, ok := msg.(programStatusMsg)
	if !ok {
		return msg
	}
	if status.busy != m.working || status.busy == m.reported {
		return nil
	}
	m.reported = status.busy
	if status.busy {
		return tea.RawMsg{Msg: programWorking}
	}
	return tea.RawMsg{Msg: programIdle}
}

func runSyncTUI(ctx context.Context, v view, model syncingModel) (tea.Model, error) {
	opts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)}
	if !v.canReportProgramStatus() {
		return tea.NewProgram(model, opts...).Run()
	}
	m := &programStatusModel{Model: model, busy: model.busy}
	_, err := tea.NewProgram(m, append(opts, tea.WithFilter(m.filter))...).Run()
	if m.reported {
		io.WriteString(v.out, programIdle)
	}
	return m.Model, err
}
