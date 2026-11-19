package provider

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

// SetupField describes one input needed to connect a provider.
type SetupField struct {
	Key, Title, Hint, Placeholder string
	Validate                      func(string) (string, error)
}

// SetupForm describes the connection fields and credential instructions for a provider.
type SetupForm struct {
	Fields                          []SetupField
	TokenPage, TokenLink, TokenHint string
}

const githubTokenURL = "https://github.com/settings/tokens/new?description=workfile&scopes="

var setupForms = map[string]SetupForm{
	"jira": {
		Fields: []SetupField{
			{"site", "Your Jira site", "Use the address where you open Jira.", "example-team.atlassian.net", setupSite},
			{"email", "Your Jira email", "Use the email address for your Atlassian account.", "alice@example.com", setupEmail},
			{Key: "token", Title: "Create a Jira API token", Validate: setupToken},
		},
		TokenPage: "https://id.atlassian.com/manage-profile/security/api-tokens",
		TokenLink: "Open Atlassian API tokens",
		TokenHint: "Create an API token, name it workfile, then paste it here.",
	},
	"github": {
		Fields: []SetupField{
			{"org", "Your GitHub organisation", "In github.com/example-team, the organisation is example-team.", "example-team", setupOrg},
			{Key: "token", Title: "Create a GitHub token", Validate: setupToken},
		},
		TokenPage: githubTokenURL + "repo,read:org",
		TokenLink: "Open GitHub token page",
		TokenHint: "Create a classic token with repo and read:org. Authorise SSO if your organisation requires it.",
	},
}

func SetupFormFor(kind string, tracker bool) (SetupForm, bool) {
	form, ok := setupForms[kind]
	if kind == "github" && tracker {
		form.TokenPage = githubTokenURL + "repo,read:org,read:project"
		form.TokenHint = "Create a classic token with repo, read:org and read:project. Authorise SSO if your organisation requires it."
	}
	return form, ok
}

var setupSitePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.atlassian\.net$`)
var setupOrgPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func setupSite(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(value, "https://"), "/"))
	if !setupSitePattern.MatchString(value) {
		return "", errors.New("enter a Jira Cloud hostname ending in .atlassian.net")
	}
	return value, nil
}
func setupEmail(value string) (string, error) {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || strings.ContainsAny(value, "\r\n\x00\"'") {
		return "", errors.New("enter an email address, such as alice@example.com")
	}
	return value, nil
}
func setupOrg(value string) (string, error) {
	if !setupOrgPattern.MatchString(value) {
		return "", errors.New("enter the organisation name, not a GitHub URL")
	}
	return value, nil
}

func setupToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00\"'") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", errors.New("paste only the token, on one line, without quotes")
	}
	return value, nil
}
