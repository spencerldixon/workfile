//go:build !unix

package cli

func queryTerminalBackground() (r, g, b uint8, ok bool) { return 0, 0, 0, false }
