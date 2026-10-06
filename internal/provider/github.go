package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"workfile/internal/workfile"
)

type pageInfo struct {
	HasNextPage bool
	EndCursor   string
}
type connection[T any] struct {
	Nodes    []T
	PageInfo pageInfo
}
type login struct{ Login string }
type label struct{ Name string }
type review struct {
	State  string
	Author login
	Commit *struct{ OID string }
}
type reviewRequest struct{ RequestedReviewer login }
type commitStatus struct{ StatusCheckRollup json.RawMessage }
type commitNode struct{ Commit *commitStatus }
type pullRequest struct {
	ID, Title, URL, HeadRefName, State string
	Number                             int
	Body                               string
	IsDraft                            *bool
	HeadRefOID                         string
	Commits                            *connection[commitNode]
	Repository                         struct{ Name string }
	Author                             login
	Labels                             connection[label]
	LatestOpinionatedReviews           connection[review]
	ReviewRequests                     connection[reviewRequest]
}

const repositoryQuery = `query($org: String!, $after: String) {
  organization(login: $org) {
    repositories(first: 100, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { name isArchived }
    }
  }
}`

const prFields = `
  id number title url headRefName headRefOid state isDraft
  commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
  repository { name }
  author { login }
  labels(first: 100) { nodes { name } pageInfo { hasNextPage endCursor } }
  latestOpinionatedReviews(first: 100) { nodes { state author { login } commit { oid } } pageInfo { hasNextPage endCursor } }
  reviewRequests(first: 100) { nodes { requestedReviewer { ... on User { login } } } pageInfo { hasNextPage endCursor } }
`

// Discovery only needs the fields used to link a PR to a ticket. Fetch expensive
// label and review connections only for PRs that appear in the report.
const discoveryFields = `id title headRefName state body`

const searchQuery = `query($q: String!, $after: String, $first: Int!) {
  search(query: $q, type: ISSUE, first: $first, after: $after) {
    issueCount
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest { ` + discoveryFields + ` } }
  }
}`

func nextCursor(info pageInfo, seen map[string]bool) (string, error) {
	if !info.HasNextPage {
		return "", nil
	}
	if info.EndCursor == "" || seen[info.EndCursor] {
		return "", errors.New("GitHub returned a missing or repeated page cursor")
	}
	seen[info.EndCursor] = true
	return info.EndCursor, nil
}

func (c *Client) repositories(ctx context.Context, p workfile.Provider, token string) ([]string, error) {
	var names []string
	var after any
	seen := map[string]bool{}
	for {
		var response struct {
			Organization *struct {
				Repositories connection[struct {
					Name       string
					IsArchived bool
				}]
			}
		}
		if err := c.graph(ctx, token, repositoryQuery, map[string]any{"org": p.Org, "after": after}, &response); err != nil {
			return nil, err
		}
		if response.Organization == nil {
			return nil, errors.New("organisation not found or not visible to this token")
		}
		page := response.Organization.Repositories
		if page.Nodes == nil {
			return nil, errors.New("GitHub returned no repository list")
		}
		for _, repo := range page.Nodes {
			if !repo.IsArchived {
				names = append(names, repo.Name)
			}
		}
		cursor, err := nextCursor(page.PageInfo, seen)
		if err != nil {
			return nil, err
		}
		if cursor == "" {
			break
		}
		after = cursor
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func (c *Client) repositoryPRs(ctx context.Context, p workfile.Provider, token, repo string, probe bool) ([]pullRequest, int, error) {
	days, _ := strconv.Atoi(strings.TrimSuffix(p.Lookback, "d"))
	since := c.Now().AddDate(0, 0, -days).Format("2006-01-02")
	query := fmt.Sprintf("repo:%s/%s is:pr updated:>=%s", p.Org, repo, since)
	var after any
	var prs []pullRequest
	count := 0
	seen := map[string]bool{}
	for {
		var response struct {
			Search *struct {
				IssueCount int
				Nodes      []*pullRequest
				PageInfo   pageInfo
			}
		}
		first := 100
		search := searchQuery
		if probe {
			first = 1
			// Connection tests still exercise permissions for all PR facts.
			search = strings.Replace(search, discoveryFields, prFields, 1)
		}
		if err := c.graph(ctx, token, search, map[string]any{"q": query, "after": after, "first": first}, &response); err != nil {
			return nil, 0, err
		}
		if response.Search == nil || response.Search.Nodes == nil {
			return nil, 0, errors.New("GitHub returned no PR search results")
		}
		page := response.Search
		count = page.IssueCount
		if count > 1000 && !probe {
			return nil, count, fmt.Errorf("%s/%s has %d PRs in the lookback; GitHub search stops at 1000, so shorten lookback", p.Org, repo, count)
		}
		for _, pr := range page.Nodes {
			if pr != nil && (pr.State == "OPEN" || pr.State == "MERGED") {
				prs = append(prs, *pr)
			}
		}
		if probe {
			break
		}
		cursor, err := nextCursor(page.PageInfo, seen)
		if err != nil {
			return nil, 0, err
		}
		if cursor == "" {
			break
		}
		after = cursor
	}
	return prs, count, nil
}

// Each job owns one result slot. Workers share only immutable configuration and the HTTP client.
func parallel[T any](ctx context.Context, count int, run func(context.Context, int) (T, error)) ([]T, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]T, count)
	errs := make([]error, count)
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(6, count) {
		workers.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				results[i], errs[i] = run(ctx, i)
				if errs[i] != nil {
					cancel()
				}
			}
		})
	}
	for i := range count {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

// Boundaries avoid accidentally linking APP-12 to APP-123 or MYAPP-12.
var ticketKey = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*-[0-9]+`)

func linkedKeys(text string) []string {
	var keys []string
	for _, match := range ticketKey.FindAllStringIndex(text, -1) {
		word := func(c byte) bool {
			return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
		}
		if match[0] > 0 && word(text[match[0]-1]) || match[1] < len(text) && word(text[match[1]]) {
			continue
		}
		key := strings.ToUpper(text[match[0]:match[1]])
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func (c *Client) githubRecords(ctx context.Context, name string, tickets []workfile.Ticket) (map[string][]workfile.Record, []string, error) {
	p := c.Workspace.Providers[name]
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return nil, nil, err
	}
	repos, err := c.repositories(ctx, p, token)
	if err != nil {
		return nil, nil, err
	}
	wanted := map[string]bool{}
	for _, t := range tickets {
		wanted[strings.ToUpper(t.Key)] = true
	}
	type linkedPR struct {
		pr   pullRequest
		keys []string
	}
	results, err := parallel(ctx, len(repos), func(ctx context.Context, i int) ([]linkedPR, error) {
		prs, _, err := c.repositoryPRs(ctx, p, token, repos[i], false)
		if err != nil {
			return nil, err
		}
		var linked []linkedPR
		for _, pr := range prs {
			keys := linkedKeys(pr.Title + " " + pr.HeadRefName)
			keys = append(keys, explicitKeys(pr.Body, c.Workspace.Providers[c.Workspace.Policy.Tracker].Site)...)
			slices.Sort(keys)
			keys = slices.Compact(keys)
			keys = slices.DeleteFunc(keys, func(key string) bool { return !wanted[key] })
			if len(keys) == 0 {
				continue
			}
			linked = append(linked, linkedPR{pr: pr, keys: keys})
		}
		return linked, nil
	})
	if err != nil {
		return nil, nil, err
	}
	var matched []linkedPR
	for _, result := range results {
		matched = append(matched, result...)
	}
	// Use a single worker pool across repositories, including when every linked
	// PR belongs to the same repository. Keep result order independent of timing.
	records, err := parallel(ctx, len(matched), func(ctx context.Context, i int) (workfile.Record, error) {
		pr, err := c.readPR(ctx, token, matched[i].pr.ID)
		if err != nil {
			return workfile.Record{}, err
		}
		record := pr.record()
		record.Repository = p.Org + "/" + pr.Repository.Name
		return record, nil
	})
	if err != nil {
		return nil, nil, err
	}
	linked := map[string][]workfile.Record{}
	for i, record := range records {
		for _, key := range matched[i].keys {
			linked[key] = append(linked[key], record)
		}
	}
	var warnings []string
	if len(repos) == 0 {
		warnings = append(warnings, name+": no visible active repositories; check organisation access")
	}
	return linked, warnings, nil
}

func (c *Client) readPR(ctx context.Context, token, id string) (pullRequest, error) {
	if id == "" {
		return pullRequest{}, errors.New("GitHub returned a linked PR without an ID")
	}
	const query = `query($id: ID!) {
	  node(id: $id) { ... on PullRequest { ` + prFields + ` } }
	}`
	var response struct{ Node *pullRequest }
	if err := c.graph(ctx, token, query, map[string]any{"id": id}, &response); err != nil {
		return pullRequest{}, err
	}
	if response.Node == nil || response.Node.ID != id || response.Node.Repository.Name == "" || response.Node.Number == 0 {
		return pullRequest{}, errors.New("GitHub could not read a linked PR")
	}
	if err := c.completePR(ctx, token, response.Node); err != nil {
		return pullRequest{}, err
	}
	return *response.Node, nil
}

func (c *Client) completePR(ctx context.Context, token string, pr *pullRequest) error {
	if pr.Labels.Nodes == nil || pr.LatestOpinionatedReviews.Nodes == nil || pr.ReviewRequests.Nodes == nil {
		return errors.New("GitHub returned incomplete facts for a linked PR")
	}
	// Large PRs need further pages for labels, reviews, or review requests.
	const query = `query($id: ID!, $labels: String, $reviews: String, $requests: String) {
      node(id: $id) { ... on PullRequest {
        labels(first: 100, after: $labels) { nodes { name } pageInfo { hasNextPage endCursor } }
        latestOpinionatedReviews(first: 100, after: $reviews) { nodes { state author { login } commit { oid } } pageInfo { hasNextPage endCursor } }
        reviewRequests(first: 100, after: $requests) { nodes { requestedReviewer { ... on User { login } } } pageInfo { hasNextPage endCursor } }
      } }
    }`
	seen := map[string]map[string]bool{"labels": {}, "reviews": {}, "requests": {}}
	for pr.Labels.PageInfo.HasNextPage || pr.LatestOpinionatedReviews.PageInfo.HasNextPage || pr.ReviewRequests.PageInfo.HasNextPage {
		variables := map[string]any{"id": pr.ID}
		infos := map[string]pageInfo{"labels": pr.Labels.PageInfo, "reviews": pr.LatestOpinionatedReviews.PageInfo, "requests": pr.ReviewRequests.PageInfo}
		for name, info := range infos {
			cursor, err := nextCursor(info, seen[name])
			if err != nil {
				return err
			}
			if cursor != "" {
				variables[name] = cursor
			}
		}
		var response struct{ Node *pullRequest }
		if err := c.graph(ctx, token, query, variables, &response); err != nil {
			return err
		}
		if response.Node == nil {
			return errors.New("GitHub could not read a linked PR")
		}
		next := response.Node
		if infos["labels"].HasNextPage && next.Labels.Nodes == nil || infos["reviews"].HasNextPage && next.LatestOpinionatedReviews.Nodes == nil || infos["requests"].HasNextPage && next.ReviewRequests.Nodes == nil {
			return errors.New("GitHub returned an incomplete PR page")
		}
		if infos["labels"].HasNextPage {
			pr.Labels.Nodes = append(pr.Labels.Nodes, next.Labels.Nodes...)
			pr.Labels.PageInfo = next.Labels.PageInfo
		}
		if infos["reviews"].HasNextPage {
			pr.LatestOpinionatedReviews.Nodes = append(pr.LatestOpinionatedReviews.Nodes, next.LatestOpinionatedReviews.Nodes...)
			pr.LatestOpinionatedReviews.PageInfo = next.LatestOpinionatedReviews.PageInfo
		}
		if infos["requests"].HasNextPage {
			pr.ReviewRequests.Nodes = append(pr.ReviewRequests.Nodes, next.ReviewRequests.Nodes...)
			pr.ReviewRequests.PageInfo = next.ReviewRequests.PageInfo
		}
	}
	return nil
}

func (p pullRequest) record() workfile.Record {
	labels, approvers, reviewers := []string{}, []string{}, []string{}
	approvals := []workfile.Actor{}
	fresh := []workfile.Actor{}
	unavailable := map[string]string{}
	freshKnown := p.HeadRefOID != ""
	for _, l := range p.Labels.Nodes {
		labels = append(labels, l.Name)
	}
	for _, r := range p.LatestOpinionatedReviews.Nodes {
		if !slices.Contains([]string{"APPROVED", "CHANGES_REQUESTED", "DISMISSED", "COMMENTED", "PENDING"}, r.State) || r.State == "APPROVED" && r.Author.Login == "" {
			unavailable["approvals"] = "GitHub did not return complete reviewer evidence"
			freshKnown = false
		}
		if r.State == "APPROVED" && r.Author.Login != "" && !slices.Contains(approvers, r.Author.Login) {
			approvers = append(approvers, r.Author.Login)
			approvals = append(approvals, workfile.Actor{Value: r.Author.Login, ID: r.Author.Login})
			if r.Commit == nil || r.Commit.OID == "" {
				freshKnown = false
			} else if r.Commit.OID == p.HeadRefOID {
				fresh = append(fresh, workfile.Actor{Value: r.Author.Login, ID: r.Author.Login})
			}
		}
	}
	for _, r := range p.ReviewRequests.Nodes {
		if r.RequestedReviewer.Login != "" {
			reviewers = append(reviewers, r.RequestedReviewer.Login)
		}
	}
	state := strings.ToLower(p.State)
	parts := []string{state, "by " + p.Author.Login}
	if len(labels) > 0 {
		parts = append(parts, strings.Join(labels, ", "))
	}
	if len(approvers) > 0 {
		parts = append(parts, "approved by "+strings.Join(approvers, ", "))
	} else {
		parts = append(parts, "no approvals")
	}
	if len(reviewers) > 0 {
		parts = append(parts, "waiting on "+strings.Join(reviewers, ", "))
	}
	facts := workfile.Facts{"labels": labels, "approvals": approvals, "state": state, "author": p.Author.Login, "review_requests": reviewers}
	if p.Author.Login == "" {
		unavailable["author"] = "GitHub did not return a visible PR author"
	}
	if state != "open" && state != "merged" {
		unavailable["state"] = "GitHub did not return a supported PR state"
	}
	if p.IsDraft == nil {
		unavailable["draft"] = "GitHub did not return draft status"
	} else {
		facts["draft"] = strconv.FormatBool(*p.IsDraft)
	}
	if freshKnown {
		facts["fresh_approvals"] = fresh
	} else {
		unavailable["fresh_approvals"] = "GitHub did not return the commits needed to check approval freshness"
	}
	if status, reason := p.checkStatus(); reason != "" {
		unavailable["checks"] = reason
	} else {
		facts["checks"] = status
	}
	return workfile.Record{
		ID: fmt.Sprintf("%s#%d", p.Repository.Name, p.Number), URL: p.URL, Title: p.Title, Summary: strings.Join(parts, " · "),
		People: append([]string{p.Author.Login}, reviewers...),
		Facts:  facts, Unavailable: unavailable,
	}
}
