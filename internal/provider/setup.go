package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"workfile/internal/workfile"
)

// SetupProject is a project visible to the connected tracker account.
type SetupProject struct{ Key, Name string }

// SetupRepo is an unarchived repository in the connected GitHub organisation.
type SetupRepo struct{ Name string }

type SetupPerson struct {
	AccountID, DisplayName, AccountType string
	Active                              bool
}

type setupTracker interface {
	setupProjects(context.Context) ([]SetupProject, error)
	setupStatuses(context.Context, string) ([]string, error)
}

func (c *Client) setupTracker() (setupTracker, error) {
	conn, err := c.connection(c.Workspace.Policy.Tracker)
	if err != nil {
		return nil, err
	}
	t, ok := conn.(setupTracker)
	if !ok {
		return nil, errors.New("the tracker does not support interactive setup")
	}
	return t, nil
}

func (c *Client) SetupProjects(ctx context.Context) ([]SetupProject, error) {
	t, err := c.setupTracker()
	if err != nil {
		return nil, err
	}
	return t.setupProjects(ctx)
}

func (c *Client) SetupStatuses(ctx context.Context, project string) ([]string, error) {
	t, err := c.setupTracker()
	if err != nil {
		return nil, err
	}
	return t.setupStatuses(ctx, project)
}

func (c *Client) SetupPeople(ctx context.Context, project string) ([]SetupPerson, error) {
	t, err := c.setupTracker()
	if err != nil {
		return nil, err
	}
	if source, ok := t.(interface {
		setupPeople(context.Context, string) ([]SetupPerson, error)
	}); ok {
		return source.setupPeople(ctx, project)
	}
	return nil, nil
}

func (c *Client) SetupRepos(ctx context.Context, name string) ([]SetupRepo, error) {
	conn, err := c.connection(name)
	if err != nil {
		return nil, err
	}
	source, ok := conn.(interface {
		setupRepos(context.Context) ([]SetupRepo, error)
	})
	if !ok {
		return nil, fmt.Errorf("%s does not support repository discovery", name)
	}
	return source.setupRepos(ctx)
}

func (c jiraTracker) setupProjects(ctx context.Context) ([]SetupProject, error) {
	base, auth, err := c.jira(ctx, c.name)
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
func (c jiraTracker) setupStatuses(ctx context.Context, project string) ([]string, error) {
	base, auth, err := c.jira(ctx, c.name)
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
func (c jiraTracker) setupPeople(ctx context.Context, project string) ([]SetupPerson, error) {
	base, auth, err := c.jira(ctx, c.name)
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

func (c githubTracker) setupProjects(ctx context.Context) ([]SetupProject, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	const query = `query($org: String!, $after: String) {
	  organization(login: $org) {
	    projectsV2(first: 50, after: $after) {
	      nodes { number title closed }
	      pageInfo { hasNextPage endCursor }
	    }
	  }
	}`
	var projects []SetupProject
	org := c.Workspace.Providers[c.name].Org
	var after any
	cursors := map[string]bool{}
	for {
		var response struct {
			Organization *struct {
				ProjectsV2 *graphConnection[struct {
					Number int
					Title  string
					Closed bool
				}]
			}
		}
		if err := c.graph(ctx, token, query, map[string]any{"org": org, "after": after}, &response); err != nil {
			return nil, err
		}
		if response.Organization == nil || response.Organization.ProjectsV2 == nil {
			return nil, errors.New("could not read projects for the organisation")
		}
		page := response.Organization.ProjectsV2
		for _, project := range page.Nodes {
			if project.Number > 0 && !project.Closed {
				projects = append(projects, SetupProject{Key: strconv.Itoa(project.Number), Name: project.Title})
			}
		}
		cursor, err := nextCursor(page.PageInfo, cursors)
		if err != nil {
			return nil, err
		}
		if cursor == "" {
			return projects, nil
		}
		after = cursor
	}
}

func (c githubTracker) setupStatuses(ctx context.Context, project string) ([]string, error) {
	number, err := strconv.Atoi(project)
	if err != nil || number < 1 {
		return nil, errors.New("choose a project with a valid project number")
	}
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	const query = `query($org: String!, $project: Int!) {
	  organization(login: $org) {
	    projectV2(number: $project) {
	      field(name: "Status") { ... on ProjectV2SingleSelectField { options { name } } }
	    }
	  }
	}`
	result, err := readProject[struct {
		Field struct{ Options []struct{ Name string } }
	}](ctx, c, token, query, map[string]any{"project": number}, 2)
	if err != nil {
		return nil, err
	}
	statuses := make([]string, 0, len(result.Field.Options))
	for _, option := range result.Field.Options {
		if option.Name != "" {
			statuses = append(statuses, option.Name)
		}
	}
	if len(statuses) == 0 {
		return nil, fmt.Errorf("project #%d has no Status options", number)
	}
	return statuses, nil
}

func (c githubTracker) setupRepos(ctx context.Context) ([]SetupRepo, error) {
	return githubCodeHost{Client: c.Client, name: c.name}.setupRepos(ctx)
}

func (c githubCodeHost) setupRepos(ctx context.Context) ([]SetupRepo, error) {
	p := c.Workspace.Providers[c.name]
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return nil, err
	}
	p.Repos = workfile.Names{"all"}
	names, _, err := c.repositories(ctx, p, token)
	if err != nil {
		return nil, err
	}
	repos := make([]SetupRepo, len(names))
	for i, name := range names {
		repos[i] = SetupRepo{name}
	}
	return repos, nil
}
