package provider

import (
	"context"
	"errors"
	"fmt"
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
	repos, err := c.repositories(ctx, p, token)
	if err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return identity + " · connected; no visible active repositories", nil
	}
	_, err = c.searchPRs(ctx, token, fmt.Sprintf("org:%s is:pr archived:false", p.Org), true)
	return fmt.Sprintf("%s · %s · %d visible active repositories", identity, p.Org, len(repos)), err
}
