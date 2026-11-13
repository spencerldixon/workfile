package cli

import (
	"fmt"
	"os"
	"testing"
)

// TestMain keeps every test's status snapshots out of the real home folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wf-state-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
