//go:build unix

package automation

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Lock makes sure only one wf watch fires automations for a workspace folder,
// so nothing fires twice. The lock goes when the process ends, even if it
// crashes.
func (s Store) Lock() (release func(), err error) {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "watch.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another wf watch is already running for this workspace; stop it first")
	}
	return func() { f.Close() }, nil
}
