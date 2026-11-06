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
// to the controlling terminal. Terminals that do not answer give no colour.
func queryTerminalBackground() (r, g, b uint8, ok bool) {
	return parseBackground(askTerminal("\x1b]11;?\x07", func(reply []byte) bool {
		return bytes.IndexByte(reply, 7) >= 0 || bytes.Contains(reply, []byte("\x1b\\"))
	}))
}

// askTerminal writes a request to the controlling terminal and collects its
// reply until done says it is complete, or a short wait runs out. A terminal
// that does not answer gives what arrived so far.
func askTerminal(request string, done func([]byte) bool) []byte {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer tty.Close()
	fd := int(tty.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer term.Restore(fd, state)
	if _, err := tty.WriteString(request); err != nil {
		return nil
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
			return reply
		}
		chunk := make([]byte, 256)
		got, err := unix.Read(fd, chunk)
		if err != nil || got == 0 {
			return reply
		}
		reply = append(reply, chunk[:got]...)
		if done(reply) {
			break
		}
	}
	return reply
}
