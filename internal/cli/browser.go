package cli

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// openBrowser opens a web address with the system's own opener, with no shell.
func openBrowser(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{target}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	default:
		command = "xdg-open"
		args = []string{target}
	}
	return exec.CommandContext(ctx, command, args...).Run()
}
