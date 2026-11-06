package cli

import (
	"context"
	"fmt"
	"slices"

	"workfile/internal/workfile"
)

var statusCommand = command{flags: ticketFlags, ticketKeys: true, run: status}

// status answers "what do I do next?" for the board, a person or named
// tickets. Exit 1 when a ticket is out of policy or blocked, 2 when one cannot
// be checked.
func status(ctx context.Context, e env) (int, error) {
	v, w, o := e.view, e.w, e.o
	r, err := assess(ctx, e)
	if err != nil {
		return 2, err
	}
	code := r.exitCode(func(a workfile.Assessment) bool { return a.Violation() || a.Blocked() })
	switch {
	case o.json:
		return code, writeReport(v.out, w, r.all, o, r.warnings, code)
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
