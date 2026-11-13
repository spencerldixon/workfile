package cli

import (
	"context"
	"fmt"
	"slices"

	"workfile/internal/workfile"
)

var statusCommand = command{
	flags:      slices.Concat(ticketFlags, []string{"gate", "stage"}),
	ticketKeys: true,
	run:        status,
}

// statusMode is which screen wf status draws.
type statusMode int

const (
	modeRollup  statusMode = iota // no keys, no --me, no --user, no --gate, no --stage
	modeListing                   // --gate or --stage: tickets failing that gate or out of policy in that stage
	modeMine                      // --me or --user: the person's work
	modeTickets                   // keys: the detail of each ticket
)

// statusMode decides the mode from the arguments alone.
func (o options) statusMode() statusMode {
	switch {
	case len(o.keys) > 0:
		return modeTickets
	case o.gate != "" || o.stage != "":
		return modeListing
	case o.me || o.user != "":
		return modeMine
	}
	return modeRollup
}

// status answers "where is the work, and what does it need?" for the board, a
// person or named tickets. Exit 1 when a ticket is out of policy or blocked, 2
// when one cannot be checked.
func status(ctx context.Context, e env) (int, error) {
	v, w, o := e.view, e.w, e.o
	if o.gate != "" {
		if _, ok := w.Policy.Gates[o.gate]; !ok {
			return 2, fmt.Errorf("%s is not a gate in policy.yml", o.gate)
		}
	}
	if o.stage != "" && !slices.Contains(w.Policy.States, o.stage) {
		return 2, fmt.Errorf("%s is not a stage in policy.yml", o.stage)
	}
	r, err := assess(ctx, e)
	if err != nil {
		return 2, err
	}
	code := r.exitCode(func(a workfile.Assessment) bool { return a.Violation() || a.Blocked() })
	switch {
	case o.json:
		return code, writeReport(v.out, w, r.all, o, r.warnings, code)
	case interactiveHealth(v, o, r.all):
		return code, exploreHealth(ctx, v, w, r.all, o)
	case interactiveStatus(v, o, r.all):
		// m switches to the person's own work, so ask who they are, unless the
		// list is already someone's. Without an answer the key is left out.
		var me map[string]string
		if r.who == nil {
			me, _ = identity(ctx, w, e.client)
		}
		return code, exploreStatus(ctx, v, w, r.all, o, me)
	}
	v.statusReport(w, r.all, o)
	v.evidenceReport(r.all, o)
	return code, nil
}

func (v view) statusReport(w *workfile.Workspace, all []workfile.Assessment, o options) {
	switch o.statusMode() {
	case modeListing:
		v.healthList(w, all, o)
		return
	case modeRollup:
		if len(all) == 0 {
			v.heading("status")
			v.wrap("No tickets match this selection.", "  ")
			v.line("")
			return
		}
		fmt.Fprint(v.out, newHealthScreen(v, w, all, o).printed())
		return
	}
	if len(o.keys) > 0 {
		shown := slices.DeleteFunc(slices.Clone(all), func(a workfile.Assessment) bool { return o.failing && group(a) >= 3 })
		if len(shown) > 0 {
			for _, a := range shown {
				fmt.Fprint(v.out, newTicketModel(v, w, a).render())
			}
			return
		}
	}
	fmt.Fprint(v.out, newStatusScreen(v, w, all, o).printed())
}
