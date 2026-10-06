package provider

import (
	"encoding/json"
	"strings"
)

func (p pullRequest) checkStatus() (string, string) {
	const missing = "GitHub did not return the current commit's check status"
	if p.Commits == nil || len(p.Commits.Nodes) != 1 || p.Commits.Nodes[0].Commit == nil {
		return "", missing
	}
	raw := p.Commits.Nodes[0].Commit.StatusCheckRollup
	if len(raw) == 0 {
		return "", missing
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return "none", ""
	}
	var rollup struct{ State string }
	if err := json.Unmarshal(raw, &rollup); err != nil {
		return "", "GitHub returned an unknown check status"
	}
	switch rollup.State {
	case "SUCCESS":
		return "success", ""
	case "PENDING", "EXPECTED":
		return "pending", ""
	case "FAILURE", "ERROR":
		return "failure", ""
	default:
		return "", "GitHub returned an unknown check status"
	}
}
