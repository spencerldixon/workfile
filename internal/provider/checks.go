package provider

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

func (p pullRequest) checkStatus() (string, string) {
	const missing = "GitHub did not return the current commit's check status"
	if p.Commits == nil || len(p.Commits.Nodes) != 1 || p.Commits.Nodes[0].Commit == nil {
		return "", missing
	}
	raw := p.Commits.Nodes[0].Commit.StatusCheckRollup
	if len(raw) == 0 {
		return "", missing
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return "none", ""
	}
	var rollup struct{ State string }
	if err := json.Unmarshal(raw, &rollup); err != nil {
		return "", "GitHub returned an unknown check status"
	}
	switch rollup.State {
	case "SUCCESS":
		return "success", ""
	case "PENDING", "EXPECTED":
		return "pending", ""
	case "FAILURE", "ERROR":
		return "failure", ""
	default:
		return "", "GitHub returned an unknown check status"
	}
}

// One read is bounded to 50 jobs. A PR with more still links to its checks page.
const failedChecksQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) {
    commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 50) { nodes {
      __typename
      ... on CheckRun { name conclusion detailsUrl }
      ... on StatusContext { context state targetUrl }
    } } } } } }
  } }
}`

// FailedChecks reads the check runs and statuses of a PR's latest commit and
// keeps the failing ones, in the order GitHub gives them.
func (c githubCodeHost) FailedChecks(ctx context.Context, record workfile.Record) ([]workfile.FailedCheck, error) {
	p := c.Workspace.Providers[c.name]
	repo, number, owner, ok := splitRef(p.Org, p.Org+"/"+record.ID)
	if !ok || !listedRepo(p, repo) {
		return nil, errors.New("this PR is not in the connection's repositories")
	}
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return nil, err
	}
	var response struct {
		Repository *struct {
			PullRequest *struct {
				Commits graphConnection[struct {
					Commit *struct {
						StatusCheckRollup *struct {
							Contexts graphConnection[struct {
								Typename                     string `json:"__typename"`
								Name, Conclusion, DetailsURL string
								Context, State, TargetURL    string
							}]
						}
					}
				}]
			}
		}
	}
	if err := c.graph(ctx, token, failedChecksQuery, map[string]any{"owner": owner, "name": repo, "number": number}, &response); err != nil {
		return nil, err
	}
	if response.Repository == nil || response.Repository.PullRequest == nil {
		return nil, errors.New("GitHub could not read this PR's checks")
	}
	commits := response.Repository.PullRequest.Commits.Nodes
	if len(commits) != 1 || commits[0].Commit == nil || commits[0].Commit.StatusCheckRollup == nil {
		return nil, nil
	}
	var failed []workfile.FailedCheck
	for _, n := range commits[0].Commit.StatusCheckRollup.Contexts.Nodes {
		switch {
		case n.Typename == "CheckRun" && slices.Contains([]string{"FAILURE", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED"}, n.Conclusion):
			failed = append(failed, workfile.FailedCheck{Name: n.Name, URL: n.DetailsURL})
		case n.Typename == "StatusContext" && (n.State == "FAILURE" || n.State == "ERROR"):
			failed = append(failed, workfile.FailedCheck{Name: n.Context, URL: n.TargetURL})
		}
	}
	return failed, nil
}
