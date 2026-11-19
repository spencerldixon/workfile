package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

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
	settings, err := workfile.ReadTrackerSettings(e.w)
	if err != nil {
		return 2, err
	}
	e.tracker = settings
	view, err := pickView(e.o, e.w.Policy, settings)
	if err != nil {
		return 2, err
	}
	if v.color && isTerminal(os.Stdin) && isTerminal(v.out) {
		m := newTrackerModel(ctx, e)
		m.gfx = detectGraphics()
		m.tagPalette = queryTicketPalette()
		final, err := runSyncTUI(ctx, v, m)
		if err != nil && ctx.Err() == nil {
			return 2, err
		}
		if m, ok := final.(*trackerModel); ok && m.fatal != nil {
			return 2, m.fatal
		}
		return 0, nil
	}
	defer e.startProgramWork()()
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
	if view.board() {
		return printTrackerBoard(ctx, e, all, view, me)
	}
	if view.attention() != "" {
		who := readActionIdentities(ctx, e, me).who
		base := arrangeTickets(all, e.w.Policy, view, me)
		checked, total, unclear := 0, 0, 0
		var matched []workfile.Listing
		for _, ticket := range base {
			if e.w.Policy.Transitions[ticket.State].End {
				continue
			}
			total++
			if checked >= trackerCheckBatch {
				continue
			}
			assessment, err := readPolicy(ctx, e, ticket.Key)
			checked++
			state := workfile.AttentionUnknown
			if err == nil {
				state = e.w.NextActions(assessment, who).State
			}
			if state == workfile.AttentionUnknown {
				unclear++
			}
			if string(state) == view.attention() {
				matched = append(matched, ticket)
			}
		}
		if len(matched) == 0 {
			v.heading("tracker")
			v.wrap("No matching tickets among the checked sample.", ticketIndent)
		} else {
			printTickets(v, e.w, matched)
		}
		v.wrap(fmt.Sprintf("Checked %d of %d active tickets; %d have unavailable evidence or unclear ownership. This is a sample when tickets remain unchecked.", checked, total, unclear), ticketIndent)
		return 0, nil
	}
	printTickets(v, e.w, arrangeTickets(all, e.w.Policy, view, me))
	return 0, nil
}

// ticketView is one tab: everything when empty, the person's work with me, or
// one state of the pipeline.
type ticketView struct {
	me     bool
	state  string
	custom *workfile.TrackerView
}

// ticketViews uses configured views when present. Without a views list, the
// defaults are everything, the person's work, then each state in workflow order.
func ticketViews(p workfile.Policy, settings workfile.TrackerSettings) []ticketView {
	var views []ticketView
	if settings.Views == nil {
		views = append(views, ticketView{}, ticketView{me: true})
		for _, state := range p.States {
			views = append(views, ticketView{state: state})
		}
	}
	for i := range settings.Views {
		custom := &settings.Views[i]
		views = append(views, ticketView{me: custom.Filter.Assignee == "me" || custom.Layout == "board" && custom.Filter.Assignee == "", custom: custom})
	}
	return views
}

// pickView is the view to start on, from --view. State names come from the
// policy, so they are checked here and not with the other arguments.
func pickView(o options, p workfile.Policy, settings workfile.TrackerSettings) (ticketView, error) {
	name := strings.ToLower(o.view)
	if name == "" {
		name = strings.ToLower(settings.DefaultView)
	}
	if name == "" {
		name = "all"
	}
	var choices []string
	for _, v := range ticketViews(p, settings) {
		id := "all"
		switch {
		case v.custom != nil:
			id = v.custom.ID
		case v.me:
			id = "me"
		case v.state != "":
			id = v.state
		}
		if strings.EqualFold(id, name) {
			return v, nil
		}
		choices = append(choices, id)
	}
	return ticketView{}, fmt.Errorf("%s is not a view; choose %s", clean(name), strings.Join(choices, ", "))
}

func (v ticketView) label() string {
	if v.custom != nil {
		return clean(v.custom.Title)
	}
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
	if v.custom != nil {
		filter := v.custom.Filter
		if len(filter.States) > 0 && !slices.Contains(filter.States, t.State) {
			return false
		}
		if len(filter.Origins) > 0 && !slices.ContainsFunc(filter.Origins, func(origin string) bool { return strings.EqualFold(origin, t.Origin) }) {
			return false
		}
		assignee := filter.Assignee
		if assignee == "" && v.board() {
			assignee = "me"
		}
		switch assignee {
		case "me":
			return me != "" && t.AssigneeID == me
		case "unassigned":
			return t.AssigneeID == "" && t.Assignee == ""
		}
		return true
	}
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

func printTickets(v view, w *workfile.Workspace, all []workfile.Listing) {
	v.heading(strings.ToLower(w.TrackerLabel()) + " tickets")
	v.wrap(scopeText(w, v.width-2), "  ")
	v.line("")
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
