package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
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
	// own query language, replaces the configured extra filter, not the explicit
	// projects, repos and statuses.
	Listing(ctx context.Context, scope string) ([]workfile.Listing, error)
	// Detail reads one ticket's text and comments. If it cannot return every
	// comment, it returns the newest.
	Detail(ctx context.Context, key string) (workfile.Detail, error)
	// Image downloads an image named by a block's Attachment.
	Image(ctx context.Context, attachment string) ([]byte, error)
	// Activity reads what happened on these tickets after since, for the
	// inbox. No keys means no request. Kinds supplied are in Role.Activity.
	Activity(ctx context.Context, keys []string, since time.Time) ([]workfile.Activity, error)
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
	return t.Detail(ctx, key)
}

func (c *Client) Image(ctx context.Context, attachment string) ([]byte, error) {
	t, err := c.tracker()
	if err != nil {
		return nil, err
	}
	return t.Image(ctx, attachment)
}

// Activity reads one poll's activity: comments on the tickets from the
// tracker since trackerSince, and PR activity from every code host. A failing
// connection does not hide the others: their results come back with an error
// naming each failed connection.
func (c *Client) Activity(ctx context.Context, q workfile.ActivityQuery, trackerSince time.Time) (workfile.ActivityResult, error) {
	result := workfile.ActivityResult{Updated: map[string]time.Time{}}
	var errs []error
	if t, err := c.tracker(); err != nil {
		errs = append(errs, err)
	} else if events, err := t.Activity(ctx, q.Tickets, trackerSince); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", c.Workspace.Policy.Tracker, err))
	} else {
		result.Events = append(result.Events, events...)
	}
	err := c.eachHost(func(name string, host codeHost) error {
		got, err := host.Activity(ctx, q.PRs)
		if err != nil {
			return err
		}
		result.Events = append(result.Events, got.Events...)
		maps.Copy(result.Updated, got.Updated)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}

// InboxPRs reads personal PRs and explicitly watched repositories within each connection.
func (c *Client) InboxPRs(ctx context.Context, settings workfile.InboxSettings) (map[string][]workfile.Record, error) {
	out := map[string][]workfile.Record{}
	reviews := settings.ReviewRequests != nil && *settings.ReviewRequests
	err := c.eachHost(func(name string, host codeHost) error {
		records, err := host.InboxPRs(ctx, reviews, settings.Repositories[name])
		if err == nil {
			out[name] = records
		}
		return err
	})
	return out, err
}

// FailedChecks reads the failing jobs of one PR, from the code host
// connection that supplied it.
func (c *Client) FailedChecks(ctx context.Context, connection string, record workfile.Record) ([]workfile.FailedCheck, error) {
	if c.Workspace.IsTracker(connection) {
		return nil, fmt.Errorf("%s is not a code host", connection)
	}
	conn, err := c.connection(connection)
	if err != nil {
		return nil, err
	}
	host, ok := conn.(codeHost)
	if !ok {
		return nil, fmt.Errorf("%s is not a code host", connection)
	}
	return host.FailedChecks(ctx, record)
}

// eachHost runs fn for every code host connection, in name order, and joins
// the errors, each named by its connection.
func (c *Client) eachHost(fn func(string, codeHost) error) error {
	var errs []error
	for _, name := range workfile.SortedKeys(c.Workspace.Providers) {
		if c.Workspace.IsTracker(name) {
			continue
		}
		conn, err := c.connection(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		host, ok := conn.(codeHost)
		if !ok {
			continue
		}
		if err := fn(name, host); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
