package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

var proveCommand = command{
	flags: []string{"dir", "json"},
	args: func(o options) error {
		if len(o.keys) > 0 {
			return errors.New("prove takes no positional arguments; it checks the policy, not tickets")
		}
		return nil
	},
	run: prove,
}

// prove checks the policy itself, not tickets. It answers: which gates is a
// ticket in each stage sure to have passed, and do the invariants in
// policy.yml hold? It reads no provider, so it needs no credentials and runs
// in CI on a policy change. Exit 0 every invariant holds, 1 one fails.
func prove(_ context.Context, e env) (int, error) {
	v, w, o := e.view, e.w, e.o
	proofs := w.Policy.Prove()
	code := 0
	for _, proof := range proofs {
		if !proof.Holds {
			code = 1
		}
	}
	if o.json {
		return code, writeProof(v.out, w, proofs, code)
	}
	fmt.Fprint(v.out, newProveModel(v, w, proofs).render())
	return code, nil
}

type stageGuarantee struct {
	Stage string   `json:"stage"`
	Gates []string `json:"gates"`
}

type proveReport struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	PolicyHash    string           `json:"policy_hash"`
	ExitCode      int              `json:"exit_code"`
	Invariants    []workfile.Proof `json:"invariants"`
	Guarantees    []stageGuarantee `json:"guarantees"`
}

// writeProof lists guarantees in pipeline order, so array order is meaningful
// here as in the other reports.
func writeProof(out io.Writer, w *workfile.Workspace, proofs []workfile.Proof, code int) error {
	report := proveReport{SchemaVersion: 1, Command: "prove", PolicyHash: w.PolicyHash(), ExitCode: code, Invariants: append([]workfile.Proof{}, proofs...)}
	guarantees := w.Policy.Guarantees()
	for _, stage := range w.Policy.States {
		report.Guarantees = append(report.Guarantees, stageGuarantee{stage, append([]string{}, guarantees[stage]...)})
	}
	return encodeCleanJSON(out, report)
}

// proveModel draws the answer to `wf prove`: a summary, each invariant with
// the path that breaks it, then what each stage guarantees. It is drawn once
// and printed.
type proveModel struct {
	v      view
	t      theme
	w      *workfile.Workspace
	proofs []workfile.Proof
}

func newProveModel(v view, w *workfile.Workspace, proofs []workfile.Proof) *proveModel {
	return &proveModel{v: v, t: newTheme(v.color, v.laneBackground()), w: w, proofs: proofs}
}

func (m *proveModel) Init() tea.Cmd { return nil }
func (m *proveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.v.width = size.Width
	}
	return m, nil
}
func (m *proveModel) View() tea.View { return tea.NewView(m.render()) }

func (m *proveModel) panelWidth() int { return max(14, m.v.width-len(ticketIndent)) }
func (m *proveModel) width() int      { return m.panelWidth() - 4 }

func (m *proveModel) render() string {
	return panels(m.t, m.panelWidth(), [][]string{m.summary(), m.invariants(), m.guarantees()})
}

// wrap breaks text to the panel, indenting every line, then styles each line.
func (m *proveModel) wrap(text, indent string, style func(string) string) []string {
	lines := wrapped(text, max(4, m.width()-len(indent)))
	for i, line := range lines {
		lines[i] = indent + style(line)
	}
	return lines
}

// pack joins names with sep, breaking lines only between names, so a stage or
// gate is never split. A continuation line starts with the separator.
func (m *proveModel) pack(names []string, sep, indent string, style func(string) string) []string {
	width := max(4, m.width()-len(indent))
	var joined []string
	line := ""
	for _, name := range names {
		switch {
		case line == "":
			line = name
		case runewidth.StringWidth(line+sep+name) <= width:
			line += sep + name
		default:
			joined = append(joined, line)
			line = strings.TrimLeft(sep, " ") + name
		}
	}
	joined = append(joined, line)
	var lines []string
	for _, line := range joined {
		lines = append(lines, m.wrap(line, indent, style)...)
	}
	return lines
}

func (m *proveModel) summary() []string {
	lines := []string{m.t.bold("WORKFILE PROVE")}
	failed := 0
	for _, proof := range m.proofs {
		if !proof.Holds {
			failed++
		}
	}
	text := "Every invariant holds. " + quantity(len(m.proofs), "invariant", "invariants") + " checked."
	switch {
	case len(m.proofs) == 0:
		text = "No invariants yet. Add them to policy.yml to make wf prove fail when a path skips a gate."
	case failed > 0:
		text = fmt.Sprintf("%d of %s can be skipped.", failed, quantity(len(m.proofs), "invariant", "invariants"))
	}
	return append(lines, m.wrap(text, "", m.t.dim)...)
}

func (m *proveModel) invariants() []string {
	if len(m.proofs) == 0 {
		return nil
	}
	lines := []string{m.t.bold("INVARIANTS")}
	for _, proof := range m.proofs {
		claim := proof.Stage + " requires " + human(proof.Gate)
		if proof.Holds {
			lines = append(lines, m.t.good("✓ ")+claim)
			continue
		}
		lines = append(lines, m.t.bad("✗ ")+claim)
		if len(proof.Bypass) == 0 {
			lines = append(lines, m.wrap("Tickets start in "+proof.Stage+", before any gate.", "  ", m.t.dim)...)
			continue
		}
		lines = append(lines, m.wrap("A ticket can reach "+proof.Stage+" without it:", "  ", m.t.dim)...)
		lines = append(lines, m.pack(stages(proof.Bypass), " → ", "  ", plainText)...)
		fix := "Require " + human(proof.Gate) + " on a move on this path, then run wf prove again."
		if len(proof.Fixes) > 0 {
			var moves []string
			for _, move := range proof.Fixes {
				moves = append(moves, move.From+" → "+move.To)
			}
			fix = "Require " + human(proof.Gate) + " on " + strings.Join(moves, " or ") + "."
		}
		lines = append(lines, m.wrap(fix, "  ", m.t.dim)...)
	}
	return lines
}

// stages lists the stages a bypass goes through, so it reads like the pipeline.
func stages(moves []workfile.Move) []string {
	result := []string{moves[0].From}
	for _, move := range moves {
		result = append(result, move.To)
	}
	return result
}

func (m *proveModel) guarantees() []string {
	lines := []string{m.t.bold("WHAT EACH STAGE GUARANTEES")}
	lines = append(lines, m.wrap("Every path to the stage passes these gates.", "", m.t.dim)...)
	guarantees := m.w.Policy.Guarantees()
	column := 0
	for _, stage := range m.w.Policy.States {
		column = max(column, len(stage))
	}
	// On a narrow screen the gates go under the stage instead of beside it.
	indent := strings.Repeat(" ", column+2)
	if m.width()-len(indent) < 16 {
		indent = "  "
	}
	for _, stage := range m.w.Policy.States {
		var names []string
		for _, gate := range guarantees[stage] {
			names = append(names, human(gate))
		}
		rows := m.pack(names, " · ", indent, plainText)
		if len(names) == 0 {
			rows = m.wrap("nothing", indent, m.t.dim)
		}
		if len(indent) == 2 {
			lines = append(lines, m.t.bold(stage))
		} else {
			rows[0] = m.t.bold(stage) + strings.Repeat(" ", len(indent)-len(stage)) + strings.TrimPrefix(rows[0], indent)
		}
		lines = append(lines, rows...)
	}
	return lines
}
