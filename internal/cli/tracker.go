package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

var trackerCommand = command{
	flags: []string{"dir", "view"},
	args: func(o options) error {
		if len(o.keys) > 0 {
			return errors.New("tracker takes no positional arguments; search for a ticket with / on the screen")
		}
		return nil
	},
	run: tracker,
}

// tracker answers "what is this ticket, and what does it need?" It only reads:
// the tracker's ticket, plus what the policy says about it. In a terminal it is
// a Bubble Tea screen; piped, it prints a plain list.
func tracker(ctx context.Context, e env) (int, error) {
	v := e.view
	view, err := pickView(e.o, e.w.Policy)
	if err != nil {
		return 2, err
	}
	if v.color && isTerminal(os.Stdin) && isTerminal(v.out) {
		m := newTrackerModel(ctx, e)
		m.gfx = detectGraphics()
		final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)).Run()
		if err != nil && ctx.Err() == nil {
			return 2, err
		}
		if m, ok := final.(*trackerModel); ok && m.fatal != nil {
			return 2, m.fatal
		}
		return 0, nil
	}
	all, err := e.client.Listing(ctx, "")
	if err != nil {
		return 2, err
	}
	me := ""
	if view.me {
		if me, err = trackerIdentity(ctx, e); err != nil {
			return 2, err
		}
	}
	printTickets(v, arrangeTickets(all, e.w.Policy, view, me))
	return 0, nil
}

// ticketView is one tab: everything when empty, the person's work with me, or
// one state of the pipeline.
type ticketView struct {
	me    bool
	state string
}

// ticketViews lists the tabs: everything, the person's work, then each state in
// workflow order.
func ticketViews(p workfile.Policy) []ticketView {
	views := []ticketView{{}, {me: true}}
	for _, state := range p.States {
		views = append(views, ticketView{state: state})
	}
	return views
}

// pickView is the view to start on, from --view. State names come from the
// policy, so they are checked here and not with the other arguments.
func pickView(o options, p workfile.Policy) (ticketView, error) {
	name := strings.ToLower(o.view)
	switch {
	case name == "me":
		return ticketView{me: true}, nil
	case name == "" || name == "all":
		return ticketView{}, nil
	}
	for _, state := range p.States {
		if strings.EqualFold(state, o.view) {
			return ticketView{state: state}, nil
		}
	}
	return ticketView{}, fmt.Errorf("%s is not a view; choose %s", o.view, strings.Join(append([]string{"all", "me"}, p.States...), ", "))
}

func (v ticketView) label() string {
	switch {
	case v.me:
		return "Assigned to me"
	case v.state == "":
		return "All"
	}
	name := []rune(human(clean(v.state)))
	return strings.ToUpper(string(name[:1])) + string(name[1:])
}

func (v ticketView) keeps(t workfile.Listing, me string) bool {
	switch {
	case v.me:
		return me != "" && t.AssigneeID == me
	case v.state != "":
		return t.State == v.state
	}
	return true
}

// arrangeTickets keeps what the view wants, ordered by the pipeline with the
// tracker's order within each stage. Tickets with no known state come last, so
// they are seen, not lost.
func arrangeTickets(all []workfile.Listing, p workfile.Policy, v ticketView, me string) []workfile.Listing {
	stage := func(t workfile.Listing) int {
		if i := slices.Index(p.States, t.State); i >= 0 {
			return i
		}
		return len(p.States)
	}
	shown := slices.DeleteFunc(slices.Clone(all), func(t workfile.Listing) bool { return !v.keeps(t, me) })
	slices.SortStableFunc(shown, func(a, b workfile.Listing) int { return stage(a) - stage(b) })
	return shown
}

func printTickets(v view, all []workfile.Listing) {
	v.heading("tickets")
	if len(all) == 0 {
		v.wrap(noTickets, "  ")
		v.line("")
		return
	}
	for _, t := range all {
		v.wrap(t.Key+"  "+t.Title, "  ")
		v.wrap(fmt.Sprintf("%s · %s", t.Status, assigneeName(t)), "    ")
		v.wrap(t.URL, "    ")
		v.line("")
	}
}

const noTickets = "No tickets in your scope."

func assigneeName(t workfile.Listing) string {
	if t.Assignee == "" {
		return "Unassigned"
	}
	return t.Assignee
}
