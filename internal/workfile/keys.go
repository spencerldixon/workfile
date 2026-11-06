package workfile

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)
var numberPattern = regexp.MustCompile(`^[0-9]+$`)
var mentionPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*-[0-9]+`)
var webLink = regexp.MustCompile(`https://[^\s<>"\x60]+`)

// dashKeys is Keys for trackers whose tickets look like APP-42. site and path
// say where the tracker's ticket pages live: https://site + path + KEY.
type dashKeys struct{ site, path string }

// TrackerKeys is the key rules of the workspace's tracker.
func (w *Workspace) TrackerKeys() Keys {
	p := w.Providers[w.Policy.Tracker]
	if kind, ok := Kinds[p.Kind]; ok && kind.Keys != nil {
		return kind.Keys(p)
	}
	return dashKeys{}
}

// Parse uppercases a key. A bare number is completed with the project.
func (dashKeys) Parse(input, project string) (string, error) {
	key := strings.ToUpper(input)
	if numberPattern.MatchString(key) {
		if project == "" {
			return "", fmt.Errorf("%s is not a ticket key; use the full key (for example APP-%s) or set default_project in providers.yml", key, key)
		}
		number := strings.TrimLeft(key, "0")
		if number == "" {
			number = "0"
		}
		return project + "-" + number, nil
	}
	if !keyPattern.MatchString(key) {
		return "", fmt.Errorf("%s is not a ticket key (expected APP-42 or a number)", key)
	}
	return key, nil
}

// Search is the text a code host searches for.
func (dashKeys) Search(key string) string { return key }

// Mentions finds keys in text. Boundaries avoid linking APP-12 to APP-123 or
// MYAPP-12.
func (dashKeys) Mentions(text string) []string {
	var keys []string
	for _, match := range mentionPattern.FindAllStringIndex(text, -1) {
		if match[0] > 0 && isWordByte(text[match[0]-1]) || match[1] < len(text) && isWordByte(text[match[1]]) {
			continue
		}
		key := strings.ToUpper(text[match[0]:match[1]])
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// Links finds keys in explicit links to this tracker's ticket pages. A bare
// ticket mention in a description might refer to unrelated work.
func (d dashKeys) Links(body string) []string {
	var keys []string
	for _, raw := range webLink.FindAllString(body, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ").,;]"))
		if err != nil || !strings.EqualFold(u.Host, d.site) || u.User != nil {
			continue
		}
		key := strings.TrimPrefix(u.Path, d.path)
		matches := d.Mentions(key)
		if strings.HasPrefix(u.Path, d.path) && len(matches) == 1 && matches[0] == strings.ToUpper(key) {
			keys = append(keys, matches[0])
		}
	}
	return keys
}
