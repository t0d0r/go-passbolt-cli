//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// tuiExitSignals end the TUI cleanly (clipboard cleared, logout). bubbletea
// itself only handles SIGINT and SIGTERM; SIGHUP arrives when the terminal or
// the SSH session is closed.
var tuiExitSignals = []os.Signal{syscall.SIGHUP, syscall.SIGQUIT}
