package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"workfile/internal/workfile"
)

// tracker is the place tickets live. Everything above this file works with any
// tracker: keys, statuses and addresses are the tracker's own text, and rich
// text arrives as workfile blocks. To add a tracker, write an adapter with
// these methods and list its kind in trackers. The steps are in
// docs/providers/building-a-provider.md.
type tracker interface {
	connection
	// Tickets reads tickets in full for policy checks: the named keys, or the
	// configured scope up to limit.
	Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error)
	// Listing reads the little a ticket list shows, for the whole scope and
	// everyone in it, so only_mine does not narrow it. A scope, in the tracker's
	// own query language, replaces the configured one.
	Listing(ctx context.Context, scope string) ([]workfile.Listing, error)
	// Detail reads one ticket's text and comments. If it cannot return every
	// comment, it returns the newest.
	Detail(ctx context.Context, key string) (workfile.Detail, error)
	// Image downloads an image named by a block's Attachment.
	Image(ctx context.Context, attachment string) ([]byte, error)
}

// trackers lists the adapters for each tracker kind.
var trackers = map[string]func(*Client, string) tracker{
	"jira":   func(c *Client, name string) tracker { return jiraTracker{Client: c, name: name} },
	"github": func(c *Client, name string) tracker { return githubTracker{Client: c, name: name} },
}

// stateFor is the policy state a status maps to, or "" if it has none. An exact
// match, ignoring case, wins. Otherwise leading symbols are ignored too, as emoji
// are hard to type and some hold invisible characters, so done: Done matches
// "☠️ Done". If that leaves names from two states, it is left unmapped, not
// guessed.
func stateFor(p workfile.Provider, status string) string {
	status = strings.TrimSpace(status)
	states := workfile.SortedKeys(p.Statuses)
	for _, state := range states {
		for _, name := range p.Statuses[state] {
			if strings.EqualFold(strings.TrimSpace(name), status) {
				return state
			}
		}
	}
	words := statusWords(status)
	if words == "" {
		return ""
	}
	found := ""
	for _, state := range states {
		for _, name := range p.Statuses[state] {
			if strings.EqualFold(statusWords(name), words) {
				if found != "" && found != state {
					return ""
				}
				found = state
			}
		}
	}
	return found
}

// statusWords is a status without its leading symbols.
func statusWords(status string) string {
	return strings.TrimSpace(strings.TrimLeftFunc(status, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}))
}

func (c *Client) tracker() (tracker, error) {
	name := c.Workspace.Policy.Tracker
	conn, err := c.connection(name)
	if err != nil {
		return nil, err
	}
	t, ok := conn.(tracker)
	if !ok {
		return nil, fmt.Errorf("Workfile cannot read tickets from %q yet", c.Workspace.Providers[name].Kind)
	}
	return t, nil
}

func (c *Client) Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error) {
	t, err := c.tracker()
	if err != nil {
		return nil, err
	}
	return t.Tickets(ctx, keys, limit)
}

func (c *Client) Listing(ctx context.Context, scope string) ([]workfile.Listing, error) {
	t, err := c.tracker()
	if err != nil {
		return nil, err
	}
	return t.Listing(ctx, scope)
}

func (c *Client) Detail(ctx context.Context, key string) (workfile.Detail, error) {
	t, err := c.tracker()
	if err != nil {
		return workfile.Detail{}, err
	}
	d, err := t.Detail(ctx, key)
	newestFirst(d.Comments)
	return d, err
}

// newestFirst puts the latest comment at the top, whichever tracker wrote them.
// Comments with no date go last. Replies keep their order, so a conversation
// still reads from its start.
func newestFirst(comments []workfile.Comment) {
	slices.SortStableFunc(comments, func(a, b workfile.Comment) int { return b.Created.Compare(a.Created) })
}

func (c *Client) Image(ctx context.Context, attachment string) ([]byte, error) {
	t, err := c.tracker()
	if err != nil {
		return nil, err
	}
	return t.Image(ctx, attachment)
}
