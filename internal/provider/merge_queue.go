package provider

import (
	"bytes"
	"encoding/json"
)

// A null entry means not queued. An omitted or malformed field is unknown,
// not proof that the PR is outside the queue.
func (p pullRequest) mergeQueueStatus() (string, string) {
	raw := bytes.TrimSpace(p.MergeQueueEntry)
	if bytes.Equal(raw, []byte("null")) {
		return "none", ""
	}
	var entry struct{ ID string }
	if len(raw) == 0 || json.Unmarshal(raw, &entry) != nil || entry.ID == "" {
		return "", "GitHub did not return merge queue membership"
	}
	return "queued", ""
}
