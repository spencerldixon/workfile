package provider

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// SetupProject is a project visible to the connected Jira account.
type SetupProject struct{ Key, Name string }
type SetupPerson struct {
	AccountID, DisplayName, AccountType string
	Active                              bool
}

func (c *Client) SetupProjects(ctx context.Context) ([]SetupProject, error) {
	base, auth, err := c.jira(ctx, c.Workspace.Policy.Tracker)
	if err != nil {
		return nil, err
	}
	var all []SetupProject
	for start := 0; ; {
		var page struct {
			Values []SetupProject
			IsLast bool
			Total  int
		}
		err := c.request(ctx, "GET", base+"/rest/api/3/project/search?maxResults=100&startAt="+strconv.Itoa(start), auth, nil, &page)
		if err != nil {
			return nil, err
		}
		if page.Values == nil {
			return nil, errors.New("Jira returned no project list")
		}
		all = append(all, page.Values...)
		start += len(page.Values)
		if page.IsLast || start >= page.Total || len(page.Values) == 0 {
			return all, nil
		}
	}
}

// SetupStatuses reads project statuses, not admin-only workflow definitions.
func (c *Client) SetupStatuses(ctx context.Context, project string) ([]string, error) {
	base, auth, err := c.jira(ctx, c.Workspace.Policy.Tracker)
	if err != nil {
		return nil, err
	}
	var types []struct {
		Statuses []struct {
			Name           string
			StatusCategory struct{ Key string }
		}
	}
	if err = c.request(ctx, "GET", base+"/rest/api/3/project/"+url.PathEscape(project)+"/statuses", auth, nil, &types); err != nil {
		return nil, err
	}
	// Category order supplies a starting suggestion, not a claim about configured moves.
	var result []string
	seen := map[string]bool{}
	for _, category := range []string{"new", "indeterminate", "done", ""} {
		for _, t := range types {
			for _, s := range t.Statuses {
				known := s.StatusCategory.Key == "new" || s.StatusCategory.Key == "indeterminate" || s.StatusCategory.Key == "done"
				if (s.StatusCategory.Key == category || category == "" && !known) && s.Name != "" && !seen[s.Name] {
					result = append(result, s.Name)
					seen[s.Name] = true
				}
			}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Jira returned no project statuses")
	}
	return result, nil
}

// SetupPeople imports visible, active users assignable to the selected project.
func (c *Client) SetupPeople(ctx context.Context, project string) ([]SetupPerson, error) {
	base, auth, err := c.jira(ctx, c.Workspace.Policy.Tracker)
	if err != nil {
		return nil, err
	}
	var result []SetupPerson
	seen := map[string]bool{}
	for start := 0; ; start += 100 {
		var page []SetupPerson
		endpoint := base + "/rest/api/3/user/assignable/search?project=" + url.QueryEscape(project) + "&maxResults=100&startAt=" + strconv.Itoa(start)
		if err = c.request(ctx, "GET", endpoint, auth, nil, &page); err != nil {
			return nil, err
		}
		added := 0
		for _, person := range page {
			if person.AccountID != "" && !seen[person.AccountID] {
				seen[person.AccountID] = true
				added++
				if person.Active && person.AccountType != "app" {
					result = append(result, person)
				}
			}
		}
		if len(page) < 100 {
			return result, nil
		}
		if added == 0 {
			return nil, errors.New("Jira repeated the people page")
		}
	}
}
