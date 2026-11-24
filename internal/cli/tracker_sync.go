package cli

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

const trackerSyncInterval = 2 * time.Minute

// trackerRefresh owns the refresh clock and the generation that identifies
// the current timer or in-flight refresh.
type trackerRefresh struct {
	lastSync, nextSync time.Time
	generation         int
	syncing            bool
}

type trackerSyncScheduleMsg struct{ generation int }
type trackerSyncDueMsg struct{ generation int }
type trackerSyncedMsg struct {
	generation int
	all        []workfile.Listing
	err        error
}

// Scheduling is a separate message so starting a timer never blocks a model
// update. Generation checks discard timers superseded by a manual refresh.
func (r *trackerRefresh) schedule(now time.Time) tea.Cmd {
	r.nextSync = now.Add(trackerSyncInterval)
	generation := r.generation
	return func() tea.Msg { return trackerSyncScheduleMsg{generation} }
}

func (r *trackerRefresh) timer(msg trackerSyncScheduleMsg, now time.Time) tea.Cmd {
	if msg.generation != r.generation || r.nextSync.IsZero() {
		return nil
	}
	generation := msg.generation
	delay := max(time.Duration(0), r.nextSync.Sub(now))
	return tea.Tick(delay, func(time.Time) tea.Msg { return trackerSyncDueMsg{generation} })
}

func (m *trackerModel) syncDue(msg trackerSyncDueMsg) tea.Cmd {
	if !m.refresh.due(msg) {
		return nil
	}
	if m.shownView().board() {
		return m.syncTracker()
	}
	// List views retain their on-demand behaviour and make no polling requests.
	return m.refresh.deferSync(m.now())
}

func (r *trackerRefresh) due(msg trackerSyncDueMsg) bool {
	return msg.generation == r.generation && !r.syncing
}

func (r *trackerRefresh) deferSync(now time.Time) tea.Cmd {
	r.generation++
	return r.schedule(now)
}

func (r *trackerRefresh) begin() bool {
	if r.syncing {
		return false
	}
	r.syncing = true
	r.generation++
	r.nextSync = time.Time{}
	return true
}

func (r *trackerRefresh) finish(generation int) bool {
	if generation != r.generation || !r.syncing {
		return false
	}
	r.syncing = false
	return true
}

func (m *trackerModel) syncTracker() tea.Cmd {
	if !m.loaded || !m.refresh.begin() {
		return nil
	}
	m.notice = ""
	generation, ctx, e := m.refresh.generation, m.ctx, m.e
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		all, err := e.client.Listing(ctx, "")
		return trackerSyncedMsg{generation: generation, all: all, err: err}
	})
}

func (m *trackerModel) synced(msg trackerSyncedMsg) tea.Cmd {
	if !m.refresh.finish(msg.generation) {
		return nil
	}
	if msg.err != nil {
		m.notice = "Cannot sync tickets; showing the previous data."
		return m.refresh.schedule(m.now())
	}
	m.ticketReads.invalidatePolicy(m.ctx)
	if m.whoIncomplete {
		m.whoStarted = false
	}
	listed := m.listed(ticketListMsg{all: msg.all, me: m.me})
	var preview tea.Cmd
	if m.onTicket && m.current != "" {
		preview = m.read(m.current)
	}
	return tea.Batch(listed, preview)
}

func (r trackerRefresh) caption(loaded, compact, showNext, icons bool, now time.Time) string {
	syncIcon, clockIcon := "", ""
	if icons {
		syncIcon, clockIcon = "\uf021 ", "\uf017 "
	}
	if !loaded {
		return syncIcon + "Loading…"
	}
	if r.syncing {
		return syncIcon + "Syncing…"
	}
	if r.lastSync.IsZero() {
		return syncIcon + "Not synced"
	}
	layout, label := "15:04:05", "Synced "
	if compact {
		layout, label = "15:04", "Sync "
	}
	last := r.lastSync.Format(layout)
	if r.lastSync.Format("2006-01-02") != now.Format("2006-01-02") {
		last = r.lastSync.Format("2 Jan 15:04")
	}
	caption := syncIcon + label + last
	if showNext && !r.nextSync.IsZero() {
		caption += " · " + clockIcon + "next " + r.nextSync.Format(layout)
	}
	return caption
}

func (m *trackerModel) syncCaption(room int, showNext bool) string {
	now := m.now()
	caption := m.refresh.caption(m.loaded, false, showNext, m.t.color, now)
	if ansi.StringWidth(caption) <= room {
		return caption
	}
	caption = m.refresh.caption(m.loaded, true, showNext, m.t.color, now)
	if ansi.StringWidth(caption) > room && showNext {
		caption = m.refresh.caption(m.loaded, true, false, m.t.color, now)
	}
	return caption
}

func (r *trackerRefresh) markSynced(now time.Time) tea.Cmd {
	r.lastSync = now
	return r.deferSync(now)
}
