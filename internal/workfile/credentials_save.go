package workfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Set supplies credentials in memory, so setup can validate before saving.
func (c *Credentials) Set(values map[string]string) { c.values = values }

// SaveCredentials preserves existing entries and replaces only supplied names.
// The replacement is atomic and private. Secrets never enter workspace files.
func SaveCredentials(path string, values map[string]string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("credentials must be a regular file, not a link")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c := Credentials{Path: path}
	if err := c.read(); err != nil {
		return err
	}
	for name, value := range values {
		if !credentialPattern.MatchString("${"+name+"}") || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00\"'") {
			return errors.New("invalid credential name or value")
		}
		c.values[name] = value
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	for _, name := range SortedKeys(c.values) {
		if _, err = fmt.Fprintf(f, "%s=%s\n", name, c.values[name]); err != nil {
			f.Close()
			return err
		}
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
