package tui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/passbolt/go-passbolt/helper"
)

type viewMode int

const (
	modeBrowse viewMode = iota
	modeSearch
	modeDetail
)

// Secret-fetch actions.
const (
	actionShow = "show"
	actionCopy = "copy"
	actionTOTP = "totp"
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusError
)

type loadedMsg struct {
	folders []Folder
	entries []Entry
	err     error
}

type secretMsg struct {
	id     string
	action string
	secret Secret
	err    error
}

// clockMsg drives the clipboard countdown, the clear and the live TOTP code.
type clockMsg struct{}

type row struct {
	folder *Folder
	entry  *Entry
}

// Model is the bubbletea model of the TUI.
type Model struct {
	store   Store
	clip    Clipboard
	server  string
	timeout time.Duration

	// ClearAfter is how long a copied password or TOTP code stays in the
	// clipboard.
	ClearAfter time.Duration
	now        func() time.Time
	tickEvery  time.Duration
	ticking    bool

	t       *tree
	loading bool
	loadErr error

	mode     viewMode
	prevMode viewMode
	cwd      string
	cursor   int
	offset   int
	query    string
	sort     sortMode

	detail       *Entry
	secret       *Secret
	showPassword bool

	status     string
	statusKind statusKind
	help       bool
	width      int
	height     int

	clipArmed   bool
	clipHash    [sha256.Size]byte
	clipLabel   string
	clipExpires time.Time
}

// New returns the model; timeout bounds each store call.
func New(store Store, clip Clipboard, server string, timeout time.Duration) *Model {
	return &Model{
		store:      store,
		clip:       clip,
		server:     sanitize(server),
		timeout:    timeout,
		ClearAfter: 30 * time.Second,
		now:        time.Now,
		tickEvery:  time.Second,
		t:          newTree(nil, nil),
		loading:    true,
	}
}

// Init starts loading folders and resources.
func (m *Model) Init() tea.Cmd { return m.loadCmd() }

func (m *Model) setStatus(kind statusKind, format string, a ...any) {
	m.statusKind = kind
	m.status = sanitize(fmt.Sprintf(format, a...))
}

func (m *Model) loadCmd() tea.Cmd {
	store, timeout := m.store, m.timeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		f, e, err := store.Load(ctx)
		return loadedMsg{folders: f, entries: e, err: err}
	}
}

func (m *Model) secretCmd(id, action string) tea.Cmd {
	store, timeout := m.store, m.timeout
	m.setStatus(statusInfo, "Decrypting...")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		s, err := store.Secret(ctx, id)
		return secretMsg{id: id, action: action, secret: s, err: err}
	}
}

// clockCmd starts the one-second clock unless it is already running.
func (m *Model) clockCmd() tea.Cmd {
	if m.ticking || !m.needsClock() {
		return nil
	}
	m.ticking = true
	return tea.Tick(m.tickEvery, func(time.Time) tea.Msg { return clockMsg{} })
}

func (m *Model) needsClock() bool {
	return m.clipArmed || m.liveTOTP()
}

func (m *Model) liveTOTP() bool {
	return m.mode == modeDetail && m.showPassword && m.secret != nil && m.secret.TOTPKey != "" && m.secret.TOTPSupported
}

func (m *Model) rows() []row {
	switch m.mode {
	case modeSearch:
		found := sortEntries(m.t.search(m.query), m.sort)
		out := make([]row, len(found))
		for i := range found {
			out[i] = row{entry: &found[i]}
		}
		return out
	default:
		folders := m.t.children[m.cwd]
		entries := sortEntries(m.t.entries[m.cwd], m.sort)
		out := make([]row, 0, len(folders)+len(entries))
		for i := range folders {
			out = append(out, row{folder: &folders[i]})
		}
		for i := range entries {
			out = append(out, row{entry: &entries[i]})
		}
		return out
	}
}

func (m *Model) selected() (row, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

func (m *Model) move(delta int) {
	n := len(m.rows())
	m.cursor = max(0, min(m.cursor+delta, n-1))
}

func (m *Model) pageSize() int { return max(m.listRows()-1, 1) }

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case loadedMsg:
		m.loading = false
		m.loadErr = msg.err
		if msg.err != nil {
			m.setStatus(statusError, "Error: %v", msg.err)
			return m, nil
		}
		m.t = newTree(msg.folders, msg.entries)
		if _, ok := m.t.folders[m.cwd]; !ok {
			m.cwd = ""
		}
		m.move(0)
		m.setStatus(statusInfo, "Loaded %d folders, %d resources", len(msg.folders), len(msg.entries))
		return m, nil
	case secretMsg:
		return m, m.onSecret(msg)
	case clockMsg:
		m.ticking = false
		if m.clipArmed && !m.now().Before(m.clipExpires) {
			if m.clearClipboard() {
				m.setStatus(statusInfo, "Clipboard cleared")
			}
		}
		return m, m.clockCmd()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.clearClipboard()
			return m, tea.Quit
		}
		var cmd tea.Cmd
		switch m.mode {
		case modeSearch:
			cmd = m.keySearch(msg)
		case modeDetail:
			cmd = m.keyDetail(msg)
		default:
			cmd = m.keyBrowse(msg)
		}
		if cmd == nil {
			cmd = m.clockCmd() // e.g. password shown with a live TOTP code
		}
		return m, cmd
	}
	return m, nil
}

func (m *Model) onSecret(msg secretMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus(statusError, "Error: %v", msg.err)
		return nil
	}
	s := msg.secret
	if m.mode == modeDetail && m.detail != nil && m.detail.ID == msg.id {
		m.secret = &s
	}
	switch msg.action {
	case actionShow:
		if m.secret != nil && m.detail != nil && m.detail.ID == msg.id {
			m.showPassword = true
			m.status = ""
			return m.clockCmd()
		}
	case actionCopy:
		return m.copySecret(s.Password, "Password")
	case actionTOTP:
		return m.copyTOTP(s)
	}
	return nil
}

func isBackspace(k string) bool { return k == "backspace" || k == "ctrl+h" } // DEL or ^H

func (m *Model) keyBrowse(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch {
	case k == "q":
		m.clearClipboard()
		return tea.Quit
	case k == "up" || k == "k":
		m.move(-1)
	case k == "down" || k == "j":
		m.move(1)
	case k == "pgup":
		m.move(-m.pageSize())
	case k == "pgdown":
		m.move(m.pageSize())
	case k == "home" || k == "g":
		m.cursor = 0
	case k == "end" || k == "G":
		m.move(len(m.rows()))
	case k == "enter" || k == "l" || k == "right":
		r, ok := m.selected()
		switch {
		case !ok:
		case r.folder != nil:
			m.cwd = r.folder.ID
			m.cursor, m.offset = 0, 0
		default:
			m.openDetail(*r.entry)
		}
	case isBackspace(k) || k == "h" || k == "left":
		if m.cwd == "" {
			return nil
		}
		prev := m.cwd
		m.cwd = m.t.parent(m.cwd)
		m.cursor, m.offset = 0, 0
		for i, r := range m.rows() {
			if r.folder != nil && r.folder.ID == prev {
				m.cursor = i
				break
			}
		}
	case k == "/":
		m.mode = modeSearch
		m.query = ""
		m.cursor, m.offset = 0, 0
	case k == "c":
		if r, ok := m.selected(); ok && r.entry != nil {
			return m.secretCmd(r.entry.ID, actionCopy)
		}
	case k == "u":
		if r, ok := m.selected(); ok && r.entry != nil {
			m.copyPlain(r.entry.Username, "Username")
		}
	case k == "r":
		m.loading = true
		m.setStatus(statusInfo, "Reloading...")
		return m.loadCmd()
	case k == "s" || k == "tab":
		m.cycleSort()
	case k == "?":
		m.help = !m.help
	}
	return nil
}

func (m *Model) cycleSort() {
	m.sort = m.sort.next()
	m.cursor, m.offset = 0, 0
	m.setStatus(statusInfo, "Sorted by %s", m.sort)
}

func (m *Model) keySearch(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch {
	case k == "esc":
		m.mode = modeBrowse
		m.query = ""
		m.cursor, m.offset = 0, 0
	case k == "enter":
		if r, ok := m.selected(); ok && r.entry != nil {
			m.openDetail(*r.entry)
		}
	case k == "up" || k == "ctrl+p":
		m.move(-1)
	case k == "down" || k == "ctrl+n":
		m.move(1)
	case k == "pgup":
		m.move(-m.pageSize())
	case k == "pgdown":
		m.move(m.pageSize())
	case k == "tab":
		m.cycleSort()
	case isBackspace(k):
		r := []rune(m.query)
		if len(r) == 0 {
			m.mode = modeBrowse
			m.cursor, m.offset = 0, 0
			return nil
		}
		m.query = string(r[:len(r)-1])
		m.cursor, m.offset = 0, 0
	case k == "alt+backspace" || k == "ctrl+w":
		q := strings.TrimRight(m.query, " ")
		if i := strings.LastIndex(q, " "); i >= 0 {
			m.query = q[:i+1]
		} else {
			m.query = ""
		}
		m.cursor, m.offset = 0, 0
	case msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace:
		text := string(msg.Runes)
		if msg.Type == tea.KeySpace && text == "" {
			text = " "
		}
		m.query += sanitize(text)
		m.cursor, m.offset = 0, 0
	}
	return nil
}

func (m *Model) keyDetail(msg tea.KeyMsg) tea.Cmd {
	e := m.detail
	k := msg.String()
	switch {
	case k == "esc" || isBackspace(k) || k == "h" || k == "left" || k == "q":
		m.mode = m.prevMode
		m.detail, m.secret, m.showPassword = nil, nil, false
		m.status = ""
	case k == "p":
		switch {
		case m.showPassword:
			m.showPassword = false
		case m.secret != nil:
			m.showPassword = true
		default:
			return m.secretCmd(e.ID, actionShow)
		}
	case k == "c":
		if m.secret != nil {
			return m.copySecret(m.secret.Password, "Password")
		}
		return m.secretCmd(e.ID, actionCopy)
	case k == "t":
		if m.secret != nil {
			return m.copyTOTP(*m.secret)
		}
		return m.secretCmd(e.ID, actionTOTP)
	case k == "u":
		m.copyPlain(e.Username, "Username")
	case k == "l":
		m.copyPlain(e.URI, "URI")
	case k == "?":
		m.help = !m.help
	}
	return nil
}

func (m *Model) openDetail(e Entry) {
	m.prevMode = m.mode
	m.mode = modeDetail
	m.detail = &e
	m.secret, m.showPassword = nil, false
	m.status = ""
}

// copySecret copies text and arms the clear after ClearAfter.
func (m *Model) copySecret(text, label string) tea.Cmd {
	if text == "" {
		m.setStatus(statusError, "No %s", strings.ToLower(label))
		return nil
	}
	if err := m.clip.Copy(text); err != nil {
		m.setStatus(statusError, "Copy failed: %v", err)
		return nil
	}
	m.clipArmed = true
	m.clipHash = sha256.Sum256([]byte(text))
	m.clipLabel = label
	m.clipExpires = m.now().Add(m.ClearAfter)
	m.setStatus(statusOK, "%s copied via %s", label, m.clip.Name())
	return m.clockCmd()
}

func (m *Model) copyPlain(text, label string) {
	if text == "" {
		m.setStatus(statusError, "No %s", strings.ToLower(label))
		return
	}
	if err := m.clip.Copy(text); err != nil {
		m.setStatus(statusError, "Copy failed: %v", err)
		return
	}
	m.setStatus(statusOK, "%s copied", label)
}

func (m *Model) totpCode(s Secret) (string, error) {
	return helper.GenerateOTPCode(s.TOTPKey, m.now())
}

func (m *Model) copyTOTP(s Secret) tea.Cmd {
	switch {
	case s.TOTPKey == "":
		m.setStatus(statusError, "This resource has no TOTP")
		return nil
	case !s.TOTPSupported:
		m.setStatus(statusError, "TOTP parameters not supported (only SHA1, 6 digits, 30 s)")
		return nil
	}
	code, err := m.totpCode(s)
	if err != nil {
		m.setStatus(statusError, "TOTP error: %v", err)
		return nil
	}
	return m.copySecret(code, "TOTP code")
}

// clearClipboard empties the clipboard if it still holds the copied secret
// (or cannot be read). It returns true if it cleared.
func (m *Model) clearClipboard() bool {
	if !m.clipArmed {
		return false
	}
	m.clipArmed = false
	if cur, ok := m.clip.Read(); ok && sha256.Sum256([]byte(cur)) != m.clipHash {
		return false // the user copied something else meanwhile
	}
	return m.clip.Copy("") == nil
}

// ClearClipboard is called on exit to remove a pending secret.
func (m *Model) ClearClipboard() { m.clearClipboard() }
