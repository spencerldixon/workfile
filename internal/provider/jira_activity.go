package provider

import (
	"context"
	"fmt"
	"math"
	"time"

	"workfile/internal/workfile"
)

// Activity reads comments added to these tickets after since, in one search
// for the tickets updated since then. No keys means no request.
func (c jiraTracker) Activity(ctx context.Context, keys []string, since time.Time) ([]workfile.Activity, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	base, auth, err := c.jira(ctx, c.name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.name, err)
	}
	// Jira reads absolute JQL dates in the person's own time zone, so ask in
	// minutes relative to now, with two to spare. Comments are then kept from
	// a minute before since, so one indexed late is not lost; the inbox drops
	// any it already has.
	minutes := int(math.Ceil(c.Now().Sub(since).Minutes())) + 2
	jql := fmt.Sprintf(`key in (%s) AND updated >= "-%dm"`, quotedKeys(keys), max(minutes, 2))
	keepFrom := since.Add(-time.Minute)
	body := map[string]any{"jql": jql, "fields": []string{"comment"}, "maxResults": 100}
	var page struct {
		Issues []struct {
			Key    string
			Fields struct{ Comment jiraComments }
		}
	}
	if err := c.request(ctx, "POST", base+"/rest/api/3/search/jql", auth, body, &page); err != nil {
		return nil, fmt.Errorf("%s: %w", c.name, err)
	}
	site := c.Workspace.Providers[c.name].Site
	var events []workfile.Activity
	for _, issue := range page.Issues {
		for _, comment := range issue.Fields.Comment.Comments {
			created, err := time.Parse("2006-01-02T15:04:05.000-0700", comment.Created)
			if err != nil || !created.After(keepFrom) {
				continue
			}
			events = append(events, workfile.Activity{
				Connection: c.name, Ticket: issue.Key, Kind: workfile.ActivityComment,
				Who: comment.Author.DisplayName, WhoID: comment.Author.AccountID, At: created,
				URL: "https://" + site + "/browse/" + issue.Key,
			})
		}
	}
	return events, nil
}
