package workfile

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var githubKeyPattern = regexp.MustCompile(`^[a-z0-9._-]+#([0-9]+)$`)
var githubMentionPattern = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9._-]*#[0-9]+`)
var githubIssuePath = regexp.MustCompile(`^/([^/]+)/([^/]+)/issues/([0-9]+)$`)
var githubRepoPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// githubKeys is Keys for GitHub issues, whose tickets look like app#42: the
// repository, then the number, in lower case.
type githubKeys struct{ org string }

// Parse lowers a key. A bare number, with or without #, is completed with the
// project.
func (githubKeys) Parse(input, project string) (string, error) {
	key := strings.ToLower(input)
	if bare := strings.TrimPrefix(key, "#"); numberPattern.MatchString(bare) {
		if project == "" {
			return "", fmt.Errorf("%s is not a ticket key; use the full key (for example app#%s) or set default_project in providers.yml", input, bare)
		}
		key = project + "#" + bare
	}
	match := githubKeyPattern.FindStringSubmatch(key)
	if match == nil {
		return "", fmt.Errorf("%s is not a ticket key (expected app#42 or a number)", input)
	}
	// GitHub reads numbers as 32 bits, so a longer one could never be an issue.
	number, err := strconv.Atoi(match[1])
	if err != nil || number <= 0 || number > math.MaxInt32 {
		return "", fmt.Errorf("%s is not a ticket key (%s is not an issue number)", input, match[1])
	}
	repo, _, _ := strings.Cut(key, "#")
	return repo + "#" + strconv.Itoa(number), nil
}

// Search is the text a code host searches for.
func (githubKeys) Search(key string) string { return key }

// Mentions finds full keys in text. A bare #42 is left out, because it matches
// too loosely. The boundary avoids linking app#42 to app#42x.
func (k githubKeys) Mentions(text string) []string {
	var keys []string
	for _, match := range githubMentionPattern.FindAllStringIndex(text, -1) {
		if match[1] < len(text) && isWordByte(text[match[1]]) {
			continue
		}
		key, err := k.Parse(text[match[0]:match[1]], "")
		if err == nil && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// Links finds keys in addresses of this organisation's issues.
func (k githubKeys) Links(body string) []string {
	var keys []string
	for _, raw := range webLink.FindAllString(body, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ").,;]"))
		if err != nil || !strings.EqualFold(u.Host, "github.com") || u.User != nil {
			continue
		}
		match := githubIssuePath.FindStringSubmatch(u.Path)
		if match == nil || !strings.EqualFold(match[1], k.org) {
			continue
		}
		if key, err := k.Parse(match[2]+"#"+match[3], ""); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func checkGitHubTracker(name string, p Provider, add func(string, ...any)) {
	checkGitHubOrg(name, p, add)
	if p.Project <= 0 {
		add("providers.yml: %s.project must be the number of a GitHub project", name)
	}
	if len(p.Repos) == 0 {
		add("providers.yml: %s.repos is required: list the repositories the team's issues are in, such as repos: [web, api], or write repos: all", name)
	}
	checkRepoNames(name, p, add)
	checkKeepDoneFor(name, p, add)
	if p.DefaultProject != "" && !githubRepoPattern.MatchString(p.DefaultProject) {
		add("providers.yml: %s.default_project must be a repository name in lower case, such as app", name)
	}
}
