package cli

import (
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"workfile/internal/workfile"
)

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
	Gaps          []workfile.Gap     `json:"gaps"`
	Guarantees    []stageGuarantee   `json:"guarantees"`
}

// writeLint lists guarantees in pipeline order, so array order is meaningful
// here as in the other reports. w is nil when the policy did not load; the
// policy hash and guarantees are then empty.
func writeLint(out io.Writer, w *workfile.Workspace, problems, warnings []workfile.Finding, gaps []workfile.Gap, code int, command string) error {
	report := lintReport{
		SchemaVersion: 1, Command: command, ExitCode: code,
		Problems:   append([]workfile.Finding{}, problems...),
		Warnings:   append([]workfile.Finding{}, warnings...),
		Gaps:       append([]workfile.Gap{}, gaps...),
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

// lintModel draws the workflow diagram and the sections for the selected policy
// mode. w is nil when the policy did not load.
type lintModel struct {
	v                  view
	t                  theme
	w                  *workfile.Workspace
	problems, warnings []workfile.Finding
	gaps               []workfile.Gap
	gfx                graphics
	invocation         string
	policy             *policyWorkspace
}

func newLintModel(v view, w *workfile.Workspace, problems, warnings []workfile.Finding, gaps []workfile.Gap) *lintModel {
	return &lintModel{v: v, t: newTheme(v.color, ""), w: w, problems: problems, warnings: warnings, gaps: gaps, invocation: "wf policy --lint"}
}

func (m *lintModel) Init() tea.Cmd { return nil }
func (m *lintModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.v.width = size.Width
	}
	return m, nil
}
func (m *lintModel) View() tea.View { return tea.NewView(m.render()) }

func (m *lintModel) width() int { return max(14, m.v.width-2*len(ticketIndent)) }

// render joins the sections with a blank line between, on the terminal's own
// background.
func (m *lintModel) render() string {
	if m.policy != nil {
		return m.renderSections(m.policySections(m.policy)...)
	}
	return m.renderSections(
		m.summary(),
		m.findings("Problems", m.problems, m.t.bad("✗ ")),
		m.gapList(),
		m.findings("Warnings", m.warnings, m.t.warn("! ")),
		m.flow(),
	)
}

func (m *lintModel) renderSections(sections ...[]string) string {
	lines := []string{""}
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		for _, line := range section {
			lines = append(lines, strings.TrimRight(ticketIndent+line, " "))
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

// wrap breaks text to the screen, indenting every line, then styles each line.
func (m *lintModel) wrap(text, indent string, style func(string) string) []string {
	lines := wrapped(clean(text), max(4, m.width()-len(indent)))
	for i, line := range lines {
		lines[i] = indent + style(line)
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
		for i, line := range wrapped(clean(f.Message), max(4, m.width()-2)) {
			if i == 0 {
				lines = append(lines, icon+line)
			} else {
				lines = append(lines, "  "+line)
			}
		}
	}
	return lines
}

// summary is the verdict: pass or fail, and why, in the first lines you read.
func (m *lintModel) summary() []string {
	lines := []string{m.t.accent(m.invocation), ""}
	switch {
	case len(m.problems) > 0:
		lines = append(lines, m.t.bad("✗ ")+m.t.bold("Fail")+m.t.dim("  "+quantity(len(m.problems), "problem", "problems")+" in .workfile/"))
		return append(lines, m.wrap("The policy can't be used until these are fixed. Then run "+m.invocation+" again.", "  ", m.t.dim)...)
	case len(m.gaps) > 0:
		lines = append(lines, m.t.warn("! ")+m.t.bold("Pass")+m.t.dim("  "+quantity(len(m.gaps), "gate", "gates")+" can be skipped"))
		return append(lines, m.wrap("The policy is valid, but a ticket can reach some stages without passing every gate asked for on the way in.", "  ", m.t.dim)...)
	}
	lines = append(lines, m.t.good("✓ ")+m.t.bold("Pass"))
	return append(lines, m.wrap("Nothing can slip through: every route into a stage passes every gate asked for on the way in.", "  ", m.t.dim)...)
}

// gapList says which gates a ticket can skip. A ticket only needs one route to
// pass, so this may be on purpose; the section says how to close a gap if not.
func (m *lintModel) gapList() []string {
	if len(m.gaps) == 0 {
		return nil
	}
	lines := []string{m.t.bold("Gates that can be skipped")}
	lines = append(lines, m.wrap("A ticket only needs one route into a stage to pass. If these routes are meant to be alternatives, that's fine. If not, require the gate on the moves shown, or on every move into the stage with an any entry.", "", m.t.dim)...)
	lines = append(lines, "")
	for _, gap := range m.gaps {
		text := gap.Stage + ": " + human(gap.Gate) + " is skipped by " + joinMoves(gap.Open)
		for i, line := range wrapped(text, max(4, m.width()-2)) {
			if i == 0 {
				lines = append(lines, m.t.warn("! ")+line)
			} else {
				lines = append(lines, "  "+line)
			}
		}
	}
	return lines
}

func joinMoves(moves []workfile.Move) string {
	var names []string
	for _, move := range moves {
		names = append(names, move.From+" → "+move.To)
	}
	return strings.Join(names, " or ")
}

// flow draws the policy with shortcuts above and returns below.
func (m *lintModel) flow() []string {
	if m.w == nil || len(m.problems) > 0 {
		return nil
	}
	edges, broken := m.flowEdges()
	diagram := newWorkflowDiagram(m.w.Policy, edges, broken, true)
	var sequence string
	var img *picture
	if m.v.color {
		sequence, img = diagram.printedPicture(m.gfx, m.width(), lintImageID)
	}
	diagramLines := diagram.lines(m.v, m.width(), img)
	if sequence != "" && len(diagramLines) > 0 {
		diagramLines[0] = sequence + diagramLines[0]
	}
	lines := append([]string{m.t.bold("Flow")}, diagramLines...)
	return append(lines, m.flowKey()...)
}

// flowEdges colours gated moves and highlights possible gaps.
func (m *lintModel) flowEdges() ([]pipelineEdge, map[string]bool) {
	p := m.w.Policy
	open := map[[2]string]bool{}
	broken := map[string]bool{}
	for _, gap := range m.gaps {
		broken[gap.Stage] = true
		for _, move := range gap.Open {
			open[[2]string{move.From, move.To}] = true
		}
	}
	edges := pipelineEdges(p, workfile.Assessment{})
	for i, e := range edges {
		from, to := p.States[e.from], p.States[e.to]
		edges[i].color = muted
		if len(e.gates) > 0 {
			edges[i].color = green
		}
		if open[[2]string{from, to}] {
			edges[i].color = red
		}
	}
	return edges, broken
}

// flowKey says what the colours of the moves mean. Without colour it says
// nothing, because it would describe differences nobody can see.
func (m *lintModel) flowKey() []string {
	if !m.v.color {
		return nil
	}
	keys := []string{m.t.good("->") + " Asks For A Gate", m.t.dim("-> Asks For Nothing")}
	if len(m.gaps) > 0 {
		keys = append(keys, m.t.bad("->")+" Possible Gap")
	}
	// The key wraps between entries on a narrow screen.
	lines := []string{""}
	line := ""
	for _, key := range keys {
		if line != "" && lipgloss.Width(line)+3+lipgloss.Width(key) > m.width() {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += key
	}
	return append(lines, line)
}
