package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/passbolt/go-passbolt/helper"
)

type fakeStore struct {
	folders []Folder
	entries []Entry
	secrets map[string]Secret
	loadErr error
	calls   int
}

func (f *fakeStore) Load(context.Context) ([]Folder, []Entry, error) {
	return f.folders, f.entries, f.loadErr
}

func (f *fakeStore) Secret(_ context.Context, id string) (Secret, error) {
	f.calls++
	s, ok := f.secrets[id]
	if !ok {
		return Secret{}, errors.New("not found")
	}
	return s, nil
}

type fakeClip struct {
	text     string
	readable bool
	copies   int
}

func (c *fakeClip) Copy(text string) error { c.text = text; c.copies++; return nil }
func (c *fakeClip) Read() (string, bool)   { return c.text, c.readable }
func (c *fakeClip) Name() string           { return "fake" }

const totpKey = "JBSWY3DPEHPK3PXP"

func testData() *fakeStore {
	return &fakeStore{
		folders: []Folder{
			{ID: "A", Name: "Alpha"},
			{ID: "B", ParentID: "A", Name: "Beta"},
		},
		entries: []Entry{
			{ID: "e1", Name: "Root Mail", Username: "root@example.com", URI: "https://mail.example.com",
				Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Modified: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "e2", FolderID: "A", Name: "Alpha Site", Username: "alpha"},
			{ID: "e3", FolderID: "B", Name: "Beta DB", Username: "dbadmin", Description: "primary"},
			{ID: "e4", Name: "\x1b[31mEvil\x07", Username: "x",
				Created: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Modified: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
		secrets: map[string]Secret{
			"e1": {Password: "s3cret-root"},
			"e3": {Password: "db-pass", TOTPKey: totpKey, TOTPSupported: true},
			"e2": {Password: "a", TOTPKey: totpKey, TOTPSupported: false},
		},
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+h": // what terminals sending ^H for Backspace produce
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "alt+backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// newLoaded returns a model that has processed the initial load.
func newLoaded(t *testing.T, store *fakeStore, clip *fakeClip) *Model {
	t.Helper()
	m := New(store, clip, "https://pb.example.com", time.Second)
	m.tickEvery = time.Millisecond
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return t0 }
	m.Update(m.Init()())
	return m
}

// advance moves the fake clock of m forward.
func advance(m *Model, d time.Duration) {
	cur := m.now()
	m.now = func() time.Time { return cur.Add(d) }
}

// press sends keys and returns the command of the last one.
func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(key(k))
	}
	return cmd
}

// step runs cmd, feeds its message back and returns the next command.
func step(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	_, next := m.Update(cmd())
	return next
}

func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

func selectedName(m *Model) string {
	r, ok := m.selected()
	switch {
	case !ok:
		return ""
	case r.folder != nil:
		return r.folder.Name + "/"
	default:
		return r.entry.Name
	}
}

func TestBrowseNavigation(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	if got := selectedName(m); got != "Alpha/" {
		t.Fatalf("first row = %q, want Alpha/", got)
	}
	press(m, "enter")
	if m.cwd != "A" || selectedName(m) != "Beta/" {
		t.Fatalf("in A: cwd=%q selected=%q", m.cwd, selectedName(m))
	}
	press(m, "enter")
	if m.cwd != "B" || selectedName(m) != "Beta DB" {
		t.Fatalf("in B: cwd=%q selected=%q", m.cwd, selectedName(m))
	}
	if !strings.Contains(m.View(), "/Alpha/Beta") {
		t.Error("header does not show the folder path")
	}
	press(m, "backspace")
	if m.cwd != "A" || selectedName(m) != "Beta/" {
		t.Errorf("back in A: cwd=%q selected=%q, want cursor on Beta/", m.cwd, selectedName(m))
	}
	press(m, "h")
	if m.cwd != "" || selectedName(m) != "Alpha/" {
		t.Errorf("back at root: cwd=%q selected=%q", m.cwd, selectedName(m))
	}
	press(m, "j", "j", "j", "j", "j", "j") // past the end stays on the last row
	if m.cursor != len(m.rows())-1 {
		t.Errorf("cursor = %d, want last row %d", m.cursor, len(m.rows())-1)
	}
}

func TestSearch(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	press(m, "/")
	typeText(m, "beta")
	rows := m.rows()
	if len(rows) != 1 || rows[0].entry.ID != "e3" {
		t.Fatalf("search beta: %d rows, want e3 only", len(rows))
	}
	if !strings.Contains(m.View(), "/Alpha/Beta") {
		t.Error("search row does not show the folder path")
	}

	press(m, "esc", "/")
	typeText(m, "alpha site")
	if rows := m.rows(); len(rows) != 1 || rows[0].entry.ID != "e2" {
		t.Errorf("search 'alpha site': %d rows, want e2", len(rows))
	}
	press(m, "backspace", "backspace", "backspace", "backspace", "backspace")
	if m.query != "alpha" {
		t.Errorf("query after backspace = %q", m.query)
	}

	press(m, "enter")
	if m.mode != modeDetail || m.detail.ID != "e2" {
		t.Fatalf("enter did not open the detail of e2")
	}
	press(m, "esc")
	if m.mode != modeSearch {
		t.Error("esc from detail should return to search")
	}
}

func openDetail(t *testing.T, m *Model, query string) {
	t.Helper()
	press(m, "/")
	typeText(m, query)
	press(m, "enter")
	if m.mode != modeDetail {
		t.Fatalf("detail for %q not opened", query)
	}
}

func TestDetailShowHidePassword(t *testing.T) {
	store := testData()
	m := newLoaded(t, store, &fakeClip{})
	openDetail(t, m, "root mail")

	if v := m.View(); strings.Contains(v, "s3cret-root") || !strings.Contains(v, "********") {
		t.Fatal("password visible before p")
	}
	step(t, m, press(m, "p"))
	if !strings.Contains(m.View(), "s3cret-root") {
		t.Fatal("password not shown after p")
	}
	press(m, "p")
	if strings.Contains(m.View(), "s3cret-root") {
		t.Fatal("password still shown after second p")
	}
	press(m, "p") // cached secret: no second fetch
	if store.calls != 1 {
		t.Errorf("secret fetched %d times, want 1", store.calls)
	}

	press(m, "esc")
	if m.secret != nil || m.showPassword {
		t.Error("secret kept after leaving the detail")
	}
}

func TestCopyPasswordIsCleared(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	openDetail(t, m, "root mail")

	tick := step(t, m, press(m, "c"))
	if clip.text != "s3cret-root" {
		t.Fatalf("clipboard = %q, want the password", clip.text)
	}
	if !strings.Contains(m.status, "copied") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(m.View(), "clears in 30s") {
		t.Error("countdown not shown")
	}
	advance(m, 10*time.Second)
	tick = step(t, m, tick)
	if clip.text != "s3cret-root" || !strings.Contains(m.View(), "clears in 20s") {
		t.Fatal("cleared too early or countdown wrong")
	}
	advance(m, 21*time.Second)
	step(t, m, tick)
	if clip.text != "" {
		t.Errorf("clipboard not cleared: %q", clip.text)
	}
}

func TestClearSkippedWhenClipboardChanged(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	openDetail(t, m, "root mail")

	tick := step(t, m, press(m, "c"))
	clip.text = "something the user copied"
	advance(m, 31*time.Second)
	step(t, m, tick)
	if clip.text != "something the user copied" {
		t.Errorf("foreign clipboard content was cleared")
	}
}

func TestCopyAgainRestartsClearTimer(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	openDetail(t, m, "root mail")

	tick := step(t, m, press(m, "c"))
	advance(m, 20*time.Second)
	if cmd := press(m, "c"); cmd != nil { // clock already running
		t.Error("second copy started a second clock")
	}
	advance(m, 11*time.Second) // 31 s after the first copy, 11 s after the second
	tick = step(t, m, tick)
	if clip.text != "s3cret-root" {
		t.Fatal("cleared based on the first copy")
	}
	advance(m, 20*time.Second)
	step(t, m, tick)
	if clip.text != "" {
		t.Error("not cleared 30 s after the second copy")
	}
}

func TestUnreadableClipboardClearedUnconditionally(t *testing.T) {
	clip := &fakeClip{readable: false}
	m := newLoaded(t, testData(), clip)
	openDetail(t, m, "root mail")
	tick := step(t, m, press(m, "c"))
	advance(m, 31*time.Second)
	step(t, m, tick)
	if clip.text != "" {
		t.Error("OSC 52 style clipboard not cleared")
	}
}

func TestQuitClearsPendingPassword(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	openDetail(t, m, "root mail")
	step(t, m, press(m, "c"))
	press(m, "esc", "esc")
	cmd := press(m, "q")
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q did not quit")
	}
	if clip.text != "" {
		t.Error("clipboard not cleared on quit")
	}
}

func TestCopyUsernameFromList(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	press(m, "j") // first entry after the folder
	if cmd := press(m, "u"); cmd != nil {
		t.Error("username copy should not schedule a clear")
	}
	if clip.text == "" || clip.text == "s3cret-root" {
		t.Errorf("clipboard = %q, want a username", clip.text)
	}
}

func TestCopyPasswordFromList(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	press(m, "/")
	typeText(m, "root")
	press(m, "esc") // back to the root folder listing
	for selectedName(m) != "Root Mail" {
		press(m, "j")
		if m.cursor == len(m.rows())-1 && selectedName(m) != "Root Mail" {
			t.Fatal("Root Mail not in the root listing")
		}
	}
	step(t, m, press(m, "c"))
	if clip.text != "s3cret-root" {
		t.Errorf("clipboard = %q", clip.text)
	}
}

func TestTOTP(t *testing.T) {
	clip := &fakeClip{readable: true}
	m := newLoaded(t, testData(), clip)
	fixed := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	want, err := helper.GenerateOTPCode(totpKey, fixed)
	if err != nil {
		t.Fatal(err)
	}

	openDetail(t, m, "beta db")
	step(t, m, press(m, "t"))
	if clip.text != want {
		t.Errorf("clipboard = %q, want TOTP %q", clip.text, want)
	}

	press(m, "esc", "esc")
	openDetail(t, m, "alpha site")
	before := clip.copies
	step(t, m, press(m, "t"))
	if clip.copies != before || !strings.Contains(m.status, "not supported") {
		t.Errorf("unsupported TOTP copied or no message: status=%q", m.status)
	}
}

func TestControlCharactersStripped(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	v := m.View()
	if strings.Contains(v, "\x1b[31m") || strings.Contains(v, "\x07") {
		t.Error("escape sequence from resource name reached the screen")
	}
	if got := sanitize("a\x1b[2Jb\tc\u202ed"); got != "a[2Jb cd" {
		t.Errorf("sanitize = %q", got)
	}
}

func TestLoadErrorAndRetry(t *testing.T) {
	store := testData()
	store.loadErr = errors.New("boom")
	m := newLoaded(t, store, &fakeClip{})
	if !strings.Contains(m.View(), "Could not load data") {
		t.Fatal("load error not shown")
	}
	store.loadErr = nil
	step(t, m, press(m, "r"))
	if m.loadErr != nil || selectedName(m) != "Alpha/" {
		t.Error("reload did not recover")
	}
}

func TestTreeCycleAndUnknownParent(t *testing.T) {
	tr := newTree([]Folder{
		{ID: "X", ParentID: "Y", Name: "x"},
		{ID: "Y", ParentID: "X", Name: "y"},
		{ID: "Z", ParentID: "missing", Name: "z"},
	}, []Entry{{ID: "e", FolderID: "gone", Name: "orphan"}})

	if p := tr.path("X"); p != "/y/x" && p != "/x/y" {
		t.Errorf("cycle path = %q", p)
	}
	found := false
	for _, f := range tr.children[""] {
		if f.ID == "Z" {
			found = true
		}
	}
	if !found {
		t.Error("folder with unknown parent not shown at root")
	}
	if len(tr.entries[""]) != 1 {
		t.Error("entry with unknown folder not shown at root")
	}
}

func TestSecretFromFields(t *testing.T) {
	s := secretFromFields(map[string]any{
		"password": "pw",
		"totp":     map[string]any{"secret_key": totpKey, "algorithm": "SHA1", "digits": float64(6), "period": float64(30)},
	})
	if s.Password != "pw" || s.TOTPKey != totpKey || !s.TOTPSupported {
		t.Errorf("standard TOTP: %+v", s)
	}
	s = secretFromFields(map[string]any{"totp": map[string]any{"secret_key": totpKey, "digits": float64(8)}})
	if s.TOTPSupported {
		t.Error("8-digit TOTP marked supported")
	}
	s = secretFromFields(map[string]any{"password": "pw"})
	if s.TOTPKey != "" {
		t.Error("TOTP key from nothing")
	}
}

func TestBackspaceVariants(t *testing.T) {
	for _, bs := range []string{"backspace", "ctrl+h"} {
		t.Run(bs, func(t *testing.T) {
			if got := key(bs).String(); got != bs {
				t.Fatalf("key(%q).String() = %q", bs, got)
			}
			m := newLoaded(t, testData(), &fakeClip{})

			// browse: parent folder
			press(m, "enter", "enter")
			press(m, bs)
			if m.cwd != "A" {
				t.Errorf("browse: cwd = %q, want A", m.cwd)
			}

			// search: delete a character, then leave search on empty query
			press(m, "/")
			typeText(m, "ab")
			press(m, bs)
			if m.query != "a" {
				t.Errorf("search: query = %q, want a", m.query)
			}
			press(m, bs)
			if m.query != "" || m.mode != modeSearch {
				t.Errorf("search: query=%q mode=%v, want empty query still in search", m.query, m.mode)
			}
			press(m, bs)
			if m.mode != modeBrowse {
				t.Errorf("search: %s on empty query should leave search", bs)
			}

			// detail: back
			openDetail(t, m, "root mail")
			press(m, bs)
			if m.mode != modeSearch {
				t.Errorf("detail: %s should go back", bs)
			}
		})
	}
}

func TestSearchDeleteWord(t *testing.T) {
	for _, k := range []string{"ctrl+w", "alt+backspace"} {
		m := newLoaded(t, testData(), &fakeClip{})
		press(m, "/")
		typeText(m, "alpha site")
		press(m, k)
		if m.query != "alpha " {
			t.Errorf("%s: query = %q, want %q", k, m.query, "alpha ")
		}
		press(m, k)
		if m.query != "" {
			t.Errorf("%s: query = %q, want empty", k, m.query)
		}
	}
}

func TestSortModes(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	entryOrder := func() []string {
		var ids []string
		for _, r := range m.rows() {
			if r.entry != nil {
				ids = append(ids, r.entry.ID)
			}
		}
		return ids
	}

	press(m, "s")
	if m.sort != sortModified {
		t.Fatalf("sort = %v, want last modified", m.sort)
	}
	if got := entryOrder(); len(got) != 2 || got[0] != "e1" { // e1 modified 2026-10, e4 2026-06
		t.Errorf("modified order = %v, want e1 first", got)
	}
	if !strings.Contains(m.View(), "2026-10-01") {
		t.Error("date column not shown when sorting by time")
	}
	if rows := m.rows(); rows[0].folder == nil {
		t.Error("folders should stay first")
	}

	press(m, "s")
	if got := entryOrder(); m.sort != sortCreated || got[0] != "e4" { // e4 created 2026-06, e1 2026-01
		t.Errorf("created order = %v (sort %v), want e4 first", got, m.sort)
	}

	press(m, "s")
	if m.sort != sortName {
		t.Error("sort did not cycle back to name")
	}

	// search with an empty query lists everything; tab sorts it
	press(m, "/", "tab")
	if m.sort != sortModified || m.rows()[0].entry.ID != "e1" {
		t.Error("tab in search did not sort by last modified")
	}
}

func TestTwoPaneLayout(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.View()
	for _, want := range []string{"Folder", "sort: name", "1/3", "Contains", "passbolt"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	press(m, "j")
	if v := m.View(); !strings.Contains(v, "Preview") || !strings.Contains(v, "Modified") {
		t.Error("preview of a resource not shown")
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > 120 {
			t.Errorf("line %d is %d columns wide, terminal is 120", i, w)
		}
	}
	if strings.Contains(m.View(), "s3cret") {
		t.Error("preview shows a secret")
	}
}

func TestNarrowTerminalFits(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	for _, mode := range []func(){func() {}, func() { press(m, "/") }, func() { press(m, "enter") }} {
		mode()
		for i, line := range strings.Split(m.View(), "\n") {
			if w := ansi.StringWidth(line); w > 50 {
				t.Errorf("line %d is %d columns wide, terminal is 50", i, w)
			}
		}
	}
}

func TestLiveTOTPShownWithPassword(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	openDetail(t, m, "beta db")
	cmd := step(t, m, press(m, "p"))
	want, err := helper.GenerateOTPCode(totpKey, m.now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.View(), want[:3]+" "+want[3:]) {
		t.Errorf("live TOTP %s not shown", want)
	}
	if cmd == nil {
		t.Error("no clock started for the live TOTP code")
	}
	press(m, "p")
	if strings.Contains(m.View(), want[:3]+" "+want[3:]) {
		t.Error("TOTP code still shown after hiding")
	}
}
