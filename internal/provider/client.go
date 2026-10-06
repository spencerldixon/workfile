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
	"strings"
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
}

func New(w *workfile.Workspace) *Client {
	return &Client{
		Workspace: w,
		HTTP:      &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		githubURL: "https://api.github.com/graphql", jiraGateway: "https://api.atlassian.com/ex/jira/",
		jiraBases: map[string]string{}, Now: time.Now,
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
	var response struct {
		Data   json.RawMessage
		Errors []json.RawMessage
	}
	err := c.request(ctx, "POST", c.githubURL, "Bearer "+token, map[string]any{"query": query, "variables": variables}, &response)
	if err != nil {
		return err
	}
	if len(response.Errors) > 0 {
		return errors.New("GitHub rejected the query; check token permissions, organisation access, and rate limits")
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return errors.New("GitHub returned no data")
	}
	if json.Unmarshal(response.Data, target) != nil {
		return errors.New("GitHub returned an unexpected response")
	}
	return nil
}

func (c *Client) Identity(ctx context.Context, name string) (string, error) {
	p := c.Workspace.Providers[name]
	if p.Kind == "jira" {
		base, auth, err := c.jira(ctx, name)
		if err != nil {
			return "", err
		}
		var user struct {
			AccountID string `json:"accountId"`
		}
		if err := c.request(ctx, "GET", base+"/rest/api/3/myself", auth, nil, &user); err != nil {
			return "", err
		}
		if user.AccountID == "" {
			return "", errors.New("Jira did not return an account identity; check read:jira-user permission")
		}
		return user.AccountID, nil
	}
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return "", err
	}
	var response struct{ Viewer struct{ Login string } }
	if err := c.graph(ctx, token, `query { viewer { login } }`, nil, &response); err != nil {
		return "", err
	}
	if response.Viewer.Login == "" {
		return "", errors.New("GitHub did not return an account identity")
	}
	return response.Viewer.Login, nil
}

func (c *Client) Test(ctx context.Context, name string) (string, error) {
	identity, err := c.Identity(ctx, name)
	if err != nil {
		return "", err
	}
	p := c.Workspace.Providers[name]
	if p.Kind == "jira" {
		items, err := c.Tickets(ctx, nil, 1)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return identity + " · connected; scope returned no visible tickets", nil
		}
		detail := identity + " · scope readable"
		if c.needsLabelHistory() {
			detail += " · label history readable"
		}
		return detail, nil
	}
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return "", err
	}
	repos, err := c.repositories(ctx, p, token)
	if err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return identity + " · connected; no visible active repositories", nil
	}
	_, _, err = c.repositoryPRs(ctx, p, token, repos[0], true)
	return fmt.Sprintf("%s · %s · %d visible active repositories", identity, p.Org, len(repos)), err
}

func (c *Client) Records(ctx context.Context, tickets []workfile.Ticket) ([]workfile.Ticket, []string, error) {
	var warnings []string
	if len(tickets) == 0 {
		return tickets, warnings, nil
	}
	for _, name := range workfile.SortedKeys(c.Workspace.Providers) {
		if name == c.Workspace.Policy.Tracker {
			continue
		}
		linked, notes, err := c.githubRecords(ctx, name, tickets)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		warnings = append(warnings, notes...)
		for i := range tickets {
			if tickets[i].Records == nil {
				tickets[i].Records = map[string][]workfile.Record{}
			}
			tickets[i].Records[name] = linked[strings.ToUpper(tickets[i].Key)]
			if len(notes) > 0 {
				if tickets[i].Unavailable == nil {
					tickets[i].Unavailable = map[string]string{}
				}
				tickets[i].Unavailable[name+".prs"] = "No visible active repositories; PR evidence cannot be checked"
			}
		}
	}
	return tickets, warnings, nil
}
