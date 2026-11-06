// Package provider is the read-only boundary between workfile and external services.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"workfile/internal/workfile"
)

type Client struct {
	Workspace              *workfile.Workspace
	HTTP                   *http.Client
	Credentials            workfile.Credentials
	githubURL, jiraGateway string
	jiraBases              map[string]string
	Now                    func() time.Time

	// viewFilters remembers each connection's saved view, read once per run.
	viewLock    sync.Mutex
	viewFilters map[string]string
}

func New(w *workfile.Workspace) *Client {
	return &Client{
		Workspace: w,
		HTTP:      &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		githubURL: "https://api.github.com/graphql", jiraGateway: "https://api.atlassian.com/ex/jira/",
		jiraBases: map[string]string{}, Now: time.Now, viewFilters: map[string]string{},
	}
}

// Never include response bodies or authenticated request URLs in an error.
func (c *Client) request(ctx context.Context, method, endpoint, auth string, body, target any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
		if err != nil {
			return errors.New("invalid provider URL")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "workfile")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		response, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("cannot reach provider; check your network and the configured site")
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
		response.Body.Close()
		status := response.StatusCode
		if (status == 429 || status == 502 || status == 503 || status == 504) && attempt < 2 {
			delay := time.Duration(attempt+1) * time.Second
			if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
			if delay > 10*time.Second {
				return errors.New("provider rate limit reached; try again later")
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if status < 200 || status >= 300 {
			hint := ""
			switch status {
			case 400:
				hint = "; check the configured query and settings"
			case 401:
				hint = "; check the account and token expiry"
			case 403:
				hint = "; check token permissions, organisation access, and SSO authorisation"
			case 429:
				hint = "; rate limit reached; try again later"
			}
			return fmt.Errorf("provider returned HTTP %d%s", status, hint)
		}
		if readErr != nil {
			return errors.New("could not read provider response")
		}
		if len(payload) > 16<<20 {
			return errors.New("provider response exceeds 16 MB; narrow the configured scope")
		}
		if err := json.Unmarshal(payload, target); err != nil {
			return errors.New("provider returned invalid JSON")
		}
		return nil
	}
	return errors.New("provider request failed")
}

func (c *Client) graph(ctx context.Context, token, query string, variables any, target any) error {
	data, failures, err := c.graphPost(ctx, token, query, variables)
	if err != nil {
		return err
	}
	if len(failures) > 0 {
		return errGraphRejected
	}
	return graphData(data, target)
}

// graphPartial is graph for a read where a part may not exist. GitHub answers
// that with NOT_FOUND beside the rest of the data, leaving the part null for the
// caller to notice. Only a NOT_FOUND at most depth steps into the response is
// allowed: any other error fails the read. Errors are told apart by type,
// because their messages are GitHub's own text and never reach a person.
func (c *Client) graphPartial(ctx context.Context, token, query string, variables any, depth int, target any) error {
	data, failures, err := c.graphPost(ctx, token, query, variables)
	if err != nil {
		return err
	}
	rejected := false
	for _, raw := range failures {
		var failure struct {
			Type string
			Path []any
		}
		_ = json.Unmarshal(raw, &failure)
		switch {
		case failure.Type == "INSUFFICIENT_SCOPES":
			return errGraphScopes
		case failure.Type == "NOT_FOUND" && len(failure.Path) > 0 && len(failure.Path) <= depth:
		default:
			rejected = true
		}
	}
	if rejected {
		return errGraphRejected
	}
	return graphData(data, target)
}

var (
	errGraphRejected = errors.New("GitHub rejected the query; check token permissions, organisation access, and rate limits")
	errGraphScopes   = errors.New("the token is missing a scope; a classic token needs repo, read:org and read:project")
)

func (c *Client) graphPost(ctx context.Context, token, query string, variables any) (json.RawMessage, []json.RawMessage, error) {
	var response struct {
		Data   json.RawMessage
		Errors []json.RawMessage
	}
	err := c.request(ctx, "POST", c.githubURL, "Bearer "+token, map[string]any{"query": query, "variables": variables}, &response)
	return response.Data, response.Errors, err
}

func graphData(data json.RawMessage, target any) error {
	if len(data) == 0 || string(data) == "null" {
		return errors.New("GitHub returned no data")
	}
	if json.Unmarshal(data, target) != nil {
		return errors.New("GitHub returned an unexpected response")
	}
	return nil
}

func (c *Client) Records(ctx context.Context, tickets []workfile.Ticket) ([]workfile.Ticket, []string, error) {
	if len(tickets) == 0 {
		return tickets, nil, nil
	}
	keys := c.Workspace.TrackerKeys()
	for _, name := range workfile.SortedKeys(c.Workspace.Providers) {
		if c.Workspace.IsTracker(name) {
			continue
		}
		conn, err := c.connection(name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		host, ok := conn.(codeHost)
		if !ok {
			return nil, nil, fmt.Errorf("%s: not a code host", name)
		}
		linked, err := host.Records(ctx, tickets, keys)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		for i := range tickets {
			if tickets[i].Records == nil {
				tickets[i].Records = map[string][]workfile.Record{}
			}
			tickets[i].Records[name] = linked[tickets[i].Key]
		}
	}
	return tickets, nil, nil
}

// download reads one file, never more than 16 MB. The address and credentials
// are the caller's; this follows no redirects, so the token stays on its host.
func (c *Client) download(ctx context.Context, address, auth string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, errors.New("invalid provider URL")
	}
	req.Header.Set("User-Agent", "workfile")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot reach provider; check your network and the configured site")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		return nil, errors.New("could not read provider response")
	}
	if len(data) > 16<<20 {
		return nil, errors.New("image exceeds 16 MB")
	}
	return data, nil
}
