package provider

import (
	"net/url"
	"regexp"
	"strings"
)

var webLink = regexp.MustCompile(`https://[^\s<>"\x60]+`)

// Only explicit browse links to the configured Jira site link body text.
// A bare ticket mention in a description might refer to unrelated work.
func explicitKeys(body, site string) []string {
	var keys []string
	for _, raw := range webLink.FindAllString(body, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ").,;]"))
		if err != nil || !strings.EqualFold(u.Host, site) || u.User != nil {
			continue
		}
		key := strings.TrimPrefix(u.Path, "/browse/")
		matches := linkedKeys(key)
		if strings.HasPrefix(u.Path, "/browse/") && len(matches) == 1 && matches[0] == strings.ToUpper(key) {
			keys = append(keys, matches[0])
		}
	}
	return keys
}
