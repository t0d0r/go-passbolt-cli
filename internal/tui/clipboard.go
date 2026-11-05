package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
	"golang.org/x/term"
)

// Clipboard copies text. Read returns false if the backend cannot read the
// clipboard (OSC 52).
type Clipboard interface {
	Copy(text string) error
	Read() (string, bool)
	Name() string
	// Verifiable is false if a clear can only be requested, not confirmed.
	Verifiable() bool
}

// errNoClipboard means there is no safe way to copy.
var errNoClipboard = errors.New("no clipboard available (no clipboard tool and no terminal for OSC 52)")

// trustedDirs are searched for clipboard tools instead of PATH, so a
// user-writable directory early in PATH cannot receive copied passwords.
var trustedDirs = []string{"/usr/bin", "/bin"}

// clipboardTimeout bounds a clipboard tool; some (xclip) fork a helper that
// keeps running to serve the selection.
const clipboardTimeout = 3 * time.Second

func trustedTool(name string) (string, bool) {
	for _, dir := range trustedDirs {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

type commandClipboard struct {
	name  string
	copy  []string
	clear []string // used for empty text if set (wl-copy needs --clear)
	paste []string
}

func (c *commandClipboard) Name() string     { return c.name }
func (c *commandClipboard) Verifiable() bool { return true }

func (c *commandClipboard) Copy(text string) error {
	args := c.copy
	if text == "" && c.clear != nil {
		args = c.clear
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // absolute path from trustedDirs, text goes to stdin
	cmd.Stdin = strings.NewReader(text)
	// No Stdout/Stderr pipes: a forked helper that inherits them would keep
	// Run waiting until the selection is taken over.
	if err := cmd.Run(); err != nil {
		return errors.New(c.name + ": " + err.Error())
	}
	return nil
}

func (c *commandClipboard) Read() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.paste[0], c.paste[1:]...).Output() //nolint:gosec // absolute path from trustedDirs
	if err != nil {
		return "", false
	}
	return string(out), true
}

// osc52Clipboard asks the terminal to set the clipboard; it also works over
// SSH when the terminal allows OSC 52. It writes only to the controlling
// terminal, never to stdout/stderr, which may be redirected to a file.
type osc52Clipboard struct {
	tty io.Writer
}

func (o *osc52Clipboard) Name() string     { return "terminal (OSC 52)" }
func (o *osc52Clipboard) Verifiable() bool { return false }

func (o *osc52Clipboard) Copy(text string) error {
	if o.tty == nil {
		return errNoClipboard
	}
	seq := osc52.New(text)
	if text == "" {
		seq = osc52.Clear()
	}
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		seq = seq.Screen()
	}
	_, err := seq.WriteTo(o.tty)
	return err
}

func (o *osc52Clipboard) Read() (string, bool) { return "", false }

// openTTY returns the controlling terminal for OSC 52, or nil if there is
// none (the caller closes it).
func openTTY() *os.File {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	if !term.IsTerminal(int(f.Fd())) {
		_ = f.Close()
		return nil
	}
	return f
}

// NewSystemClipboard picks the first available backend. Clipboard tools are
// taken from trustedDirs only. Without one, OSC 52 is written to tty (the
// controlling terminal, may be nil: copying then fails with a message).
func NewSystemClipboard(tty io.Writer) Clipboard {
	tool := func(name string) string { p, _ := trustedTool(name); return p }
	switch {
	case runtime.GOOS == "darwin" && tool("pbcopy") != "" && tool("pbpaste") != "":
		return &commandClipboard{name: "pbcopy", copy: []string{tool("pbcopy")}, paste: []string{tool("pbpaste")}}
	case os.Getenv("WAYLAND_DISPLAY") != "" && tool("wl-copy") != "" && tool("wl-paste") != "":
		return &commandClipboard{name: "wl-copy", copy: []string{tool("wl-copy")}, clear: []string{tool("wl-copy"), "--clear"}, paste: []string{tool("wl-paste"), "--no-newline"}}
	case os.Getenv("DISPLAY") != "" && tool("xclip") != "":
		return &commandClipboard{name: "xclip", copy: []string{tool("xclip"), "-selection", "clipboard"}, paste: []string{tool("xclip"), "-selection", "clipboard", "-o"}}
	case os.Getenv("DISPLAY") != "" && tool("xsel") != "":
		return &commandClipboard{name: "xsel", copy: []string{tool("xsel"), "--clipboard", "--input"}, paste: []string{tool("xsel"), "--clipboard", "--output"}}
	}
	if tty == nil {
		return &osc52Clipboard{} // copying fails with errNoClipboard
	}
	return &osc52Clipboard{tty: tty}
}

// OpenTTY is used by the tui command to give OSC 52 the terminal.
func OpenTTY() *os.File { return openTTY() }
