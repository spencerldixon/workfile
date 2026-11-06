package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"workfile/internal/workfile"
)

// githubTracker reads tickets from GitHub Issues. Status comes from a GitHub
// Project's Status field, never from the issue's open or closed state, which
// does not follow the board.
type githubTracker struct {
	*Client
	name string
}

// viewMemory is how long a view's filter and the tickets it shows are remembered.
// Opening ticket after ticket reuses them, and a long run such as wf watch still
// sees the view change.
const viewMemory = 5 * time.Minute

// issuesPerRead is how many issues one aliased key read asks for.
const issuesPerRead = 50

const githubStatusField = `fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } }`

// A list shows little, so it asks for little. A view check needs only the key.
const (
	githubKeyFields     = `number repository { name }`
	githubListingFields = githubKeyFields + ` title url assignees(first: 1) { nodes { login name } } issueType { name }`
	githubTicketFields  = githubListingFields + ` body labels(first: 100) { nodes { name } }
	  closedByPullRequestsReferences(first: 20) { nodes { number repository { name owner { login } } } }`
)

func githubItemsQuery(fields string) string {
	return `query($org: String!, $project: Int!, $after: String, $query: String) {
	  organization(login: $org) {
	    projectV2(number: $project) {
	      items(first: 100, after: $after, query: $query) {
	        pageInfo { hasNextPage endCursor }
	        nodes { type isArchived ` + githubStatusField + ` content { ... on Issue { ` + fields + ` } } }
	      }
	    }
	  }
	}`
}

// githubIssuesQuery reads count issues by key, each under its own alias. Keys
// travel as variables, so their text never becomes part of the query.
func githubIssuesQuery(count int) string {
	var query strings.Builder
	query.WriteString("query($org: String!")
	for i := range count {
		fmt.Fprintf(&query, ", $r%d: String!, $n%d: Int!", i, i)
	}
	query.WriteString(") {\n")
	for i := range count {
		fmt.Fprintf(&query, "  i%d: repository(owner: $org, name: $r%d) { issue(number: $n%d) { ...ticket } }\n", i, i, i)
	}
	query.WriteString("}\nfragment ticket on Issue { " + githubTicketFields + `
	  projectItems(first: 100) {
	    pageInfo { hasNextPage }
	    nodes { isArchived project { number owner { ... on Organization { login } } } ` + githubStatusField + ` }
	  }
	}`)
	return query.String()
}

type githubStatus struct{ Name string }

type githubAssignee struct{ Login, Name string }

// displayName is the name people read, or the login if they set none.
func (a githubAssignee) displayName() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Login
}

type githubIssue struct {
	Number                         int
	Title, Body, URL               string
	Repository                     struct{ Name string }
	Assignees                      graphConnection[githubAssignee]
	Labels                         *graphConnection[label]
	IssueType                      *struct{ Name string }
	ClosedByPullRequestsReferences graphConnection[struct {
		Number     int
		Repository struct {
			Name  string
			Owner login
		}
	}]
	ProjectItems graphConnection[struct {
		IsArchived bool
		Project    struct {
			Number int
			Owner  login
		}
		FieldValueByName *githubStatus
	}]
}

func (i githubIssue) key() string {
	return strings.ToLower(i.Repository.Name) + "#" + strconv.Itoa(i.Number)
}

func (i githubIssue) assignee() githubAssignee {
	if len(i.Assignees.Nodes) > 0 {
		return i.Assignees.Nodes[0]
	}
	return githubAssignee{}
}

// githubItem is one entry on a project's board.
type githubItem struct {
	Type             string
	IsArchived       bool
	FieldValueByName *githubStatus
	Content          githubIssue
}

func (i githubItem) status() string { return statusName(i.FieldValueByName) }

func statusName(s *githubStatus) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func (c githubTracker) token() (string, error) {
	token, err := c.Credentials.Get(c.Workspace.Providers[c.name].Auth["token"])
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.name, err)
	}
	return token, nil
}

// readProject reads one thing from the configured project. depth is how far into
// the response a NOT_FOUND may be, for parts the caller checks for itself.
func readProject[T any](ctx context.Context, c githubTracker, token, query string, variables map[string]any, depth int) (T, error) {
	var none T
	p := c.Workspace.Providers[c.name]
	sent := map[string]any{"org": p.Org, "project": p.Project}
	for name, value := range variables {
		sent[name] = value
	}
	var response struct {
		Organization *struct{ ProjectV2 *T }
	}
	if err := c.graphPartial(ctx, token, query, sent, depth, &response); err != nil {
		return none, err
	}
	if response.Organization == nil || response.Organization.ProjectV2 == nil {
		return none, fmt.Errorf("cannot find project %d in %s; check the project and org settings", p.Project, p.Org)
	}
	return *response.Organization.ProjectV2, nil
}

// items reads the live issues of the project, up to limit when set. Drafts, PRs
// and items the token cannot see have no issue, so they are skipped.
func (c githubTracker) items(ctx context.Context, token, fields, query string, limit int) ([]githubItem, error) {
	var found []githubItem
	var after any
	seen := map[string]bool{}
	for {
		variables := map[string]any{"after": after}
		if query != "" {
			variables["query"] = query
		}
		page, err := readProject[struct{ Items graphConnection[githubItem] }](ctx, c, token, githubItemsQuery(fields), variables, 2)
		if err != nil {
			return nil, err
		}
		if page.Items.Nodes == nil {
			return nil, errors.New("GitHub returned no project items")
		}
		for _, item := range page.Items.Nodes {
			if item.Type != "ISSUE" || item.IsArchived {
				continue
			}
			if item.Content.Number == 0 || item.Content.Repository.Name == "" {
				return nil, errors.New("GitHub returned an issue without a number or repository")
			}
			found = append(found, item)
			if limit > 0 && len(found) == limit {
				return found, nil
			}
		}
		cursor, err := nextCursor(page.Items.PageInfo, seen)
		if err != nil {
			return nil, err
		}
		if cursor == "" {
			return found, nil
		}
		after = cursor
	}
}

// githubView is what is remembered of a saved view. A filter of "" means the whole
// project. shown is nil until something needs it, and is never changed once set.
type githubView struct {
	read   time.Time
	filter string
	shown  map[string]bool
}

// view is the remembered view, read again if it is too old. A failed read
// remembers nothing. The caller holds viewLock.
func (c githubTracker) view(ctx context.Context, token string) (*githubView, error) {
	if view := c.views[c.name]; view != nil && c.Now().Sub(view.read) < viewMemory {
		return view, nil
	}
	p := c.Workspace.Providers[c.name]
	const query = `query($org: String!, $project: Int!, $view: Int!) {
	  organization(login: $org) { projectV2(number: $project) { view(number: $view) { filter } } }
	}`
	project, err := readProject[struct{ View *struct{ Filter string } }](ctx, c, token, query, map[string]any{"view": p.View}, 3)
	if err != nil {
		return nil, err
	}
	if project.View == nil {
		return nil, fmt.Errorf("cannot find view %d on project %d; check the view setting", p.View, p.Project)
	}
	view := &githubView{read: c.Now(), filter: strings.TrimSpace(project.View.Filter)}
	c.views[c.name] = view
	return view, nil
}

// viewFilter is the filter of the configured view, or "" for the whole project.
// A view with an empty or null filter means the whole project too.
func (c githubTracker) viewFilter(ctx context.Context, token string) (string, error) {
	if c.Workspace.Providers[c.name].View == 0 {
		return "", nil
	}
	c.viewLock.Lock()
	defer c.viewLock.Unlock()
	view, err := c.view(ctx, token)
	if err != nil {
		return "", err
	}
	return view.filter, nil
}

// viewShown is the keys the configured view shows, or nil if it hides nothing.
// Only a read of the whole view can tell, so it is remembered with the filter.
func (c githubTracker) viewShown(ctx context.Context, token string) (map[string]bool, error) {
	if c.Workspace.Providers[c.name].View == 0 {
		return nil, nil
	}
	c.viewLock.Lock()
	defer c.viewLock.Unlock()
	view, err := c.view(ctx, token)
	if err != nil || view.filter == "" {
		return nil, err
	}
	if view.shown == nil {
		items, err := c.items(ctx, token, githubKeyFields, view.filter, 0)
		if err != nil {
			return nil, err
		}
		shown := make(map[string]bool, len(items))
		for _, item := range items {
			shown[item.Content.key()] = true
		}
		view.shown = shown
	}
	return view.shown, nil
}

// assignee is the filter value for the person's own account: me.<name> from
// config.yml when set, then me.github, otherwise whoever owns the token.
func (c githubTracker) assignee() (string, error) {
	me, err := workfile.Me()
	if err != nil {
		return "", err
	}
	if id := me[c.name]; id != "" {
		return id, nil
	}
	if id := me["github"]; id != "" {
		return id, nil
	}
	return "@me", nil
}

func (c githubTracker) Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	var tickets []workfile.Ticket
	if len(keys) > 0 {
		tickets, err = c.byKey(ctx, token, keys)
	} else {
		tickets, err = c.scope(ctx, token, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.name, err)
	}
	return tickets, nil
}

func (c githubTracker) scope(ctx context.Context, token string, limit int) ([]workfile.Ticket, error) {
	query, err := c.viewFilter(ctx, token)
	if err != nil {
		return nil, err
	}
	if c.Workspace.Settings.OnlyMine {
		who, err := c.assignee()
		if err != nil {
			return nil, err
		}
		query = strings.TrimSpace(query + " assignee:" + who)
	}
	items, err := c.items(ctx, token, githubTicketFields, query, limit)
	if err != nil {
		return nil, err
	}
	tickets := make([]workfile.Ticket, 0, len(items))
	for _, item := range items {
		tickets = append(tickets, c.ticket(item.Content.key(), item.Content, item.status(), ""))
	}
	return tickets, nil
}

// byKey reads the named issues even if the scope hides them. A key that does not
// exist is left out.
func (c githubTracker) byKey(ctx context.Context, token string, keys []string) ([]workfile.Ticket, error) {
	var wanted []string
	rules := c.Workspace.TrackerKeys()
	for _, input := range keys {
		key, err := rules.Parse(input, c.Workspace.Providers[c.name].DefaultProject)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(wanted, key) {
			wanted = append(wanted, key)
		}
	}
	batches := slices.Collect(slices.Chunk(wanted, issuesPerRead))
	read, err := parallel(ctx, len(batches), func(ctx context.Context, i int) ([]workfile.Ticket, error) {
		return c.readIssues(ctx, token, batches[i])
	})
	if err != nil {
		return nil, err
	}
	tickets := slices.Concat(read...)
	if err := c.markHidden(ctx, token, tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func (c githubTracker) readIssues(ctx context.Context, token string, keys []string) ([]workfile.Ticket, error) {
	variables := map[string]any{"org": c.Workspace.Providers[c.name].Org}
	for i, key := range keys {
		repo, number, _ := strings.Cut(key, "#")
		n, err := strconv.Atoi(number)
		if err != nil {
			return nil, fmt.Errorf("%s is not a ticket key", key)
		}
		variables[fmt.Sprintf("r%d", i)] = repo
		variables[fmt.Sprintf("n%d", i)] = n
	}
	var response map[string]*struct{ Issue *githubIssue }
	if err := c.graphPartial(ctx, token, githubIssuesQuery(len(keys)), variables, 2, &response); err != nil {
		return nil, err
	}
	var tickets []workfile.Ticket
	for i, key := range keys {
		if found := response[fmt.Sprintf("i%d", i)]; found != nil && found.Issue != nil {
			tickets = append(tickets, c.onProject(key, *found.Issue))
		}
	}
	return tickets, nil
}

// onProject turns an issue read by key into a ticket, using this project's item.
// The owner is compared too: an issue can sit on another owner's project with
// the same number.
func (c githubTracker) onProject(key string, issue githubIssue) workfile.Ticket {
	p := c.Workspace.Providers[c.name]
	for _, item := range issue.ProjectItems.Nodes {
		if item.Project.Number == p.Project && strings.EqualFold(item.Project.Owner.Login, p.Org) {
			t := c.ticket(key, issue, statusName(item.FieldValueByName), "")
			t.OutOfScope = item.IsArchived
			return t
		}
	}
	if issue.ProjectItems.PageInfo.HasNextPage {
		return c.ticket(key, issue, "", fmt.Sprintf("%s is on too many projects to find project %d", key, p.Project))
	}
	t := c.ticket(key, issue, "", fmt.Sprintf("%s is not on project %d", key, p.Project))
	t.OutOfScope = true
	return t
}

// markHidden sets OutOfScope on tickets the configured view filters out. It reads
// the view only if a ticket is left to check.
func (c githubTracker) markHidden(ctx context.Context, token string, tickets []workfile.Ticket) error {
	if !slices.ContainsFunc(tickets, func(t workfile.Ticket) bool { return !t.OutOfScope }) {
		return nil
	}
	shown, err := c.viewShown(ctx, token)
	if err != nil || shown == nil {
		return err
	}
	for i := range tickets {
		tickets[i].OutOfScope = tickets[i].OutOfScope || !shown[tickets[i].Key]
	}
	return nil
}

// ticket builds a ticket. problem is why its status cannot be known, if any.
func (c githubTracker) ticket(key string, issue githubIssue, status, problem string) workfile.Ticket {
	p := c.Workspace.Providers[c.name]
	assignee := issue.assignee()
	t := workfile.Ticket{Key: key, Title: issue.Title, URL: issue.URL, Assignee: assignee.Login, Unavailable: map[string]string{}}
	t.State = stateFor(p, status)
	switch {
	case problem != "":
		t.Error = problem
	case status == "":
		t.Error = fmt.Sprintf("%s has no Status on project %d", key, p.Project)
	case t.State == "":
		t.Error = fmt.Sprintf("GitHub status %q is not mapped in %s.statuses", status, c.name)
	}
	t.Summary = status
	if t.Summary == "" {
		t.Summary = "No status"
	}
	if assignee.Login == "" {
		t.Summary += " · unassigned"
	} else {
		t.Summary += " · assigned to " + assignee.displayName()
	}
	description, headings := markdownText(issue.Body)
	t.Facts = workfile.Facts{
		"summary": issue.Title, "description": description, "headings": headings,
		"assignee": assignee.displayName(), "project": issue.Repository.Name,
	}
	if issue.Labels == nil {
		t.Unavailable["labels"] = "GitHub did not return labels"
	} else {
		labels := make([]workfile.Actor, 0, len(issue.Labels.Nodes))
		for _, l := range issue.Labels.Nodes {
			labels = append(labels, workfile.Actor{Value: l.Name})
		}
		t.Facts["labels"] = labels
	}
	if issue.IssueType == nil {
		t.Unavailable["type"] = "GitHub has no issue type for " + key
	} else {
		t.Facts["type"] = issue.IssueType.Name
	}
	for _, pr := range issue.ClosedByPullRequestsReferences.Nodes {
		t.LinkedPRs = append(t.LinkedPRs, fmt.Sprintf("%s/%s#%d", pr.Repository.Owner.Login, pr.Repository.Name, pr.Number))
	}
	return t
}

// fenceMarker is the run of three or more backticks or tildes that opens line,
// or "" if it does not open a code fence.
func fenceMarker(line string) string {
	for _, mark := range []string{"`", "~"} {
		rest := strings.TrimLeft(line, mark)
		if size := len(line) - len(rest); size >= 3 {
			return line[:size]
		}
	}
	return ""
}

func (c githubTracker) Listing(ctx context.Context, scope string) ([]workfile.Listing, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	if scope == "" {
		if scope, err = c.viewFilter(ctx, token); err != nil {
			return nil, fmt.Errorf("%s: %w", c.name, err)
		}
	}
	items, err := c.items(ctx, token, githubListingFields, scope, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.name, err)
	}
	p := c.Workspace.Providers[c.name]
	listing := make([]workfile.Listing, 0, len(items))
	for _, item := range items {
		issue := item.Content
		entry := workfile.Listing{
			Key: issue.key(), Title: issue.Title, Status: item.status(), State: stateFor(p, item.status()),
			Assignee: issue.assignee().displayName(), AssigneeID: issue.assignee().Login, URL: issue.URL,
		}
		if issue.IssueType != nil {
			entry.Type = issue.IssueType.Name
		}
		listing = append(listing, entry)
	}
	return listing, nil
}

func (c githubTracker) Identity(ctx context.Context) (string, error) {
	return githubCodeHost{Client: c.Client, name: c.name}.Identity(ctx)
}

// Test proves the token can read the project, and the view if one is set.
func (c githubTracker) Test(ctx context.Context) (string, error) {
	identity, err := c.Identity(ctx)
	if err != nil {
		return "", err
	}
	token, err := c.token()
	if err != nil {
		return "", err
	}
	const query = `query($org: String!, $project: Int!) {
	  organization(login: $org) { projectV2(number: $project) { title items(first: 1) { totalCount } } }
	}`
	project, err := readProject[struct {
		Title string
		Items struct{ TotalCount int }
	}](ctx, c, token, query, nil, 2)
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.name, err)
	}
	if _, err := c.viewFilter(ctx, token); err != nil {
		return "", fmt.Errorf("%s: %w", c.name, err)
	}
	return fmt.Sprintf("%s · %s · %d items", identity, project.Title, project.Items.TotalCount), nil
}

// githubDetailQuery reads one issue's body, or its newest 100 comments.
const githubDetailQuery = `query($org: String!, $repo: String!, $number: Int!) {
	  repository(owner: $org, name: $repo) { issue(number: $number) { body } }
	}`

const githubCommentsQuery = `query($org: String!, $repo: String!, $number: Int!) {
	  repository(owner: $org, name: $repo) { issue(number: $number) {
	    comments(last: 100) { totalCount nodes { author { login } createdAt body } }
	  } }
	}`

// Detail reads one issue's text and its newest 100 comments. The comments are a
// second read: if it fails, the issue is still worth showing.
func (c githubTracker) Detail(ctx context.Context, key string) (workfile.Detail, error) {
	token, err := c.token()
	if err != nil {
		return workfile.Detail{}, err
	}
	p := c.Workspace.Providers[c.name]
	key, err = c.Workspace.TrackerKeys().Parse(key, p.DefaultProject)
	if err != nil {
		return workfile.Detail{}, err
	}
	repo, number, _ := strings.Cut(key, "#")
	n, _ := strconv.Atoi(number)
	variables := map[string]any{"org": p.Org, "repo": repo, "number": n}
	var body struct {
		Repository *struct{ Issue *struct{ Body string } }
	}
	if err := c.graphPartial(ctx, token, githubDetailQuery, variables, 2, &body); err != nil {
		return workfile.Detail{}, fmt.Errorf("%s: %w", c.name, err)
	}
	if body.Repository == nil || body.Repository.Issue == nil {
		return workfile.Detail{}, fmt.Errorf("%s: cannot find %s", c.name, key)
	}
	d := workfile.Detail{Key: key, Body: markdownBlocks(body.Repository.Issue.Body)}
	var thread struct {
		Repository *struct {
			Issue *struct {
				Comments struct {
					TotalCount int
					Nodes      []struct {
						Author    *struct{ Login string }
						CreatedAt string
						Body      string
					}
				}
			}
		}
	}
	if c.graphPartial(ctx, token, githubCommentsQuery, variables, 2, &thread) != nil || thread.Repository == nil || thread.Repository.Issue == nil {
		return d, nil
	}
	comments := thread.Repository.Issue.Comments
	d.CommentTotal = comments.TotalCount
	for _, node := range comments.Nodes {
		// A date or author GitHub leaves out is left empty, not an error.
		created, _ := time.Parse(time.RFC3339, node.CreatedAt)
		comment := workfile.Comment{Created: created, Body: markdownBlocks(node.Body)}
		if node.Author != nil {
			comment.Author = node.Author.Login
		}
		d.Comments = append(d.Comments, comment)
	}
	return d, nil
}

// githubImageHosts are the only hosts an image may come from. The address comes
// from issue text, so it must never decide where the token goes.
var githubImageHosts = []string{"github.com", "user-images.githubusercontent.com", "private-user-images.githubusercontent.com"}

// Image downloads one image a body named. The address is checked before any
// request: https only, no login or port, and the host must be exactly one of
// GitHub's own. The client follows no redirects, so the token stays on that host.
func (c githubTracker) Image(ctx context.Context, attachment string) ([]byte, error) {
	if strings.ContainsFunc(attachment, func(r rune) bool { return r <= ' ' || r == '\\' || r == 0x7f }) {
		return nil, errors.New("not an image address")
	}
	u, err := url.Parse(attachment)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host != u.Hostname() || !slices.Contains(githubImageHosts, strings.ToLower(u.Hostname())) {
		return nil, errors.New("not an image address on GitHub")
	}
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	return c.download(ctx, u.String(), "Bearer "+token)
}
