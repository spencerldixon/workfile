package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"workfile/internal/workfile"
)

// listingFields is all a ticket list shows. Tickets reads far more, and can
// read label history in bulk, so a list of the whole scope must not use it.
var listingFields = []string{"summary", "status", "assignee", "issuetype"}

// Listing reads the little a ticket list needs, for every ticket in scope.
func (c jiraTracker) Listing(ctx context.Context, scope string) ([]workfile.Listing, error) {
	name := c.name
	p := c.Workspace.Providers[name]
	base, auth, err := c.jira(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if scope != "" {
		p.Scope = scope
	}
	// only_mine does not apply here: the list has its own Assigned to me tab, and
	// needs other people's and unassigned tickets for the rest.
	condition, order := jiraSearch(p), jiraOrder(p)
	issues, err := c.search(ctx, name, base, auth, "("+condition+") ORDER BY "+order, listingFields, 0)
	if err != nil {
		return nil, err
	}
	items := make([]workfile.Listing, 0, len(issues))
	for _, issue := range issues {
		f := issue.Fields
		items = append(items, workfile.Listing{Key: issue.Key, Title: f.Summary, Status: f.Status.Name, State: jiraState(p, f.Status.Name), Assignee: f.Assignee.DisplayName, AssigneeID: f.Assignee.AccountID, Type: f.IssueType.Name, URL: "https://" + p.Site + "/browse/" + url.PathEscape(issue.Key)})
	}
	return items, nil
}

// jiraComments is a page of comments, as a ticket embeds them or as the comment
// endpoint returns them.
type jiraComments struct {
	Total    int
	Comments []struct {
		Author struct {
			DisplayName string `json:"displayName"`
		}
		Created string
		Body    json.RawMessage
	}
}

// Detail reads one ticket's description, comments and attachments.
func (c jiraTracker) Detail(ctx context.Context, key string) (workfile.Detail, error) {
	name := c.name
	base, auth, err := c.jira(ctx, name)
	if err != nil {
		return workfile.Detail{}, fmt.Errorf("%s: %w", name, err)
	}
	var issue struct {
		Fields struct {
			Description json.RawMessage
			Comment     jiraComments
			Attachment  []struct{ ID, Filename string }
		}
	}
	endpoint := base + "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=description,comment,attachment"
	if err := c.request(ctx, "GET", endpoint, auth, nil, &issue); err != nil {
		return workfile.Detail{}, fmt.Errorf("%s: %w", name, err)
	}
	f := issue.Fields
	files := make([]jiraFile, len(f.Attachment))
	for i, a := range f.Attachment {
		files[i] = jiraFile{a.ID, a.Filename}
	}
	// A ticket embeds only some of a long thread, and not the newest. One more
	// read gets those. If it fails, the ticket is still worth showing.
	comments := f.Comment
	if comments.Total > len(comments.Comments) {
		var newest jiraComments
		endpoint := base + "/rest/api/3/issue/" + url.PathEscape(key) + "/comment?orderBy=-created"
		if c.request(ctx, "GET", endpoint, auth, nil, &newest) == nil && len(newest.Comments) > 0 {
			comments = newest
		}
	}
	d := workfile.Detail{Key: key, Body: matchImages(document(f.Description), files), CommentTotal: comments.Total}
	for _, comment := range comments.Comments {
		// A date Jira writes some other way is left out, not an error.
		created, _ := time.Parse("2006-01-02T15:04:05.000-0700", comment.Created)
		d.Comments = append(d.Comments, workfile.Comment{Author: comment.Author.DisplayName, Created: created, Body: matchImages(document(comment.Body), files)})
	}
	return d, nil
}

// jiraFile is a file attached to a ticket.
type jiraFile struct{ id, name string }

// matchImages swaps each image block's media id, which document leaves in
// Attachment, for the id of the file it shows. Jira's image nodes use a media
// id that is not the attachment id, so the file name usually decides. An image
// with no file keeps its name and loses its Attachment.
func matchImages(blocks []workfile.Block, files []jiraFile) []workfile.Block {
	for i := range blocks {
		b := &blocks[i]
		if b.Kind != workfile.BlockImage {
			continue
		}
		media := b.Attachment
		b.Attachment = ""
		for _, f := range files {
			if f.id == media || b.Name != "" && f.name == b.Name {
				b.Attachment = f.id
				if b.Name == "" {
					b.Name = f.name
				}
				break
			}
		}
	}
	return blocks
}

var attachmentID = regexp.MustCompile(`^[0-9]+$`)

// Image downloads one attachment. The address is built from the id alone, never
// from a URL in a response. Jira redirects to its file store by default, and
// this client follows no redirects, so redirect=false asks Jira for the bytes
// itself. The token never reaches another host.
func (c jiraTracker) Image(ctx context.Context, id string) ([]byte, error) {
	if !attachmentID.MatchString(id) {
		return nil, errors.New("not an attachment id")
	}
	name := c.name
	base, auth, err := c.jira(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return c.download(ctx, base+"/rest/api/3/attachment/content/"+id+"?redirect=false", auth)
}
