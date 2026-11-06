package cli

import (
	"context"
	"fmt"
	"slices"

	"workfile/internal/workfile"
)

var healthCommand = command{
	flags:      slices.Concat(ticketFlags, []string{"gate", "stage"}),
	ticketKeys: true,
	run:        health,
}

// health answers "is the board following our workflow?" Exit 1 when a ticket
// is out of policy, 2 when one cannot be checked.
func health(ctx context.Context, e env) (int, error) {
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
	code := r.exitCode(workfile.Assessment.Violation)
	switch {
	case o.json:
		return code, writeReport(v.out, w, r.all, o, r.warnings, code)
	case interactiveHealth(v, o, r.all):
		return code, exploreHealth(ctx, v, w, r.all, o)
	}
	v.healthReport(w, r.all, o)
	v.evidenceReport(r.all, o)
	return code, nil
}

func (v view) healthReport(w *workfile.Workspace, all []workfile.Assessment, o options) {
	switch {
	case o.listing():
		v.healthList(w, all, o)
	case len(all) == 0:
		v.heading("health")
		v.wrap("No tickets match this selection.", "  ")
		v.line("")
	default:
		fmt.Fprint(v.out, newHealthScreen(v, w, all, o).printed())
	}
}
