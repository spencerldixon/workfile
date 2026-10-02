//go:build unix

package cli

import (
	"bytes"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// queryTerminalBackground sends the OSC 11 "what is your background?" request
// to the controlling terminal and waits briefly for the answer. Terminals that
// do not answer simply give no colour.
func queryTerminalBackground() (r, g, b uint8, ok bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 0, 0, 0, false
	}
	defer tty.Close()
	fd := int(tty.Fd())
	if !term.IsTerminal(fd) {
		return 0, 0, 0, false
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, 0, 0, false
	}
	defer term.Restore(fd, state)
	if _, err := tty.WriteString("\x1b]11;?\x07"); err != nil {
		return 0, 0, 0, false
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	var reply []byte
	for time.Now().Before(deadline) {
		var ready unix.FdSet
		ready.Set(fd)
		wait := unix.NsecToTimeval(int64(time.Until(deadline)))
		n, err := unix.Select(fd+1, &ready, nil, nil, &wait)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return 0, 0, 0, false
		}
		chunk := make([]byte, 64)
		got, err := unix.Read(fd, chunk)
		if err != nil || got == 0 {
			return 0, 0, 0, false
		}
		reply = append(reply, chunk[:got]...)
		if bytes.IndexByte(reply, 7) >= 0 || bytes.Contains(reply, []byte("\x1b\\")) {
			break
		}
	}
	return parseBackground(reply)
}
