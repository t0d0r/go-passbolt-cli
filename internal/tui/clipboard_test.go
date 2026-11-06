package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClipboardToolsIgnorePATH(t *testing.T) {
	// A fake pbcopy/xclip early in PATH must not be used.
	dir := t.TempDir()
	for _, name := range []string{"pbcopy", "pbpaste", "xclip", "xsel", "wl-copy", "wl-paste"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	clip := NewSystemClipboard(nil)
	if c, ok := clip.(*commandClipboard); ok {
		for _, p := range append(append([]string{}, c.copy...), c.paste...) {
			if strings.HasPrefix(p, dir) {
				t.Errorf("clipboard tool taken from PATH: %s", p)
			}
		}
		if !filepath.IsAbs(c.copy[0]) || (!strings.HasPrefix(c.copy[0], "/usr/bin/") && !strings.HasPrefix(c.copy[0], "/bin/")) {
			t.Errorf("copy tool %q is not from a trusted directory", c.copy[0])
		}
	}
}

func TestOSC52WithoutTTYRefuses(t *testing.T) {
	o := &osc52Clipboard{}
	if err := o.Copy("secret"); !errors.Is(err, errNoClipboard) {
		t.Errorf("Copy without a terminal = %v, want errNoClipboard", err)
	}
	var buf bytes.Buffer
	o = &osc52Clipboard{tty: &buf}
	if err := o.Copy("secret"); err != nil || buf.Len() == 0 {
		t.Errorf("Copy to a terminal: err=%v, %d bytes", err, buf.Len())
	}
	if o.Verifiable() {
		t.Error("OSC 52 claims a verifiable clear")
	}
}
