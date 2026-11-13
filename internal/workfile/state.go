package workfile

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// StateDir is where wf keeps snapshots of this workspace for status --since:
// $XDG_STATE_HOME/workfile/<key>/snapshots, with ~/.local/state as the default.
// The key is the policy name when set, else a short hash of the .workfile/ path.
func StateDir(w *Workspace) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	key := w.Policy.Name
	if key == "" {
		abs, err := filepath.Abs(w.Dir)
		if err != nil {
			abs = w.Dir
		}
		key = fmt.Sprintf("%x", sha256.Sum256([]byte(abs)))[:16]
	}
	return filepath.Join(dir, "workfile", key, "snapshots")
}
