package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

// checkMove answers one question, for an agent or a person: may this ticket
// move to that stage right now? It reads the ticket fresh and asks the policy.
// Agents run it before they move a ticket, so an automation can never move
// work the team's gates do not allow. Exit 0 yes, 1 no, 2 cannot check.
func checkMove(ctx context.Context, v view, w *workfile.Workspace, client source, o options, errOut io.Writer) (int, error) {
	if !slices.Contains(w.Policy.States, o.to) {
		return 2, fmt.Errorf("%s is not a stage in policy.yml", o.to)
	}
	tickets, err := client.Tickets(ctx, o.keys, 0)
	if err != nil {
		return 2, err
	}
	if len(tickets) == 0 {
		return 2, fmt.Errorf("%s is not visible to your Jira token: check the key, or ask for access", o.keys[0])
	}
	tickets, warnings, err := client.Records(ctx, tickets[:1])
	if err != nil {
		return 2, err
	}
	if !o.json {
		for _, warning := range warnings {
			fmt.Fprintln(errOut, "wf: "+clean(warning))
		}
	}
	a := w.Assess(tickets[0])
	r := judgeMove(a, o.to)
	if o.json {
		return r.code, writeCheck(v.out, w, a, r, warnings)
	}
	fmt.Fprint(v.out, newCheckModel(v, w, a, r).render())
	return r.code, nil
}

// moveVerdict is the answer to "may it move?".
type moveVerdict struct {
	to       string
	allowed  bool
	code     int
	problem  string // why it cannot be answered, or why there is no such move
	possible []string
	unmet    []workfile.Reason
}

func judgeMove(a workfile.Assessment, to string) moveVerdict {
	r := moveVerdict{to: to, code: 1}
	for _, route := range a.Routes {
		r.possible = append(r.possible, route.To)
	}
	i := slices.IndexFunc(a.Routes, func(route workfile.RouteAssessment) bool { return route.To == to })
	switch {
	case a.Ticket.Error != "":
		r.code, r.problem = 2, a.Ticket.Error
	case i < 0:
		r.problem = fmt.Sprintf("policy.yml has no move from %s to %s.", a.Ticket.State, to)
	case a.Routes[i].Available():
		r.allowed, r.code = true, 0
	default:
		for _, check := range a.Routes[i].Checks {
			for _, reason := range check.Reasons {
				if !slices.Contains(r.unmet, reason) {
					r.unmet = append(r.unmet, reason)
				}
			}
		}
	}
	return r
}

type checkRecord struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Repository string `json:"repository"`
	Provider   string `json:"provider"`
	Head       string `json:"head,omitempty"`
}

type checkReport struct {
	SchemaVersion int               `json:"schema_version"`
	Command       string            `json:"command"`
	PolicyHash    string            `json:"policy_hash"`
	ObservedAt    string            `json:"observed_at"`
	ExitCode      int               `json:"exit_code"`
	Allowed       bool              `json:"allowed"`
	Ticket        string            `json:"ticket"`
	Title         string            `json:"title"`
	URL           string            `json:"url"`
	From          string            `json:"from"`
	To            string            `json:"to"`
	Error         string            `json:"error,omitempty"`
	Possible      []string          `json:"possible_moves"`
	Unmet         []workfile.Reason `json:"unmet_requirements"`
	Records       []checkRecord     `json:"records"`
	Warnings      []string          `json:"warnings"`
}

// writeCheck includes each PR's latest commit, so an agent can confirm
// nothing changed between this check and the move it makes.
func writeCheck(out io.Writer, w *workfile.Workspace, a workfile.Assessment, r moveVerdict, warnings []string) error {
	report := checkReport{
		SchemaVersion: 1, Command: "check", PolicyHash: w.PolicyHash(), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExitCode: r.code, Allowed: r.allowed, Ticket: a.Ticket.Key, Title: a.Ticket.Title, URL: a.Ticket.URL,
		From: a.Ticket.State, To: r.to, Error: r.problem, Possible: append([]string{}, r.possible...),
		Unmet: append([]workfile.Reason{}, r.unmet...), Records: []checkRecord{}, Warnings: append([]string{}, warnings...),
	}
	for _, name := range workfile.SortedKeys(a.Ticket.Records) {
		for _, p := range a.Ticket.Records[name] {
			report.Records = append(report.Records, checkRecord{p.ID, p.URL, p.Title, p.Repository, name, p.Head})
		}
	}
	return encodeCleanJSON(out, report)
}

// checkModel draws the answer to `wf check`: the ticket, then one panel that
// says yes, or no and what to do first. It is drawn once and printed.
type checkModel struct {
	*ticketModel
	r moveVerdict
}

func newCheckModel(v view, w *workfile.Workspace, a workfile.Assessment, r moveVerdict) *checkModel {
	m := newTicketModel(v, w, a)
	m.to = r.to
	return &checkModel{ticketModel: m, r: r}
}

func (m *checkModel) Init() tea.Cmd                       { return nil }
func (m *checkModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *checkModel) View() tea.View                      { return tea.NewView(m.render()) }

func (m *checkModel) render() string {
	return panels(m.t, m.panelWidth(), [][]string{m.header(), m.answer()})
}

func (m *checkModel) answer() []string {
	r := m.r
	if r.allowed {
		lines := []string{m.verdictHeading("Can move to "+r.to, true)}
		return append(lines, m.wrap("Every requirement for "+r.to+" is met.", m.t.good)...)
	}
	lines := []string{m.verdictHeading("Cannot move to "+r.to+" yet", false)}
	if r.problem != "" {
		lines = append(lines, m.wrap(r.problem, m.t.bad)...)
		if r.code == 1 && len(r.possible) > 0 {
			lines = append(lines, m.wrap("It can move to "+strings.Join(r.possible, " or ")+".", m.t.dim)...)
		}
		return lines
	}
	return append(lines, m.fixes(m.blockers())...)
}

// panels draws each section in its own panel, a blank line apart. Without a
// background a panel is just its text.
func panels(t theme, width int, sections [][]string) string {
	lines := []string{""}
	for _, s := range sections {
		if len(s) == 0 {
			continue
		}
		if t.bg != "" {
			s = strings.Split(t.panel(strings.Join(s, "\n"), width), "\n")
		}
		for _, line := range s {
			lines = append(lines, strings.TrimRight(ticketIndent+line, " "))
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
}
