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

var lintCommand = command{
	flags: []string{"dir", "json"},
	args: func(o options) error {
		if len(o.keys) > 0 {
			return errors.New("lint takes no positional arguments; it checks the policy, not tickets")
		}
		return nil
	},
	noWorkspace: true,
	run:         lint,
}

// proveCommand is a temporary alias for lint, replaced by the alias table.
var proveCommand = command{
	flags:       lintCommand.flags,
	args:        lintCommand.args,
	noWorkspace: true,
	run: func(ctx context.Context, e env) (int, error) {
		fmt.Fprintln(e.errOut, "wf: prove is now lint. The old name will stop working in a later release.")
		return lint(ctx, e)
	},
}

// lint checks the policy itself, not tickets. It reports problems that stop
// the policy loading, plain mistakes, and then answers: which gates is a
// ticket in each stage sure to have passed, and do the invariants in
// policy.yml hold? It reads no provider, so it needs no credentials and runs
// in CI on a policy change. Exit 0 sound (warnings allowed), 1 a problem or a
// failing invariant. A missing or unreadable .workfile/ is an error, exit 2.
func lint(_ context.Context, e env) (int, error) {
	v, o := e.view, e.o
	w, err := workfile.Load(o.dir)
	var pe *workfile.PolicyError
	var problems, warnings []workfile.Finding
	var proofs []workfile.Proof
	switch {
	case errors.As(err, &pe):
		for _, problem := range pe.Problems {
			problems = append(problems, workfile.Finding{Rule: "invalid-policy", Severity: "error", Message: problem})
		}
		w = nil
	case err != nil:
		return 0, err
	default:
		warnings = w.Policy.Lint()
		proofs = w.Policy.Prove()
	}
	code := 0
	if len(problems) > 0 {
		code = 1
	}
	for _, proof := range proofs {
		if !proof.Holds {
			code = 1
		}
	}
	if o.json {
		return code, writeLint(v.out, w, problems, warnings, proofs, code)
	}
	fmt.Fprint(v.out, newLintModel(v, w, problems, warnings, proofs).render())
	return code, nil
}

type stageGuarantee struct {
	Stage string   `json:"stage"`
	Gates []string `json:"gates"`
}

type lintReport struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	PolicyHash    string             `json:"policy_hash"`
	ExitCode      int                `json:"exit_code"`
	Problems      []workfile.Finding `json:"problems"`
	Warnings      []workfile.Finding `json:"warnings"`
	Invariants    []workfile.Proof   `json:"invariants"`
	Guarantees    []stageGuarantee   `json:"guarantees"`
}

// writeLint lists guarantees in pipeline order, so array order is meaningful
// here as in the other reports. w is nil when the policy did not load; the
// policy hash is then empty and the invariants and guarantees are empty.
func writeLint(out io.Writer, w *workfile.Workspace, problems, warnings []workfile.Finding, proofs []workfile.Proof, code int) error {
	report := lintReport{
		SchemaVersion: 1, Command: "lint", ExitCode: code,
		Problems:   append([]workfile.Finding{}, problems...),
		Warnings:   append([]workfile.Finding{}, warnings...),
		Invariants: append([]workfile.Proof{}, proofs...),
		Guarantees: []stageGuarantee{},
	}
	if w != nil {
		report.PolicyHash = w.PolicyHash()
		guarantees := w.Policy.Guarantees()
		for _, stage := range w.Policy.States {
			report.Guarantees = append(report.Guarantees, stageGuarantee{stage, append([]string{}, guarantees[stage]...)})
		}
	}
	return encodeCleanJSON(out, report)
}

// lintModel draws the answer to `wf lint`: a summary, problems, warnings, each
// invariant with the path that breaks it, then what each stage guarantees. It
// is drawn once and printed. w is nil when the policy did not load.
type lintModel struct {
	v                  view
	t                  theme
	w                  *workfile.Workspace
	problems, warnings []workfile.Finding
	proofs             []workfile.Proof
}

func newLintModel(v view, w *workfile.Workspace, problems, warnings []workfile.Finding, proofs []workfile.Proof) *lintModel {
	return &lintModel{v: v, t: newTheme(v.color, v.laneBackground()), w: w, problems: problems, warnings: warnings, proofs: proofs}
}

func (m *lintModel) Init() tea.Cmd { return nil }
func (m *lintModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.v.width = size.Width
	}
	return m, nil
}
func (m *lintModel) View() tea.View { return tea.NewView(m.render()) }

func (m *lintModel) panelWidth() int { return max(14, m.v.width-len(ticketIndent)) }
func (m *lintModel) width() int      { return m.panelWidth() - 4 }

func (m *lintModel) render() string {
	return panels(m.t, m.panelWidth(), [][]string{m.summary(), m.findings("PROBLEMS", m.problems, m.t.bad("✗ ")), m.findings("WARNINGS", m.warnings, m.t.dim("! ")), m.invariants(), m.guarantees()})
}

// wrap breaks text to the panel, indenting every line, then styles each line.
func (m *lintModel) wrap(text, indent string, style func(string) string) []string {
	lines := wrapped(text, max(4, m.width()-len(indent)))
	for i, line := range lines {
		lines[i] = indent + style(line)
	}
	return lines
}

// pack joins names with sep, breaking lines only between names, so a stage or
// gate is never split. A continuation line starts with the separator.
func (m *lintModel) pack(names []string, sep, indent string, style func(string) string) []string {
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

// findings lists problems or warnings, one wrapped message each.
func (m *lintModel) findings(title string, list []workfile.Finding, icon string) []string {
	if len(list) == 0 {
		return nil
	}
	lines := []string{m.t.bold(title)}
	for _, f := range list {
		parts := wrapped(clean(f.Message), max(4, m.width()-2))
		for i, line := range parts {
			if i == 0 {
				lines = append(lines, icon+line)
			} else {
				lines = append(lines, "  "+line)
			}
		}
	}
	return lines
}

func (m *lintModel) summary() []string {
	lines := []string{m.t.bold("WORKFILE LINT")}
	if len(m.problems) > 0 {
		text := quantity(len(m.problems), "problem", "problems") + " in .workfile/. Fix them, then run wf lint again."
		return append(lines, m.wrap(text, "", m.t.dim)...)
	}
	failed := 0
	for _, proof := range m.proofs {
		if !proof.Holds {
			failed++
		}
	}
	text := "Every invariant holds. " + quantity(len(m.proofs), "invariant", "invariants") + " checked."
	switch {
	case len(m.proofs) == 0:
		text = "No invariants to check."
	case failed > 0:
		text = fmt.Sprintf("%d of %s can be skipped.", failed, quantity(len(m.proofs), "invariant", "invariants"))
	}
	return append(lines, m.wrap(text, "", m.t.dim)...)
}

func (m *lintModel) invariants() []string {
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
		fix := "Require " + human(proof.Gate) + " on a move on this path, then run wf lint again."
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

func (m *lintModel) guarantees() []string {
	if m.w == nil {
		return nil
	}
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
