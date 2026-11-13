package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"workfile/internal/snapshot"
	"workfile/internal/workfile"
)

// comparison is what wf status --since found: the earlier snapshot it used
// and what changed since.
type comparison struct {
	found   bool // an earlier snapshot exists
	oldest  bool // it is older than asked for: the oldest on this computer
	since   snapshot.Since
	taken   time.Time // when the earlier snapshot was taken
	now     time.Time
	changes []snapshot.Change
}

// compare reads the earlier snapshot the person asked for and diffs it against
// this run. now is the snapshot of this run, taken after the earlier one was
// read.
func compare(store snapshot.Store, since snapshot.Since, now snapshot.Snapshot, stages []string) (comparison, error) {
	c := comparison{since: since, now: now.TakenAt}
	var before snapshot.Snapshot
	var ok bool
	var err error
	if since.Last {
		before, ok, err = store.Latest()
	} else {
		before, ok, err = store.Before(now.TakenAt.Add(-since.Ago))
	}
	if err != nil {
		return c, err
	}
	c.found = !before.TakenAt.IsZero()
	if !c.found {
		return c, nil
	}
	c.oldest = !since.Last && !ok
	c.taken = before.TakenAt
	c.changes = snapshot.OrderByStages(snapshot.Diff(before, now), stages)
	return c, nil
}

// when says which earlier run was used, in words.
func (c comparison) when() string {
	taken, now := c.taken.Local(), c.now.Local()
	var text string
	if c.since.Last {
		text = "your last run, " + elapsed(now.Sub(taken))
	} else {
		day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local) }
		switch days := int(day(now).Sub(day(taken)).Round(24*time.Hour) / (24 * time.Hour)); {
		case days <= 0:
			text = "today at " + taken.Format("15:04")
		case days == 1:
			text = "yesterday at " + taken.Format("15:04")
		default:
			text = taken.Format("Mon 2 Jan") + " at " + taken.Format("15:04")
		}
	}
	if c.oldest {
		text += " (the oldest on this computer)"
	}
	return text
}

func elapsed(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", unit)
		}
		return fmt.Sprintf("%d %ss ago", n, unit)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/(24*time.Hour)), "day")
}

// sinceModel draws the panel above the status output: what changed since an
// earlier run. Like the ticket screen it is drawn once and printed.
type sinceModel struct {
	v    view
	t    theme
	c    comparison
	urls map[string]string // ticket key to address
}

func newSinceModel(v view, c comparison, all []workfile.Assessment) *sinceModel {
	urls := map[string]string{}
	for _, a := range all {
		urls[a.Ticket.Key] = a.Ticket.URL
	}
	return &sinceModel{v: v, t: newTheme(v.color, v.laneBackground()), c: c, urls: urls}
}

func (m *sinceModel) panelWidth() int { return max(14, m.v.width-len(ticketIndent)) }
func (m *sinceModel) width() int      { return m.panelWidth() - 4 }

func (m *sinceModel) render() string {
	if !m.c.found {
		return panels(m.t, m.panelWidth(), [][]string{
			{m.t.bold("Since an earlier run")},
			m.wrap("Nothing to compare yet. Run wf status again later to see what changed.", m.t.dim),
		})
	}
	lines := m.wrap("Since "+m.c.when(), m.t.bold)
	if len(m.c.changes) == 0 {
		return panels(m.t, m.panelWidth(), [][]string{append(lines, m.wrap("Nothing changed.", m.t.dim)...)})
	}
	lines = append(lines, "")
	for i := 0; i < len(m.c.changes); {
		j := i
		for j < len(m.c.changes) && m.c.changes[j].Key == m.c.changes[i].Key {
			j++
		}
		lines = append(lines, m.row(m.c.changes[i:j])...)
		i = j
	}
	return panels(m.t, m.panelWidth(), [][]string{lines})
}

func (m *sinceModel) wrap(text string, style func(string) string) []string {
	lines := wrapped(text, m.width())
	for i, line := range lines {
		lines[i] = style(line)
	}
	return lines
}

// row is one ticket: its key, then its changes packed onto lines under it.
func (m *sinceModel) row(changes []snapshot.Change) []string {
	key := changes[0].Key
	target := m.urls[key]
	keyWidth := ansi.StringWidth(clean(key))
	gap := 2
	avail := max(8, m.width()-keyWidth-gap)
	var body []string
	var line string
	flush := func() {
		if line != "" {
			body = append(body, line)
			line = ""
		}
	}
	const sep = " · "
	for _, c := range changes {
		plain, style := m.describe(c)
		if ansi.StringWidth(plain) > avail {
			flush()
			for _, part := range wrapped(plain, avail) {
				body = append(body, style(part))
			}
			continue
		}
		cur := ansi.StringWidth(ansi.Strip(line))
		if line != "" && cur+len(sep)+ansi.StringWidth(plain) > avail {
			flush()
		}
		if line != "" {
			line += m.t.dim(sep)
		}
		line += style(plain)
	}
	flush()
	if len(body) == 0 {
		body = []string{""}
	}
	var out []string
	for i, text := range body {
		lead := strings.Repeat(" ", keyWidth+gap)
		if i == 0 {
			lead = m.t.bold(m.v.link(key, target)) + strings.Repeat(" ", gap)
		}
		out = append(out, strings.TrimRight(lead+text, " "))
	}
	if !m.v.hyperlinks() && target != "" {
		for _, r := range textRows(target, strings.Repeat(" ", keyWidth+gap), muted, "", m.width()) {
			out = append(out, m.v.render(r))
		}
	}
	return out
}

// describe is a change as plain text, and the style to draw it in. The icon
// and the words carry the meaning, colour only helps.
func (m *sinceModel) describe(c snapshot.Change) (string, func(string) string) {
	gates := func(label string) string {
		names := make([]string, len(c.Gates))
		for i, gate := range c.Gates {
			names[i] = clean(human(gate))
		}
		if len(names) == 0 {
			return label
		}
		return label + ": " + strings.Join(names, ", ")
	}
	same := func(s string) string { return s }
	switch c.Kind {
	case snapshot.Moved:
		return clean(c.From) + " → " + clean(c.To), same
	case snapshot.BecameReady:
		return "✓ ready for " + clean(c.To), m.t.good
	case snapshot.NewlyBlocked:
		return "✗ " + gates("blocked"), m.t.bad
	case snapshot.OutOfPolicy:
		return "‼ " + gates("out of policy"), m.t.bad
	case snapshot.BackOnTrack:
		return "· back on track", m.t.dim
	}
	return "new", same
}
