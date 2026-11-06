package cli

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)
var numberPattern = regexp.MustCompile(`^[0-9]+$`)

// completeKeys turns bare numbers into full keys using the tracker's
// default_project, and drops repeats so a ticket is only fetched once.
func completeKeys(keys []string, project string) ([]string, error) {
	var result []string
	for _, key := range keys {
		if numberPattern.MatchString(key) {
			if project == "" {
				return nil, fmt.Errorf("%s is not a ticket key; use the full key (for example APP-%s) or set default_project in providers.yml", key, key)
			}
			number := strings.TrimLeft(key, "0")
			if number == "" {
				number = "0"
			}
			key = project + "-" + number
		}
		if !slices.Contains(result, key) {
			result = append(result, key)
		}
	}
	return result, nil
}

// ticketArgs checks that every positional argument is a ticket key, and
// uppercases them. A bare number such as 2713 is completed once the workspace
// is loaded, because the project comes from providers.yml.
func ticketArgs(o options) error {
	for i, key := range o.keys {
		key = strings.ToUpper(key)
		if !keyPattern.MatchString(key) && !numberPattern.MatchString(key) {
			return fmt.Errorf("%s is not a ticket key (expected APP-42 or a number)", key)
		}
		o.keys[i] = key
	}
	return nil
}
