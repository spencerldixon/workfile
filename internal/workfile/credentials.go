package workfile

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "workfile", file)
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

func Me() (map[string]string, error) {
	var config struct {
		Me map[string]string `yaml:"me"`
	}
	if err := ReadYAML(PersonalPath("config.yml"), &config, true); err != nil {
		return nil, err
	}
	return config.Me, nil
}
