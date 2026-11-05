package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/passbolt/go-passbolt/helper"
)

type fakeStore struct {
	folders  []Folder
	entries  []Entry
	secrets  map[string]Secret
	loadErr  error
	calls    int
	perms    map[string][]Share
	permErr  error
	favCalls []string
	favErr   error
}

func (f *fakeStore) SetFavorite(_ context.Context, id, favID string, add bool) (string, error) {
	f.favCalls = append(f.favCalls, fmt.Sprintf("%s:%s:%v", id, favID, add))
	if f.favErr != nil {
		return "", f.favErr
	}
	if add {
		return "fav-" + id, nil
	}
	return "", nil
}

func (f *fakeStore) Permissions(_ context.Context, id string) ([]Share, error) {
	return f.perms[id], f.permErr
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
			{ID: "e1", Name: "Root Mail", Username: "root@example.com", URI: "https://mail.example.com", MyAccess: accessOwner,
				Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Modified: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "e2", FolderID: "A", Name: "Alpha Site", Username: "alpha"},
			{ID: "e3", FolderID: "B", Name: "Beta DB", Username: "dbadmin", Description: "primary"},
			{ID: "e4", Name: "\x1b[31mEvil\x07", Username: "x",
				Created: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Modified: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
		perms: map[string][]Share{
			"e1": {
				{Name: "Ana Petrova", Detail: "ana@example.com", Type: accessOwner},
				{Group: true, Name: "Ops", Type: accessUpdate},
				{Name: "Ivan", Detail: "ivan@example.com", Type: accessRead},
			},
			"e3": {{Name: "Me", Type: accessOwner}},
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
	press(m, "v") // top -> right
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

// TestColumnsSizedToContent guards against the username column being pushed
// to the far right on wide terminals.
func TestColumnsSizedToContent(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 230, Height: 20})
	for _, line := range strings.Split(m.View(), "\n") {
		i := strings.Index(line, "Root Mail")
		if i < 0 {
			continue
		}
		j := strings.Index(line, "root@example.com")
		// longest name in the root folder is "Root Mail" (9) -> gap of 2
		if got := ansi.StringWidth(line[i:j]); got != len("Root Mail")+colGap {
			t.Errorf("username starts %d columns after the name, want %d", got, len("Root Mail")+colGap)
		}
		return
	}
	t.Fatal("Root Mail row not found")
}

func TestDistribute(t *testing.T) {
	cases := []struct{ space, n, u, x, wn, wu, wx int }{
		{100, 20, 10, 30, 20, 10, 30}, // fits: content widths
		{40, 60, 30, 30, 20, 10, 10},  // tight: user/extra a quarter each
		{60, 10, 40, 40, 10, 35, 15},  // short names give space back to user, then extra
	}
	for _, c := range cases {
		n, u, x := distribute(c.space, c.n, c.u, c.x)
		if n != c.wn || u != c.wu || x != c.wx {
			t.Errorf("distribute(%d, %d, %d, %d) = %d, %d, %d; want %d, %d, %d", c.space, c.n, c.u, c.x, n, u, x, c.wn, c.wu, c.wx)
		}
		if n+u+x > c.space {
			t.Errorf("distribute(%d, ...) overflows: %d", c.space, n+u+x)
		}
	}
}

func TestDetailShowsAccess(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	press(m, "/")
	typeText(m, "root mail")
	cmd := press(m, "enter")
	if !strings.Contains(m.View(), "loading...") {
		t.Error("access not shown as loading")
	}
	step(t, m, cmd)
	v := m.View()
	for _, want := range []string{"with 2 users, 1 groups", "Ana Petrova", "<ana@example.com>", "can update", "Ops", "can read", "You", "owner"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if strings.Index(v, "Ana Petrova") > strings.Index(v, "Ivan") {
		t.Error("owner not listed first")
	}
}

func TestDetailNotShared(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	press(m, "/")
	typeText(m, "beta db")
	step(t, m, press(m, "enter"))
	if !strings.Contains(m.View(), "no (only you)") {
		t.Error("single permission not shown as not shared")
	}
}

func TestAccessErrorAndStaleResult(t *testing.T) {
	store := testData()
	store.permErr = errors.New("forbidden")
	m := newLoaded(t, store, &fakeClip{})
	press(m, "/")
	typeText(m, "root mail")
	stale := press(m, "enter")
	press(m, "esc") // back to search
	typeText(m, "") // still "root mail"
	press(m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	typeText(m, "beta db")
	fresh := press(m, "enter") // detail of e3
	step(t, m, stale)          // result for e1 arrives late
	if m.detail.ID != "e3" || m.sharesErr != nil || !m.sharesBusy {
		t.Fatal("late result for another resource was applied")
	}
	step(t, m, fresh)
	if !strings.Contains(m.View(), "could not load: forbidden") {
		t.Error("permission error not shown")
	}
}

func TestPreviewShowsMyAccess(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	for selectedName(m) != "Root Mail" {
		press(m, "j")
	}
	if v := m.View(); !strings.Contains(v, "Access") || !strings.Contains(v, "owner") {
		t.Error("top preview lacks the access level")
	}
	press(m, "v")
	if !strings.Contains(m.View(), "Your access") {
		t.Error("right preview lacks Your access")
	}
}

func TestTopPreviewLayout(t *testing.T) {
	m := newLoaded(t, testData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for selectedName(m) != "Root Mail" {
		press(m, "j")
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Fatalf("frame has %d lines, want 30", len(lines))
	}
	// preview strip directly under the header, the list below it
	if !strings.Contains(lines[1], "Root Mail") || !strings.Contains(lines[2], "root@example.com") || !strings.Contains(lines[2], "2026-10-01") {
		t.Errorf("preview strip not on top:\n%s\n%s\n%s", lines[1], lines[2], lines[3])
	}
	if !strings.Contains(lines[1+topPreviewHeight], "/") || !strings.Contains(lines[2+topPreviewHeight], "Name") {
		t.Errorf("list box not below the preview:\n%s\n%s", lines[1+topPreviewHeight], lines[2+topPreviewHeight])
	}
	// the list uses the full width: the URI column is not cut
	if !strings.Contains(m.View(), "https://mail.example.com") {
		t.Error("URI cut although the list has the full width")
	}

	press(m, "v", "v") // top -> right -> off
	if m.preview != previewOff || strings.Contains(m.View(), "Created") {
		t.Error("preview not hidden")
	}
	press(m, "v")
	if m.preview != previewTop {
		t.Error("v did not cycle back to top")
	}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	if m.effectivePreview() != previewOff {
		t.Error("top preview not hidden on a low terminal")
	}
}

func TestManySharesAreCutWithNote(t *testing.T) {
	store := testData()
	for i := range 40 {
		store.perms["e1"] = append(store.perms["e1"], Share{Name: fmt.Sprintf("user%02d", i), Type: accessRead})
	}
	m := newLoaded(t, store, &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	press(m, "/")
	typeText(m, "root mail")
	step(t, m, press(m, "enter"))
	v := m.View()
	if !strings.Contains(v, "more lines") {
		t.Error("no note about cut lines")
	}
	if n := len(strings.Split(v, "\n")); n > 24 {
		t.Errorf("view has %d lines, terminal has 24", n)
	}
}

// TestFrameNeverOverflows renders awkward data in every mode at several sizes:
// the frame must fit the terminal height and keep the last column free, or a
// wrapped line scrolls the screen and duplicates borders.
func TestFrameNeverOverflows(t *testing.T) {
	store := testData()
	weird := []string{
		"line\u2028separator", "zero\u200bwidth\u200djoiner", "soft\u00adhyphen\ufeffbom",
		"emoji 🔐🧑\u200d💻 keys", "Български институт за стандартизация(ISO/IEC 27001)",
		strings.Repeat("very-long-name-", 30), "tab\there\r\nnewline\u0085nel",
	}
	for i, w := range weird {
		store.entries = append(store.entries, Entry{
			ID: fmt.Sprint("w", i), Name: w, Username: w, URI: "https://" + w,
			Description: strings.Repeat(w+" ", 20) + "\n\n\nmore\nlines",
		})
		store.perms[fmt.Sprint("w", i)] = []Share{{Name: w, Detail: w, Type: accessOwner}, {Group: true, Name: w, Type: accessRead}}
	}
	for _, size := range [][2]int{{230, 40}, {140, 30}, {100, 20}, {80, 24}, {50, 10}} {
		m := newLoaded(t, store, &fakeClip{})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		check := func(state string) {
			t.Helper()
			lines := strings.Split(m.View(), "\n")
			if len(lines) > size[1] {
				t.Errorf("%dx%d %s: %d lines", size[0], size[1], state, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w >= size[0] {
					t.Errorf("%dx%d %s: line %d is %d wide", size[0], size[1], state, i, w)
				}
				if strings.ContainsAny(l, "\u2028\u2029\u200b\u200d\u00ad\ufeff\t\r") {
					t.Errorf("%dx%d %s: line %d contains a control/format character", size[0], size[1], state, i)
				}
			}
		}
		check("browse")
		for range len(store.entries) + 3 {
			press(m, "j")
			check("browse scrolled")
		}
		press(m, "s")
		check("browse sorted")
		for i := range weird {
			press(m, "/")
			typeText(m, strings.Fields(sanitize(weird[i]))[0])
			check("search")
			if cmd := press(m, "enter"); cmd != nil {
				step(t, m, cmd)
			}
			check("detail")
			press(m, "esc", "esc")
		}
	}
}

func favData() *fakeStore {
	store := testData()
	store.entries[2].FavoriteID = "fav-e3" // Beta DB in /Alpha/Beta
	return store
}

func TestFavoritesFolderRowAndView(t *testing.T) {
	m := newLoaded(t, favData(), &fakeClip{})
	if r, _ := m.selected(); !r.favs {
		t.Fatal("first root row is not Favorites/")
	}
	if v := m.View(); !strings.Contains(v, "Favorites/") || !strings.Contains(v, "1 item") {
		t.Error("Favorites/ row or its count not shown")
	}
	press(m, "enter")
	if !m.favOnly || selectedName(m) != "Beta DB" || len(m.rows()) != 1 {
		t.Fatalf("favorites view: favOnly=%v selected=%q rows=%d", m.favOnly, selectedName(m), len(m.rows()))
	}
	if v := m.View(); !strings.Contains(v, "Favorites") || !strings.Contains(v, "/Alpha/Beta") {
		t.Error("favorites view lacks its title or the folder path")
	}
	press(m, "enter")
	if m.mode != modeDetail || !strings.Contains(m.View(), "Favorite   yes") {
		t.Error("detail does not show Favorite yes")
	}
	press(m, "esc")
	if !m.favOnly {
		t.Error("back from detail left the favorites view")
	}
	press(m, "h")
	if m.favOnly || m.cursor != 0 {
		t.Error("h did not return to the root with the cursor on Favorites/")
	}
	press(m, "F")
	if !m.favOnly {
		t.Error("F did not open the favorites view")
	}
	press(m, "F")
	if m.favOnly {
		t.Error("F did not close the favorites view")
	}
}

func TestToggleFavorite(t *testing.T) {
	store := testData()
	m := newLoaded(t, store, &fakeClip{})
	if r, _ := m.selected(); r.favs {
		t.Fatal("Favorites/ shown without favorites")
	}
	for selectedName(m) != "Root Mail" {
		press(m, "j")
	}
	step(t, m, press(m, "f"))
	if len(store.favCalls) != 1 || store.favCalls[0] != "e1::true" {
		t.Fatalf("SetFavorite calls = %v", store.favCalls)
	}
	if !strings.Contains(m.status, "Added") {
		t.Errorf("status = %q", m.status)
	}
	if favs := m.t.favorites(); len(favs) != 1 || favs[0].FavoriteID != "fav-e1" {
		t.Fatalf("favorites after add = %+v", favs)
	}
	if selectedName(m) != "Root Mail" {
		t.Errorf("cursor moved to %q when Favorites/ appeared", selectedName(m))
	}
	if !strings.Contains(m.View(), ">*") {
		t.Error("favorite mark not shown on the selected row")
	}

	// remove from the favorites view: the list shrinks to empty
	press(m, "F")
	step(t, m, press(m, "f"))
	if store.favCalls[1] != "e1:fav-e1:false" {
		t.Errorf("remove call = %q", store.favCalls[1])
	}
	if len(m.rows()) != 0 || !strings.Contains(m.View(), "No favorites yet") {
		t.Error("favorites view not empty after removing the last favorite")
	}
}

func TestToggleFavoriteInDetailAndError(t *testing.T) {
	store := testData()
	m := newLoaded(t, store, &fakeClip{})
	openDetail(t, m, "root mail")
	step(t, m, press(m, "f"))
	if m.detail.FavoriteID != "fav-e1" || !strings.Contains(m.View(), "Favorite   yes") {
		t.Error("detail not updated after adding the favorite")
	}

	store.favErr = errors.New("forbidden")
	step(t, m, press(m, "f"))
	if m.detail.FavoriteID != "fav-e1" || m.statusKind != statusError {
		t.Error("failed removal changed the state or showed no error")
	}
}

func TestFavoritesPreviews(t *testing.T) {
	m := newLoaded(t, favData(), &fakeClip{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(m.View(), "marked as favorite") {
		t.Error("top preview of Favorites/ missing")
	}
	press(m, "v") // right preview: must not crash on the pseudo-folder
	if !strings.Contains(m.View(), "Favorites") {
		t.Error("right preview of Favorites/ missing")
	}
}

func TestReloadKeepsSelection(t *testing.T) {
	store := testData()
	m := newLoaded(t, store, &fakeClip{})
	for selectedName(m) != "Root Mail" {
		press(m, "j")
	}
	store.entries = append(store.entries, Entry{ID: "new", Name: "AAA first"}) // sorts above Root Mail
	step(t, m, press(m, "r"))
	if selectedName(m) != "Root Mail" {
		t.Errorf("after reload the cursor is on %q", selectedName(m))
	}
}
