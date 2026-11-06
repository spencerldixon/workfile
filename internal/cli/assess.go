package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"workfile/internal/workfile"
)

// ticketFlags are the options of the commands that report on many tickets.
var ticketFlags = []string{"dir", "user", "limit", "json", "evidence", "all", "me", "failing"}

// checked is the tickets a command asked about, assessed against the policy.
type checked struct {
	all      []workfile.Assessment
	warnings []string
	who      map[string]string // whose work this is; nil means everyone's
	missing  bool              // a named ticket could not be read
}

// assess reads the tickets in scope and checks each against the policy.
func assess(ctx context.Context, e env) (checked, error) {
	w, client, o := e.w, e.client, e.o
	var r checked
	var err error
	if o.user != "" {
		person, ok := w.People[o.user]
		if !ok {
			return r, fmt.Errorf("%s is not in people.yml", o.user)
		}
		r.who = map[string]string{}
		for name := range w.Providers {
			if person[name] != "" {
				r.who[name] = person[name]
			}
		}
		if len(r.who) == 0 {
			return r, errors.New("that person has no provider identities in people.yml")
		}
	} else if o.me {
		if r.who, err = identity(ctx, w, client); err != nil {
			return r, err
		}
	}
	limit := o.limit
	if !o.limited() || r.who != nil {
		limit = 0
	}
	tickets, err := client.Tickets(ctx, o.keys, limit)
	if err != nil {
		return r, err
	}
	for _, key := range o.keys {
		if !slices.ContainsFunc(tickets, func(t workfile.Ticket) bool { return t.Key == key }) {
			fmt.Fprintf(e.errOut, "wf: %s is not visible to your %s token: check the key, or ask for access\n", key, w.TrackerLabel())
			r.missing = true
		}
	}
	tickets, r.warnings, err = client.Records(ctx, tickets)
	if err != nil {
		return r, err
	}
	for _, warning := range r.warnings {
		fmt.Fprintln(e.errOut, "wf: "+clean(warning))
	}
	if r.who != nil {
		tickets = slices.DeleteFunc(tickets, func(t workfile.Ticket) bool { return !t.Involves(w.Policy.Tracker, r.who) })
	}
	if o.limited() && len(tickets) > o.limit {
		tickets = tickets[:o.limit]
	}
	r.all = make([]workfile.Assessment, len(tickets))
	for i, ticket := range tickets {
		r.all[i] = w.Assess(ticket)
	}
	return r, nil
}

// exitCode is 2 when anything could not be checked, 1 when a ticket fails, and
// 0 otherwise. Each command says what failing means to it.
func (r checked) exitCode(fails func(workfile.Assessment) bool) int {
	code := 0
	for _, a := range r.all {
		if fails(a) {
			code = max(code, 1)
		}
		if a.Ticket.Error != "" {
			code = 2
		}
	}
	if r.missing {
		code = 2
	}
	return code
}

// identity is who "me" is on each provider: the setting in config.yml, or
// whoever owns the token.
func identity(ctx context.Context, w *workfile.Workspace, client source) (map[string]string, error) {
	overrides, err := workfile.Me()
	if err != nil {
		return nil, err
	}
	who := map[string]string{}
	for _, name := range workfile.SortedKeys(w.Providers) {
		if who[name], err = identityOn(ctx, w, client, overrides, name); err != nil {
			return nil, err
		}
	}
	return who, nil
}

// identityOn is who "me" is on one provider.
func identityOn(ctx context.Context, w *workfile.Workspace, client source, overrides map[string]string, name string) (string, error) {
	id := overrides[name]
	if id == "" {
		id = overrides[w.Providers[name].Kind]
	}
	if id != "" {
		return id, nil
	}
	id, err := client.Identity(ctx, name)
	if err != nil {
		return "", fmt.Errorf("%s identity: %w", name, err)
	}
	return id, nil
}

// trackerIdentity is who "me" is on the tracker alone, for a filter that needs
// no other provider.
func trackerIdentity(ctx context.Context, e env) (string, error) {
	overrides, err := workfile.Me()
	if err != nil {
		return "", err
	}
	return identityOn(ctx, e.w, e.client, overrides, e.w.Policy.Tracker)
}
