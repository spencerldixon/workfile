package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// lint checks the policy itself, not tickets. It reports problems that stop
// the policy loading, plain mistakes, and then answers: which gates is a
// ticket in each stage sure to have passed? It reads no provider, so it needs
// no credentials and runs in CI on a policy change. Exit 0 sound (warnings
// allowed, gaps included), 1 a problem. A missing or unreadable .workfile/ is an error, exit 2.
func lint(_ context.Context, e env) (int, error) {
	v, o := e.view, e.o
	v.width = max(v.width, fullWidth(v.out))
	w, err := workfile.Load(o.dir)
	var pe *workfile.PolicyError
	var problems, warnings []workfile.Finding
	var gaps []workfile.Gap
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
		gaps = w.Policy.Gaps()
	}
	code := 0
	if len(problems) > 0 {
		code = 1
	}
	if o.json {
		return code, writeLint(v.out, w, problems, warnings, gaps, code)
	}
	m := newLintModel(v, w, problems, warnings, gaps)
	if v.color && isTerminal(v.out) {
		m.gfx = detectGraphics()
	}
	fmt.Fprint(v.out, m.render())
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
	Gaps          []workfile.Gap     `json:"gaps"`
	Guarantees    []stageGuarantee   `json:"guarantees"`
}

// writeLint lists guarantees in pipeline order, so array order is meaningful
// here as in the other reports. w is nil when the policy did not load; the
// policy hash is then empty and the invariants and guarantees are empty.
func writeLint(out io.Writer, w *workfile.Workspace, problems, warnings []workfile.Finding, gaps []workfile.Gap, code int) error {
	report := lintReport{
		SchemaVersion: 1, Command: "lint", ExitCode: code,
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

// lintModel draws the answer to `wf lint`: one verdict, what is wrong, then a
// grid of which gates guard which stages. It is drawn once and printed, laid out
// for the width of the terminal. w is nil when the policy did not load.
type lintModel struct {
	v                  view
	t                  theme
	w                  *workfile.Workspace
	problems, warnings []workfile.Finding
	gaps               []workfile.Gap
	gfx                graphics
}

func newLintModel(v view, w *workfile.Workspace, problems, warnings []workfile.Finding, gaps []workfile.Gap) *lintModel {
	return &lintModel{v: v, t: newTheme(v.color, ""), w: w, problems: problems, warnings: warnings, gaps: gaps}
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
	sections := [][]string{
		m.summary(),
		m.findings("Problems", m.problems, m.t.bad("✗ ")),
		m.gapList(),
		m.findings("Warnings", m.warnings, m.t.warn("! ")),
		m.flow(),
	}
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
	lines := wrapped(text, max(4, m.width()-len(indent)))
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
	lines := []string{m.t.accent("wf lint"), ""}
	switch {
	case len(m.problems) > 0:
		lines = append(lines, m.t.bad("✗ ")+m.t.bold("Fail")+m.t.dim("  "+quantity(len(m.problems), "problem", "problems")+" in .workfile/"))
		return append(lines, m.wrap("The policy can't be used until these are fixed. Then run wf lint again.", "  ", m.t.dim)...)
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

// flow draws the policy: stages in order, shortcuts above and returns below.
// A move is green when it asks for a gate, red when it is a possible gap, and dim when it asks for nothing. A stage with a gap is red.
func (m *lintModel) flow() []string {
	if m.w == nil || len(m.problems) > 0 {
		return nil
	}
	p := m.w.Policy
	edges, broken := m.flowEdges()
	ends := map[string]bool{}
	for _, stage := range p.States {
		ends[stage] = p.Transitions[stage].End
	}
	layout := newFlowLayout(p.States, ends, edges, broken)
	flow := &layout
	lines := []string{m.t.bold("Flow")}
	if picture, ok := m.flowPicture(flow); ok {
		return append(lines, append(picture, m.flowKey()...)...)
	}
	v := m.v
	v.width = m.width() + 10
	if rows, ok := v.textFlow(flow, m.width()); ok {
		for _, r := range rows {
			lines = append(lines, strings.TrimRight(v.render(r), " "))
		}
		return append(lines, m.flowKey()...)
	}
	// Too wide for columns: strips of numbered stages.
	labels := make([]cell, len(p.States))
	for i, stage := range p.States {
		labels[i] = styled(stage, "1")
		if broken[stage] {
			labels[i] = styled(stage, "1;"+red)
		}
	}
	for _, r := range v.pipelineRows(p, workfile.Assessment{}, labels, edges) {
		lines = append(lines, v.render(r))
	}
	return append(lines, m.flowKey()...)
}

// flowEdges is the forward moves of the policy, each coloured for what it asks
// for, and the stages that have a possible gap. Returns are left out: gates
// guard the way forward.
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
	edges := slices.DeleteFunc(pipelineEdges(p, workfile.Assessment{}), pipelineEdge.backward)
	for i, e := range edges {
		from, to := p.States[e.from], p.States[e.to]
		edges[i].color = muted
		for _, route := range p.Routes(from) {
			if route.To == to && len(route.Requires) > 0 {
				edges[i].color = green
			}
		}
		if open[[2]string{from, to}] {
			edges[i].color = red
		}
	}
	return edges, broken
}

// flowPicture draws the policy as an image on a terminal that can show one. The
// picture is the text diagram with nicer clothes, so it needs the same key.
func (m *lintModel) flowPicture(layout *flowLayout) ([]string, bool) {
	if !m.v.color {
		return nil, false
	}
	img, rows, ok := flowImage(layout, m.gfx, min(m.width(), 120))
	if !ok {
		return nil, false
	}
	cols := img.Bounds().Dx() / m.gfx.cellW
	p := picture{id: lintImageID, cols: cols, rows: rows}
	sequence, err := transmit(img, p)
	if err != nil {
		return nil, false
	}
	lines := placeholderRows(p)
	lines[0] = sequence + lines[0]
	return lines, true
}

// flowKey says what the colours of the moves mean. Without colour it says
// nothing, because it would describe differences nobody can see.
func (m *lintModel) flowKey() []string {
	if !m.v.color {
		return nil
	}
	keys := []string{m.t.good("→") + " asks for a gate", m.t.dim("→ asks for nothing")}
	if len(m.gaps) > 0 {
		keys = append(keys, m.t.bad("→")+" possible gap")
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
