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
	warning := ""
	if len(missing) > 0 {
		verb := "is"
		if len(missing) > 1 {
			verb = "are"
		}
		warning = fmt.Sprintf(" · warning: github.repos lists %s, which %s not in %s or %s archived", strings.Join(missing, ", "), verb, p.Org, verb)
	}
	if len(repos) == 0 {
		return identity + " · connected; no visible active repositories" + warning, nil
	}
	_, err = c.searchPRs(ctx, token, fmt.Sprintf("org:%s is:pr archived:false", p.Org), true)
	return fmt.Sprintf("%s · %s · %d visible active repositories%s", identity, p.Org, len(repos), warning), err
}
