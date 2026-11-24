package workfile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// InboxSettings are the person's choices for the Tracker inbox. Zero values inherit
// defaults. They never change policy.
type InboxSettings struct {
	Repositories   map[string][]string `yaml:"repositories"`
	ReviewRequests *bool               `yaml:"review_requests"`
	Signals        []RowKind           `yaml:"signals"`
	RefreshEvery   int                 `yaml:"refresh_every"`
	FullEvery      int                 `yaml:"full_every"`
	Poll           Duration            `yaml:"poll"`
	Snooze         Duration            `yaml:"snooze"`
	Agent          string              `yaml:"agent"`
	Prompts        map[string]string   `yaml:"prompts"`
}

// Duration reads "2m" or "1h" from YAML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var text string
	if err := node.Decode(&text); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("%q is not a duration such as 2m or 1h", text)
	}
	d.Duration = parsed
	return nil
}

// PromptKinds are the inbox rows an agent prompt can be written for.
var PromptKinds = []string{"default", "changes_requested", "comments", "review_requested", "checks_failed", "out_of_policy", "ready"}

// Placeholders are the names a prompt or agent command may use in braces.
var Placeholders = []string{"key", "pr", "url", "stage", "to", "who", "count", "fix", "prompt"}

const minimumPoll = 30 * time.Second

var placeholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func defaultInboxSettings() InboxSettings {
	reviewRequests := false
	return InboxSettings{
		ReviewRequests: &reviewRequests,
		Poll:           Duration{30 * time.Second},
		RefreshEvery:   5,
		FullEvery:      15,
		Snooze:         Duration{time.Hour},
		Prompts: map[string]string{
			"default":           "Get {pr} past {stage} for {key}",
			"changes_requested": "Address {who}'s review on {pr} for {key}",
			"comments":          "Answer the {count} new comments on {pr} for {key}",
			"review_requested":  "Review {pr} for {who}",
			"checks_failed":     "Fix the failing checks on {pr} for {key}",
			"out_of_policy":     "Bring {key} back into policy: {fix}",
			"ready":             "Move {key} to {to}",
		},
	}
}

// ReadInboxSettings reads the same three layers as tracker settings, lowest
// precedence first: personal, the repo's .workfile/config.yml, then the
// person's settings for this workspace.
func ReadInboxSettings(w *Workspace) (InboxSettings, error) {
	personal, err := ReadPersonal()
	if err != nil {
		return InboxSettings{}, err
	}
	shared, path, err := readRepoConfig(w)
	if err != nil {
		return InboxSettings{}, err
	}
	s := defaultInboxSettings()
	for _, layer := range []struct {
		path     string
		settings InboxSettings
	}{
		{PersonalPath("config.yml"), personal.Inbox},
		{path, shared.Inbox},
		{WorkspaceSettingsPath(w.Policy.Name), w.Settings.Inbox},
	} {
		// The agent is a command run on this computer, so only the person
		// chooses it: a repo's shared config may not.
		if layer.path == path && layer.settings.Agent != "" {
			return InboxSettings{}, fmt.Errorf("%s: inbox.agent: set the agent in your personal config, not the repo's; it runs a command on your computer", path)
		}
		if err := validateInboxLayer(layer.settings); err != nil {
			return InboxSettings{}, fmt.Errorf("%s: inbox.%w", layer.path, err)
		}
		s = mergeInboxSettings(s, layer.settings)
	}
	return s, nil
}

func mergeInboxSettings(base, override InboxSettings) InboxSettings {
	if override.ReviewRequests != nil {
		base.ReviewRequests = override.ReviewRequests
	}
	if override.Repositories != nil {
		base.Repositories = override.Repositories
	}
	if override.Signals != nil {
		base.Signals = override.Signals
	}
	if override.Poll.Duration != 0 {
		base.Poll = override.Poll
	}
	if override.RefreshEvery != 0 {
		base.RefreshEvery = override.RefreshEvery
	}
	if override.FullEvery != 0 {
		base.FullEvery = override.FullEvery
	}
	if override.Snooze.Duration != 0 {
		base.Snooze = override.Snooze
	}
	if override.Agent != "" {
		base.Agent = override.Agent
	}
	prompts := make(map[string]string, len(base.Prompts))
	for kind, prompt := range base.Prompts {
		prompts[kind] = prompt
	}
	for kind, prompt := range override.Prompts {
		prompts[kind] = prompt
	}
	base.Prompts = prompts
	return base
}

func (s InboxSettings) WatchesRepository(connection, repository string) bool {
	if _, name, qualified := strings.Cut(repository, "/"); qualified {
		repository = name
	}
	return repository != "" && slices.ContainsFunc(s.Repositories[connection], func(name string) bool {
		return strings.EqualFold(name, repository)
	})
}

var inboxRepositoryPattern = regexp.MustCompile("^[a-zA-Z0-9_.-]+$")

func validateInboxLayer(s InboxSettings) error {
	for _, repos := range s.Repositories {
		for _, repo := range repos {
			if !inboxRepositoryPattern.MatchString(repo) {
				return fmt.Errorf("repositories: %q must be a repository name without its owner", repo)
			}
		}
	}
	for _, signal := range s.Signals {
		if !slices.Contains(rowRank, signal) {
			return fmt.Errorf("signals: unknown signal %q", signal)
		}
	}
	if s.Poll.Duration != 0 && s.Poll.Duration < minimumPoll {
		return fmt.Errorf("poll: %s is too often; use at least 30s", s.Poll.Duration)
	}
	if s.RefreshEvery < 0 {
		return fmt.Errorf("refresh_every: must be a positive poll count")
	}
	if s.FullEvery < 0 {
		return fmt.Errorf("full_every: must be a positive poll count")
	}
	if s.Snooze.Duration < 0 {
		return fmt.Errorf("snooze: must be positive")
	}
	if err := checkPlaceholders(s.Agent); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	for _, kind := range SortedKeys(s.Prompts) {
		if !slices.Contains(PromptKinds, kind) {
			return fmt.Errorf("prompts: %q is not a row kind; use one of %v", kind, PromptKinds)
		}
		if err := checkPlaceholders(s.Prompts[kind]); err != nil {
			return fmt.Errorf("prompts.%s: %w", kind, err)
		}
	}
	return nil
}

func checkPlaceholders(text string) error {
	for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(Placeholders, match[1]) {
			return fmt.Errorf("unknown placeholder %s; use one of %v", match[0], Placeholders)
		}
	}
	return nil
}

// repoConfig is the repo's own .workfile/config.yml. Every section is listed
// here, so reading one section never rejects another as unknown.
type repoConfig struct {
	Tracker TrackerSettings `yaml:"tracker"`
	Inbox   InboxSettings   `yaml:"inbox"`
}

func readRepoConfig(w *Workspace) (repoConfig, string, error) {
	path := filepath.Join(w.Dir, "config.yml")
	var c repoConfig
	err := ReadYAML(path, &c, true)
	return c, path, err
}
