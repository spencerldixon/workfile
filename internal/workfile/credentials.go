package workfile

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func PersonalPath(file string) string {
	key := "WORKFILE_CONFIG_FILE"
	if file == "credentials" {
		key = "WORKFILE_CREDENTIALS_FILE"
	}
	if path := os.Getenv(key); path != "" {
		return path
	}
	return filepath.Join(PersonalDir(), file)
}

// PersonalDir is the person's own Workfile folder, outside every repository.
func PersonalDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "workfile")
}

// WorkspaceSettings are the person's own settings for one workspace, kept in
// ~/.config/workfile/<workspace>/config.yml beside their private automations.
type WorkspaceSettings struct {
	// OnlyMine limits ticket listings to tickets assigned to the person.
	OnlyMine bool `yaml:"only_mine"`
}

func WorkspaceSettingsPath(name string) string {
	return filepath.Join(PersonalDir(), name, "config.yml")
}

// ReadWorkspaceSettings reads the settings for a named workspace. A workspace
// without a name, or without the file, has the defaults.
func ReadWorkspaceSettings(name string) (WorkspaceSettings, error) {
	var s WorkspaceSettings
	if name == "" {
		return s, nil
	}
	err := ReadYAML(WorkspaceSettingsPath(name), &s, true)
	return s, err
}

// MyJiraAssignee is the JQL value for the person's own Jira account: me.jira
// from config.yml when set, otherwise whoever owns the token.
func (w *Workspace) MyJiraAssignee() (string, error) {
	me, err := Me()
	if err != nil {
		return "", err
	}
	id := me[w.Policy.Tracker]
	if id == "" {
		id = me["jira"]
	}
	if id == "" {
		return "currentUser()", nil
	}
	return strconv.Quote(id), nil
}

// PrivateAutomationsPath is where a person keeps private automations for the
// workspace with this name.
func PrivateAutomationsPath(name string) string {
	return filepath.Join(PersonalDir(), name, "automations.yml")
}

type Credentials struct {
	Path   string
	values map[string]string
}

func (c *Credentials) Get(reference string) (string, error) {
	match := credentialPattern.FindStringSubmatch(reference)
	if match == nil {
		return "", errors.New("credentials must be referenced as ${NAME}")
	}
	name := match[1]
	if value := os.Getenv(name); value != "" {
		return value, nil
	}
	if c.Path == "" {
		c.Path = PersonalPath("credentials")
	}
	if c.values == nil {
		if err := c.read(); err != nil {
			return "", err
		}
	}
	if value := c.values[name]; value != "" {
		return value, nil
	}
	return "", fmt.Errorf("%s is not set; add it to %s or your environment", name, c.Path)
}

var credentialLine = regexp.MustCompile(`^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)

func (c *Credentials) read() error {
	f, err := os.Open(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		c.values = map[string]string{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read credentials: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("credentials must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("credentials are accessible to other users; run chmod 600 %s", c.Path)
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		match := credentialLine.FindStringSubmatch(text)
		if match == nil {
			return fmt.Errorf("credentials line %d: expected NAME=value", line)
		}
		value := strings.TrimSpace(match[2])
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[match[1]] = value
	}
	if err := scanner.Err(); err != nil {
		return errors.New("cannot read credentials file")
	}
	c.values = values
	return nil
}

// Personal holds the person's own settings from config.yml.
type Personal struct {
	// Me overrides the identity read from each token, by connection name or
	// provider type.
	Me map[string]string
	// Person is the person's name in people.yml, which picks their shared
	// automations file. Set as me.person.
	Person string
	// Agent is the command that receives an automation's prompt as its last
	// argument, such as [claude, -p].
	Agent []string
}

func ReadPersonal() (Personal, error) {
	var config struct {
		Me    map[string]string `yaml:"me"`
		Agent []string          `yaml:"agent"`
	}
	path := PersonalPath("config.yml")
	if err := ReadYAML(path, &config, true); err != nil {
		return Personal{}, err
	}
	p := Personal{Me: config.Me, Agent: config.Agent}
	if person, ok := p.Me["person"]; ok {
		p.Person = person
		delete(p.Me, "person")
	}
	if len(p.Agent) > 0 && strings.TrimSpace(p.Agent[0]) == "" {
		return p, fmt.Errorf("%s: agent must start with a program, such as [claude, -p]", path)
	}
	return p, nil
}

func Me() (map[string]string, error) {
	p, err := ReadPersonal()
	return p.Me, err
}
