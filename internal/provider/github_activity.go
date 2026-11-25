package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"workfile/internal/workfile"
)

// changeBatch is how many PRs one change check asks about.
const changeBatch = 100

// activityText caps how much of a comment is kept for the inbox.
const activityText = 200

var safeRepo = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Activity reads what happened on these PRs since each cursor, in two steps.
// First one small query per 100 PRs asks only when each was last updated.
// Then only PRs updated after their cursor have their timeline read. GraphQL
// has no free conditional requests, so this keeps a quiet poll to one call.
func (c githubCodeHost) Activity(ctx context.Context, prs []workfile.PRCursor) (workfile.ActivityResult, error) {
	result := workfile.ActivityResult{Updated: map[string]time.Time{}}
	if len(prs) == 0 {
		return result, nil
	}
	p := c.Workspace.Providers[c.name]
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return result, err
	}
	var changed []workfile.PRCursor
	for start := 0; start < len(prs); start += changeBatch {
		batch := prs[start:min(start+changeBatch, len(prs))]
		var b strings.Builder
		b.WriteString("query {\n")
		aliases := map[string]workfile.PRCursor{}
		for i, pr := range batch {
			repo, number, ok := splitRecordID(pr.ID)
			if !ok {
				continue
			}
			alias := "p" + strconv.Itoa(i)
			aliases[alias] = pr
			fmt.Fprintf(&b, "%s: repository(owner: %q, name: %q) { pullRequest(number: %d) { updatedAt } }\n", alias, p.Org, repo, number)
		}
		b.WriteString("}")
		if len(aliases) == 0 {
			continue
		}
		var response map[string]*struct {
			PullRequest *struct{ UpdatedAt time.Time }
		}
		if err := c.graphPartial(ctx, token, b.String(), nil, 2, &response); err != nil {
			return result, err
		}
		for alias, pr := range aliases {
			repo := response[alias]
			if repo == nil || repo.PullRequest == nil {
				continue
			}
			result.Updated[pr.ID] = repo.PullRequest.UpdatedAt
			if repo.PullRequest.UpdatedAt.After(pr.Seen) {
				changed = append(changed, pr)
			}
		}
	}
	lists, err := parallel(ctx, len(changed), func(ctx context.Context, i int) ([]workfile.Activity, error) {
		return c.timeline(ctx, token, p.Org, changed[i])
	})
	if err != nil {
		return result, err
	}
	for _, events := range lists {
		result.Events = append(result.Events, events...)
	}
	return result, nil
}

const timelineQuery = `query($owner: String!, $name: String!, $number: Int!, $since: DateTime) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) {
    timelineItems(since: $since, last: 50, itemTypes: [ISSUE_COMMENT, PULL_REQUEST_REVIEW, REVIEW_REQUESTED_EVENT, PULL_REQUEST_COMMIT]) {
      nodes {
        __typename
        ... on IssueComment { author { login } createdAt body url }
        ... on PullRequestReview { author { login } submittedAt state body url comments(first: 1) { nodes { path line body } } }
        ... on ReviewRequestedEvent { actor { login } createdAt }
        ... on PullRequestCommit { url commit { committedDate author { user { login } } } }
      }
    }
  } }
}`

type timelineItem struct {
	Typename    string `json:"__typename"`
	Author      *login
	Actor       *login
	CreatedAt   time.Time
	SubmittedAt time.Time
	State       string
	Body, URL   string
	Comments    struct {
		Nodes []struct {
			Path string
			Line int
			Body string
		}
	}
	Commit *struct {
		CommittedDate time.Time
		Author        struct{ User *login }
	}
}

func (c githubCodeHost) timeline(ctx context.Context, token, org string, pr workfile.PRCursor) ([]workfile.Activity, error) {
	repo, number, ok := splitRecordID(pr.ID)
	if !ok {
		return nil, nil
	}
	variables := map[string]any{"owner": org, "name": repo, "number": number}
	if !pr.Seen.IsZero() {
		variables["since"] = pr.Seen.UTC().Format(time.RFC3339)
	}
	var response struct {
		Repository *struct {
			PullRequest *struct {
				TimelineItems struct{ Nodes []timelineItem }
			}
		}
	}
	if err := c.graph(ctx, token, timelineQuery, variables, &response); err != nil {
		return nil, err
	}
	if response.Repository == nil || response.Repository.PullRequest == nil {
		return nil, nil
	}
	var events []workfile.Activity
	for _, item := range response.Repository.PullRequest.TimelineItems.Nodes {
		e := workfile.Activity{Connection: c.name, PR: pr.ID, URL: item.URL}
		who := item.Author
		switch item.Typename {
		case "IssueComment":
			e.Kind, e.At, e.Text = workfile.ActivityComment, item.CreatedAt, item.Body
		case "PullRequestReview":
			e.At, e.Text = item.SubmittedAt, item.Body
			switch item.State {
			case "APPROVED":
				e.Kind = workfile.ActivityApproved
			case "CHANGES_REQUESTED":
				e.Kind = workfile.ActivityChangesRequested
			case "COMMENTED":
				e.Kind = workfile.ActivityReview
			default:
				continue // pending or dismissed reviews are not news
			}
			if first := item.Comments.Nodes; len(first) > 0 {
				if e.Text == "" {
					e.Text = first[0].Body
				}
				if first[0].Path != "" {
					e.Where = first[0].Path
					if first[0].Line > 0 {
						e.Where += ":" + strconv.Itoa(first[0].Line)
					}
				}
			}
		case "ReviewRequestedEvent":
			e.Kind, e.At, who = workfile.ActivityReviewRequested, item.CreatedAt, item.Actor
		case "PullRequestCommit":
			if item.Commit == nil {
				continue
			}
			e.Kind, e.At, who = workfile.ActivityPushed, item.Commit.CommittedDate, item.Commit.Author.User
		default:
			continue
		}
		if who == nil || who.Login == "" || !e.At.After(pr.Seen) {
			continue
		}
		e.Who, e.WhoID = who.Login, who.Login
		e.Text = trimText(e.Text)
		events = append(events, e)
	}
	return events, nil
}

// splitRecordID reads a record ID such as "app#84".
func splitRecordID(id string) (string, int, bool) {
	repo, num, found := strings.Cut(id, "#")
	number, err := strconv.Atoi(num)
	if !found || err != nil || number <= 0 || !safeRepo.MatchString(repo) {
		return "", 0, false
	}
	return repo, number, true
}

func trimText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > activityText {
		return string(r[:activityText-1]) + "…"
	}
	return s
}

// mineLimit caps each search: the inbox is for work in flight, not history.
const mineLimit = 50

func (c githubCodeHost) InboxPRs(ctx context.Context, reviews bool, repos []string) ([]workfile.Record, error) {
	p := c.Workspace.Providers[c.name]
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return nil, err
	}
	search := strings.Replace(searchQuery, discoveryFields, prFields+" body", 1)
	keys := c.Workspace.TrackerKeys()
	var records []workfile.Record
	seen := map[string]bool{}
	scopes := []string{fmt.Sprintf("author:@me org:%s", p.Org)}
	if reviews {
		scopes = append(scopes, fmt.Sprintf("review-requested:@me org:%s", p.Org))
	}
	for _, repo := range repos {
		if listedRepo(p, repo) {
			scopes = append(scopes, fmt.Sprintf("repo:%s/%s", p.Org, repo))
		}
	}
	for _, scope := range scopes {
		query := "is:pr is:open " + scope
		var response struct {
			Search *struct{ Nodes []*pullRequest }
		}
		if err := c.graph(ctx, token, search, map[string]any{"q": query, "after": nil, "first": mineLimit}, &response); err != nil {
			return nil, err
		}
		if response.Search == nil {
			return nil, fmt.Errorf("GitHub returned no PR search results")
		}
		for _, pr := range response.Search.Nodes {
			if pr == nil || pr.State != "OPEN" || !listedRepo(p, pr.Repository.Name) {
				continue
			}
			id := fmt.Sprintf("%s#%d", pr.Repository.Name, pr.Number)
			if seen[id] {
				continue
			}
			if err := c.completePR(ctx, token, pr); err != nil {
				return nil, err
			}
			r := pr.record()
			r.Repository = p.Org + "/" + pr.Repository.Name
			r.TicketKeys = pr.ticketKeys(keys)
			seen[id] = true
			records = append(records, r)
		}
	}
	return records, nil
}
