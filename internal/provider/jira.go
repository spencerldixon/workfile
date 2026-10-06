package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

func (c *Client) jira(ctx context.Context, name string) (string, string, error) {
	p := c.Workspace.Providers[name]
	email, err := c.Credentials.Get(p.Auth["email"])
	if err != nil {
		return "", "", err
	}
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return "", "", err
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
	if base := c.jiraBases[name]; base != "" {
		return base, auth, nil
	}
	var tenant struct {
		CloudID string `json:"cloudId"`
	}
	if err := c.request(ctx, "GET", "https://"+p.Site+"/_edge/tenant_info", "", nil, &tenant); err != nil {
		return "", "", err
	}
	if tenant.CloudID == "" {
		return "", "", errors.New("cannot find the Jira cloud ID; check the site setting")
	}
	base := c.jiraGateway + url.PathEscape(tenant.CloudID)
	c.jiraBases[name] = base
	return base, auth, nil
}

type jiraIssue struct {
	ID, Key     string
	Unavailable map[string]string `json:"-"`
	Fields      struct {
		Summary     string
		Description json.RawMessage
		Labels      []string
		Status      struct{ Name string }
		Assignee    struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		}
		IssueType struct{ Name string } `json:"issuetype"`
	}
}

var orderBy = regexp.MustCompile(`(?i)\s+ORDER\s+BY\s+`)

// ORDER BY inside a quoted JQL value is part of the condition, not its ordering.
func splitScope(scope string) (string, string) {
	var quote rune
	escaped := false
	for i, r := range scope {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			if match := orderBy.FindStringIndex(scope[i:]); match != nil && match[0] == 0 {
				return strings.TrimSpace(scope[:i]), strings.TrimSpace(scope[i+match[1]:])
			}
		}
	}
	return strings.TrimSpace(scope), "updated DESC"
}

func (c *Client) Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error) {
	name := c.Workspace.Policy.Tracker
	p := c.Workspace.Providers[name]
	base, auth, err := c.jira(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	condition, order := splitScope(p.Scope)
	jql := "(" + condition + ")"
	if len(keys) > 0 {
		quoted := make([]string, len(keys))
		for i, key := range keys {
			data, _ := json.Marshal(key)
			quoted[i] = string(data)
		}
		jql += " AND key in (" + strings.Join(quoted, ", ") + ")"
	}
	jql += " ORDER BY " + order
	var issues []jiraIssue
	seen := map[string]bool{}
	token := ""
	for {
		pageSize := 100
		if limit > 0 {
			pageSize = min(pageSize, limit-len(issues))
		}
		body := map[string]any{"jql": jql, "fields": []string{"summary", "description", "labels", "status", "assignee", "issuetype"}, "maxResults": pageSize}
		if token != "" {
			body["nextPageToken"] = token
		}
		var page struct {
			Issues        []jiraIssue
			NextPageToken string
			IsLast        bool
		}
		if err := c.request(ctx, "POST", base+"/rest/api/3/search/jql", auth, body, &page); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if page.Issues == nil {
			return nil, errors.New("Jira search response is missing issues")
		}
		issues = append(issues, page.Issues...)
		if limit > 0 && len(issues) >= limit {
			issues = issues[:limit]
			break
		}
		if page.IsLast || page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
		if seen[token] {
			return nil, errors.New("Jira repeated a search page token")
		}
		seen[token] = true
	}
	var adders map[string]map[string]workfile.Actor
	if c.needsLabelHistory() {
		adders, err = c.labelAdders(ctx, base, auth, issues)
		if err != nil {
			return nil, fmt.Errorf("%s label history: %w", name, err)
		}
	}
	items := make([]workfile.Ticket, 0, len(issues))
	for _, issue := range issues {
		f := issue.Fields
		if issue.Key == "" || issue.ID == "" {
			return nil, errors.New("Jira returned a ticket without an ID or key")
		}
		t := workfile.Ticket{Key: issue.Key, Title: f.Summary, URL: "https://" + p.Site + "/browse/" + url.PathEscape(issue.Key), Assignee: f.Assignee.AccountID}
		for state, names := range p.Statuses {
			for _, status := range names {
				if strings.EqualFold(status, f.Status.Name) {
					t.State = state
				}
			}
		}
		if t.State == "" {
			t.Error = fmt.Sprintf("Jira status %q is not mapped in %s.statuses", f.Status.Name, name)
		}
		assignee := "unassigned"
		if f.Assignee.DisplayName != "" {
			assignee = "assigned to " + f.Assignee.DisplayName
		}
		t.Summary = f.Status.Name + " · " + assignee
		labels := make([]workfile.Actor, 0, len(f.Labels))
		for _, label := range f.Labels {
			actor := adders[issue.ID][label]
			actor.Value = label
			labels = append(labels, actor)
		}
		text, headings := description(f.Description)
		t.Facts = workfile.Facts{"summary": f.Summary, "description": text, "headings": headings, "labels": labels, "assignee": f.Assignee.DisplayName, "type": f.IssueType.Name, "project": strings.SplitN(issue.Key, "-", 2)[0]}
		t.Unavailable = issue.Unavailable
		if len(f.Description) == 0 {
			t.Unavailable["description"] = "Jira did not return the description"
			t.Unavailable["headings"] = "Jira did not return the description headings"
		}
		if f.Labels == nil {
			t.Unavailable["labels"] = "Jira did not return labels"
		}
		items = append(items, t)
	}
	return items, nil
}

func (c *Client) needsLabelHistory() bool {
	for _, rules := range c.Workspace.Policy.Gates {
		for _, rule := range rules {
			if rule.Condition.Provider == c.Workspace.Policy.Tracker && rule.Condition.Fact == "labels" && len(rule.By) > 0 {
				return true
			}
		}
	}
	return false
}

type history struct {
	Created string
	Author  struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	}
	Items []struct {
		FieldID              string `json:"fieldId"`
		FromString, ToString string
	}
}

func (c *Client) labelAdders(ctx context.Context, base, auth string, issues []jiraIssue) (map[string]map[string]workfile.Actor, error) {
	result := map[string]map[string]workfile.Actor{}
	for start := 0; start < len(issues); start += 1000 {
		ids := []string{}
		for _, issue := range issues[start:min(start+1000, len(issues))] {
			ids = append(ids, issue.ID)
		}
		histories := map[string][]history{}
		token := ""
		seen := map[string]bool{}
		for {
			body := map[string]any{"issueIdsOrKeys": ids, "fieldIds": []string{"labels"}, "maxResults": 1000}
			if token != "" {
				body["nextPageToken"] = token
			}
			var page struct {
				IssueChangeLogs []struct {
					IssueID         string `json:"issueId"`
					ChangeHistories []history
				}
				NextPageToken string
			}
			if err := c.request(ctx, "POST", base+"/rest/api/3/changelog/bulkfetch", auth, body, &page); err != nil {
				return nil, err
			}
			if page.IssueChangeLogs == nil {
				return nil, errors.New("Jira response is missing label history")
			}
			for _, log := range page.IssueChangeLogs {
				histories[log.IssueID] = append(histories[log.IssueID], log.ChangeHistories...)
			}
			token = page.NextPageToken
			if token == "" {
				break
			}
			if seen[token] {
				return nil, errors.New("Jira repeated a history page token")
			}
			seen[token] = true
		}
		for id, entries := range histories {
			slices.SortStableFunc(entries, func(a, b history) int { return strings.Compare(a.Created, b.Created) })
			result[id] = map[string]workfile.Actor{}
			for _, h := range entries {
				for _, change := range h.Items {
					if change.FieldID != "labels" {
						continue
					}
					before, after := strings.Fields(change.FromString), strings.Fields(change.ToString)
					for _, label := range before {
						if !slices.Contains(after, label) {
							delete(result[id], label)
						}
					}
					for _, label := range after {
						if !slices.Contains(before, label) {
							result[id][label] = workfile.Actor{ID: h.Author.AccountID, Name: h.Author.DisplayName}
						}
					}
				}
			}
		}
	}
	return result, nil
}
