package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Clipboard copies text. Read returns false if the backend cannot read the
// clipboard (OSC 52).
type Clipboard interface {
	Copy(text string) error
	Read() (string, bool)
	Name() string
}

type commandClipboard struct {
	name  string
	copy  []string
	clear []string // used for empty text if set (wl-copy needs --clear)
	paste []string
}

func (c *commandClipboard) Name() string { return c.name }

func (c *commandClipboard) Copy(text string) error {
	args := c.copy
	if text == "" && c.clear != nil {
		args = c.clear
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // fixed command list, text goes to stdin
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(c.name + ": " + msg)
		}
		return err
	}
	return nil
}

func (c *commandClipboard) Read() (string, bool) {
	out, err := exec.Command(c.paste[0], c.paste[1:]...).Output() //nolint:gosec // fixed command list
	if err != nil {
		return "", false
	}
	return string(out), true
}

// osc52Clipboard asks the terminal to set the clipboard; it also works over
// SSH when the terminal allows OSC 52.
type osc52Clipboard struct {
	out io.Writer
}

func (o *osc52Clipboard) Name() string { return "terminal (OSC 52)" }

func (o *osc52Clipboard) Copy(text string) error {
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
	_, err := seq.WriteTo(o.out)
	return err
}

func (o *osc52Clipboard) Read() (string, bool) { return "", false }

func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// NewSystemClipboard picks the first available backend; out receives OSC 52
// sequences when no clipboard command is available.
func NewSystemClipboard(out io.Writer) Clipboard {
	switch {
	case runtime.GOOS == "darwin" && have("pbcopy") && have("pbpaste"):
		return &commandClipboard{name: "pbcopy", copy: []string{"pbcopy"}, paste: []string{"pbpaste"}}
	case os.Getenv("WAYLAND_DISPLAY") != "" && have("wl-copy") && have("wl-paste"):
		return &commandClipboard{name: "wl-copy", copy: []string{"wl-copy"}, clear: []string{"wl-copy", "--clear"}, paste: []string{"wl-paste", "--no-newline"}}
	case os.Getenv("DISPLAY") != "" && have("xclip"):
		return &commandClipboard{name: "xclip", copy: []string{"xclip", "-selection", "clipboard"}, paste: []string{"xclip", "-selection", "clipboard", "-o"}}
	case os.Getenv("DISPLAY") != "" && have("xsel"):
		return &commandClipboard{name: "xsel", copy: []string{"xsel", "--clipboard", "--input"}, paste: []string{"xsel", "--clipboard", "--output"}}
	}
	return &osc52Clipboard{out: out}
}
