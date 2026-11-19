package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// githubCodeHost reads pull requests from GitHub. Records is in github.go.
type githubCodeHost struct {
	*Client
	name string
}

func (c githubCodeHost) Identity(ctx context.Context) (string, error) {
	p := c.Workspace.Providers[c.name]
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

func (c githubCodeHost) Test(ctx context.Context) (string, error) {
	identity, err := c.Identity(ctx)
	if err != nil {
		return "", err
	}
	p := c.Workspace.Providers[c.name]
	token, err := c.Credentials.Get(p.Auth["token"])
	if err != nil {
		return "", err
	}
	repos, missing, err := c.repositories(ctx, p, token)
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("cannot verify repositories in %s: %s. Check the token's repository access, spelling, and archive status", p.Org, strings.Join(missing, ", "))
	}
	if len(repos) == 0 {
		return "", fmt.Errorf("no active repositories visible in %s. Check the token's repository access", p.Org)
	}
	_, err = c.searchPRs(ctx, token, fmt.Sprintf("org:%s is:pr archived:false", p.Org), true)
	return fmt.Sprintf("%s · %s · %d visible active repositories", identity, p.Org, len(repos)), err
}
