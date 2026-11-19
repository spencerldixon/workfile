package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type programWorkDone struct{}

type syncStatusTestModel struct{ working bool }

func (m *syncStatusTestModel) Init() tea.Cmd {
	m.working = true
	return func() tea.Msg { return programWorkDone{} }
}

func (m *syncStatusTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg {
	case programWorkDone{}:
		m.working = false
	case tea.RawMsg{Msg: programIdle}:
		return m, tea.Quit
	}
	return m, nil
}

func (m *syncStatusTestModel) View() tea.View { return tea.NewView("") }

func TestProgramStatusReportsWorkingThenIdleThroughBubbleTea(t *testing.T) {
	var output bytes.Buffer
	child := &syncStatusTestModel{}
	model := &programStatusModel{Model: child, busy: func() bool { return child.working }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	program := tea.NewProgram(model, tea.WithFilter(model.filter), tea.WithContext(ctx),
		tea.WithInput(nil), tea.WithOutput(&output), tea.WithWindowSize(80, 24))
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	working := "\x1b]7501;state=working:app=workfile\x1b\\"
	idle := "\x1b]7501;state=idle:app=workfile\x1b\\"
	got := output.String()
	if strings.Count(got, working) != 1 || strings.Count(got, idle) != 1 || strings.Index(got, working) > strings.Index(got, idle) {
		t.Fatalf("expected one working report followed by one idle report, got %q", got)
	}
}

func TestProgramStatusIgnoresStaleAndDuplicateReports(t *testing.T) {
	m := programStatusModel{working: true, reported: true}
	if m.filter(nil, programStatusMsg{busy: false}) != nil || m.filter(nil, programStatusMsg{busy: true}) != nil {
		t.Fatal("a stale or duplicate status would reach the terminal")
	}
}

func TestProgramStatusLeavesRedirectedOutputPlain(t *testing.T) {
	var out bytes.Buffer
	e := env{view: view{out: &out, color: true}}
	e.startProgramWork()()
	if out.Len() != 0 {
		t.Fatalf("program status escaped into redirected output: %q", out.String())
	}
}
