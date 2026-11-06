package provider

import (
	"context"
	"fmt"

	"workfile/internal/workfile"
)

// connection is what every provider can do: say who the token belongs to, and
// check that the configured scope can be read.
type connection interface {
	// Identity is the account that owns the token.
	Identity(ctx context.Context) (string, error)
	// Test reads the least that proves the connection works, and describes it.
	Test(ctx context.Context) (string, error)
}

// codeHost is where pull requests live. Records links them to tickets.
type codeHost interface {
	connection
	Records(ctx context.Context, tickets []workfile.Ticket, keys workfile.Keys) (map[string][]workfile.Record, error)
}

// codeHosts lists the adapters for each code host kind.
var codeHosts = map[string]func(*Client, string) codeHost{
	"github": func(c *Client, name string) codeHost { return githubCodeHost{Client: c, name: name} },
}

// connection finds the adapter for a named connection by its role: the tracker
// if the policy names it, otherwise a code host. An unknown name, kind or role
// is an error, never a guess.
func (c *Client) connection(name string) (connection, error) {
	p, ok := c.Workspace.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", name)
	}
	if c.Workspace.IsTracker(name) {
		if build, ok := trackers[p.Kind]; ok {
			return build(c, name), nil
		}
		return nil, fmt.Errorf("Workfile cannot use %q as a tracker yet", p.Kind)
	}
	if build, ok := codeHosts[p.Kind]; ok {
		return build(c, name), nil
	}
	return nil, fmt.Errorf("Workfile cannot use %q as a code host yet", p.Kind)
}

func (c *Client) Identity(ctx context.Context, name string) (string, error) {
	conn, err := c.connection(name)
	if err != nil {
		return "", err
	}
	return conn.Identity(ctx)
}

func (c *Client) Test(ctx context.Context, name string) (string, error) {
	conn, err := c.connection(name)
	if err != nil {
		return "", err
	}
	return conn.Test(ctx)
}
