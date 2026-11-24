package cli

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"workfile/internal/workfile"
)

// trackerReads owns cached ticket reads and the delay before reading a newly
// selected ticket. A result is current only while its read is still cached.
type trackerReads struct {
	byKey      map[string]*ticketRead
	generation int
	pause      time.Duration
}

type ticketRead struct {
	detail                       *workfile.Detail
	detailErr                    error
	policy                       *workfile.Assessment
	policyErr                    error
	cancel                       context.CancelFunc
	ctx                          context.Context
	detailStarted, policyStarted bool
	checkedAt                    time.Time
	// failed holds the failing jobs of this ticket's PRs, by record ID. It is
	// read only when the ticket is opened, never for board cards.
	failed        map[string][]workfile.FailedCheck
	failedStarted bool
}

func (r *ticketRead) detailDone() bool { return r.detail != nil || r.detailErr != nil }
func (r *ticketRead) policyDone() bool { return r.policy != nil || r.policyErr != nil }
func (r *ticketRead) pending() bool {
	return r.detailStarted && !r.detailDone() || r.policyStarted && !r.policyDone() || r.failedStarted && r.failed == nil
}

type (
	// ticketSettledMsg says the selection stayed put long enough to read.
	ticketSettledMsg struct {
		gen int
		key string
	}
	ticketDetailMsg struct {
		read   *ticketRead
		detail workfile.Detail
		err    error
	}
	ticketPolicyMsg struct {
		read   *ticketRead
		policy workfile.Assessment
		err    error
	}
)

func (t *trackerReads) contains(read *ticketRead) bool {
	for _, candidate := range t.byKey {
		if candidate == read {
			return true
		}
	}
	return false
}

func (t *trackerReads) ensure(parent context.Context, ticket string) *ticketRead {
	r := t.byKey[ticket]
	if r == nil {
		ctx, cancel := context.WithCancel(parent)
		r = &ticketRead{ctx: ctx, cancel: cancel}
		t.byKey[ticket] = r
	}
	return r
}

func (r *ticketRead) checkPolicy(e env, ticket string) tea.Cmd {
	r.policyStarted = true
	ctx := r.ctx
	return func() tea.Msg {
		policy, err := readPolicy(ctx, e, ticket)
		return ticketPolicyMsg{r, policy, err}
	}
}

func (t *trackerReads) selectionChanged(previous string, keepPending bool) {
	if r := t.byKey[previous]; r != nil && r.pending() && !keepPending {
		t.discard(previous)
	}
	t.generation++
}

func (t *trackerReads) settle(ticket string) tea.Cmd {
	gen := t.generation
	return tea.Tick(t.pause, func(time.Time) tea.Msg { return ticketSettledMsg{gen, ticket} })
}

func (t *trackerReads) discard(ticket string) {
	if r := t.byKey[ticket]; r != nil && r.cancel != nil {
		r.cancel()
	}
	delete(t.byKey, ticket)
}

func (t *trackerReads) cancelAll() {
	for _, r := range t.byKey {
		if r.cancel != nil {
			r.cancel()
		}
	}
}

func (t *trackerReads) invalidatePolicy(parent context.Context) {
	// Completed rich previews remain cached, but stale policy evidence does not
	// keep looking fresh after a sync. New checks use the existing bounded queue.
	for ticket, r := range t.byKey {
		if r.cancel != nil {
			r.cancel()
		}
		if !r.detailDone() {
			delete(t.byKey, ticket)
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		t.byKey[ticket] = &ticketRead{detail: r.detail, detailErr: r.detailErr, detailStarted: true, ctx: ctx, cancel: cancel}
	}
}

func (t *trackerReads) start(parent context.Context, e env, ticket string) []tea.Cmd {
	r := t.ensure(parent, ticket)
	var cmds []tea.Cmd
	if !r.detailStarted {
		r.detailStarted = true
		ctx := r.ctx
		cmds = append(cmds, func() tea.Msg {
			detail, err := e.client.Detail(ctx, ticket)
			return ticketDetailMsg{r, detail, err}
		})
	}
	if !r.policyStarted && !r.policyDone() {
		cmds = append(cmds, r.checkPolicy(e, ticket))
	}
	return cmds
}

func (t *trackerReads) receivedDetail(msg ticketDetailMsg) bool {
	if !t.contains(msg.read) {
		return false
	}
	msg.read.detailErr = msg.err
	if msg.err == nil {
		msg.read.detail = &msg.detail
	} else {
		msg.read.detail = nil
	}
	return true
}

func (t *trackerReads) receivedPolicy(msg ticketPolicyMsg, now time.Time) bool {
	if !t.contains(msg.read) {
		return false
	}
	msg.read.policyErr, msg.read.checkedAt = msg.err, now
	if msg.err == nil {
		msg.read.policy = &msg.policy
	} else {
		msg.read.policy = nil
	}
	return true
}

// readPolicy checks one ticket as `wf status TICKET` does.
func readPolicy(ctx context.Context, e env, ticket string) (workfile.Assessment, error) {
	found, err := e.client.Tickets(ctx, []string{ticket}, 0)
	if err != nil {
		return workfile.Assessment{}, err
	}
	if len(found) == 0 {
		return workfile.Assessment{}, errors.New(ticket + " is not visible to your account")
	}
	found, _, err = e.client.Records(ctx, found[:1], true)
	if err != nil {
		return workfile.Assessment{}, err
	}
	return e.w.Assess(found[0]), nil
}
