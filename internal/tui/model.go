package tui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type clearMsg struct{ token int }

type row struct {
	folder *Folder
	entry  *Entry
}

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleFolder   = lipgloss.NewStyle().Bold(true)
)

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

	t       *tree
	loading bool
	loadErr error

	mode     viewMode
	prevMode viewMode
	cwd      string
	cursor   int
	offset   int
	query    string

	detail       *Entry
	secret       *Secret
	showPassword bool

	status string
	help   bool
	width  int
	height int

	clipArmed bool
	clipHash  [sha256.Size]byte
	clipToken int
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
		t:          newTree(nil, nil),
		loading:    true,
	}
}

// Init starts loading folders and resources.
func (m *Model) Init() tea.Cmd { return m.loadCmd() }

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
	m.status = "Decrypting..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		s, err := store.Secret(ctx, id)
		return secretMsg{id: id, action: action, secret: s, err: err}
	}
}

func (m *Model) rows() []row {
	switch m.mode {
	case modeSearch:
		found := m.t.search(m.query)
		out := make([]row, len(found))
		for i := range found {
			out[i] = row{entry: &found[i]}
		}
		return out
	default:
		folders := m.t.children[m.cwd]
		entries := m.t.entries[m.cwd]
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

func (m *Model) pageSize() int { return max(m.listHeight()-1, 1) }

func (m *Model) listHeight() int {
	h := m.height
	if h == 0 {
		h = 24
	}
	return max(h-5, 3) // header 2 lines, footer 3 lines
}

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
			m.status = "Error: " + sanitize(msg.err.Error())
			return m, nil
		}
		m.t = newTree(msg.folders, msg.entries)
		if _, ok := m.t.folders[m.cwd]; !ok {
			m.cwd = ""
		}
		m.move(0)
		m.status = fmt.Sprintf("%d folders, %d resources", len(msg.folders), len(msg.entries))
		return m, nil
	case secretMsg:
		return m, m.onSecret(msg)
	case clearMsg:
		if msg.token == m.clipToken && m.clipArmed {
			if m.clearClipboard() {
				m.status = "Clipboard cleared"
			}
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.clearClipboard()
			return m, tea.Quit
		}
		switch m.mode {
		case modeSearch:
			return m, m.keySearch(msg)
		case modeDetail:
			return m, m.keyDetail(msg)
		default:
			return m, m.keyBrowse(msg)
		}
	}
	return m, nil
}

func (m *Model) onSecret(msg secretMsg) tea.Cmd {
	if msg.err != nil {
		m.status = "Error: " + sanitize(msg.err.Error())
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
		}
	case actionCopy:
		return m.copySecret(s.Password, "Password")
	case actionTOTP:
		return m.copyTOTP(s)
	}
	return nil
}

func (m *Model) keyBrowse(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q":
		m.clearClipboard()
		return tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.pageSize())
	case "pgdown":
		m.move(m.pageSize())
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.move(len(m.rows()))
	case "enter", "l", "right":
		r, ok := m.selected()
		switch {
		case !ok:
		case r.folder != nil:
			m.cwd = r.folder.ID
			m.cursor, m.offset = 0, 0
		default:
			m.openDetail(*r.entry)
		}
	case "backspace", "h", "left":
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
	case "/":
		m.mode = modeSearch
		m.query = ""
		m.cursor, m.offset = 0, 0
	case "c":
		if r, ok := m.selected(); ok && r.entry != nil {
			return m.secretCmd(r.entry.ID, actionCopy)
		}
	case "u":
		if r, ok := m.selected(); ok && r.entry != nil {
			m.copyPlain(r.entry.Username, "Username")
		}
	case "r":
		m.loading = true
		m.status = "Reloading..."
		return m.loadCmd()
	case "?":
		m.help = !m.help
	}
	return nil
}

func (m *Model) keySearch(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.query = ""
		m.cursor, m.offset = 0, 0
	case "enter":
		if r, ok := m.selected(); ok && r.entry != nil {
			m.openDetail(*r.entry)
		}
	case "up", "ctrl+p":
		m.move(-1)
	case "down", "ctrl+n":
		m.move(1)
	case "pgup":
		m.move(-m.pageSize())
	case "pgdown":
		m.move(m.pageSize())
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor, m.offset = 0, 0
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.query += sanitize(string(msg.Runes))
			if msg.Type == tea.KeySpace && len(msg.Runes) == 0 {
				m.query += " "
			}
			m.cursor, m.offset = 0, 0
		}
	}
	return nil
}

func (m *Model) keyDetail(msg tea.KeyMsg) tea.Cmd {
	e := m.detail
	switch msg.String() {
	case "esc", "backspace", "h", "left", "q":
		m.mode = m.prevMode
		m.detail, m.secret, m.showPassword = nil, nil, false
		m.status = ""
	case "p":
		switch {
		case m.showPassword:
			m.showPassword = false
		case m.secret != nil:
			m.showPassword = true
		default:
			return m.secretCmd(e.ID, actionShow)
		}
	case "c":
		if m.secret != nil {
			return m.copySecret(m.secret.Password, "Password")
		}
		return m.secretCmd(e.ID, actionCopy)
	case "t":
		if m.secret != nil {
			return m.copyTOTP(*m.secret)
		}
		return m.secretCmd(e.ID, actionTOTP)
	case "u":
		m.copyPlain(e.Username, "Username")
	case "l":
		m.copyPlain(e.URI, "URI")
	case "?":
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

// copySecret copies text and schedules clearing it.
func (m *Model) copySecret(text, label string) tea.Cmd {
	if text == "" {
		m.status = "No " + strings.ToLower(label)
		return nil
	}
	if err := m.clip.Copy(text); err != nil {
		m.status = "Copy failed: " + sanitize(err.Error())
		return nil
	}
	m.clipArmed = true
	m.clipHash = sha256.Sum256([]byte(text))
	m.clipToken++
	token := m.clipToken
	m.status = fmt.Sprintf("%s copied via %s, clears in %s", label, m.clip.Name(), m.ClearAfter.Round(time.Second))
	return tea.Tick(m.ClearAfter, func(time.Time) tea.Msg { return clearMsg{token: token} })
}

func (m *Model) copyPlain(text, label string) {
	if text == "" {
		m.status = "No " + strings.ToLower(label)
		return
	}
	if err := m.clip.Copy(text); err != nil {
		m.status = "Copy failed: " + sanitize(err.Error())
		return
	}
	m.status = label + " copied"
}

func (m *Model) copyTOTP(s Secret) tea.Cmd {
	switch {
	case s.TOTPKey == "":
		m.status = "This resource has no TOTP"
		return nil
	case !s.TOTPSupported:
		m.status = "TOTP parameters not supported (only SHA1, 6 digits, 30 s)"
		return nil
	}
	code, err := helper.GenerateOTPCode(s.TOTPKey, m.now())
	if err != nil {
		m.status = "TOTP error: " + sanitize(err.Error())
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

// View renders the screen.
func (m *Model) View() string {
	width := m.width
	if width == 0 {
		width = 80
	}
	var b strings.Builder

	// Truncate before styling so escape sequences are never cut.
	b.WriteString(styleTitle.Render("passbolt") + "  " + styleDim.Render(truncate(m.server, max(width-10, 1))) + "\n")
	switch m.mode {
	case modeSearch:
		b.WriteString("search: " + m.query + "_\n")
	case modeDetail:
		b.WriteString(truncate(m.t.path(m.detail.FolderID), width) + "\n")
	default:
		b.WriteString(truncate(m.t.path(m.cwd), width) + "\n")
	}

	switch {
	case m.loading && m.t.all == nil && len(m.t.folders) == 0:
		b.WriteString("\nLoading...\n")
	case m.loadErr != nil && len(m.t.folders) == 0 && m.t.all == nil:
		b.WriteString("\nCould not load data. Press r to retry, q to quit.\n")
	case m.mode == modeDetail:
		b.WriteString(m.viewDetail(width))
	default:
		b.WriteString(m.viewList(width))
	}

	b.WriteString("\n" + truncate(m.status, width) + "\n")
	b.WriteString(styleDim.Render(truncate(m.hints(), width)))
	return b.String()
}

func (m *Model) viewList(width int) string {
	rows := m.rows()
	height := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
	if len(rows) == 0 {
		if m.mode == modeSearch {
			return "(no match)\n" + strings.Repeat("\n", height-1)
		}
		return "(empty)\n" + strings.Repeat("\n", height-1)
	}

	nameW := max(width*2/5, 10)
	userW := max(width/4, 8)
	var b strings.Builder
	shown := 0
	for i := m.offset; i < len(rows) && shown < height; i++ {
		r := rows[i]
		var line string
		switch {
		case r.folder != nil:
			line = truncate(sanitize(r.folder.Name)+"/", width)
		case m.mode == modeSearch:
			e := r.entry
			line = truncate(pad(sanitize(e.Name), nameW)+" "+pad(sanitize(e.Username), userW)+" "+m.t.path(e.FolderID), width)
		default:
			e := r.entry
			line = truncate(pad(sanitize(e.Name), nameW)+" "+pad(sanitize(e.Username), userW)+" "+sanitize(e.URI), width)
		}
		switch {
		case i == m.cursor:
			line = styleSelected.Render(line)
		case r.folder != nil:
			line = styleFolder.Render(line)
		}
		b.WriteString(line + "\n")
		shown++
	}
	b.WriteString(strings.Repeat("\n", height-shown))
	return b.String()
}

func (m *Model) viewDetail(width int) string {
	e := m.detail
	password := "********"
	if m.showPassword && m.secret != nil {
		password = sanitize(m.secret.Password)
	}
	lines := []string{
		"",
		"Name:      " + sanitize(e.Name),
		"Folder:    " + m.t.path(e.FolderID),
		"Username:  " + sanitize(e.Username),
		"URI:       " + sanitize(e.URI),
		"Password:  " + password,
	}
	if m.secret != nil {
		totp := "none"
		if m.secret.TOTPKey != "" {
			totp = "available (t to copy)"
			if !m.secret.TOTPSupported {
				totp = "unsupported parameters"
			}
		}
		lines = append(lines, "TOTP:      "+totp)
	}
	if e.Description != "" {
		lines = append(lines, "", "Description:", sanitize(e.Description))
	}
	var b strings.Builder
	used := 0
	for _, l := range lines {
		if used >= m.listHeight() {
			break
		}
		b.WriteString(truncate(l, width) + "\n")
		used++
	}
	b.WriteString(strings.Repeat("\n", max(m.listHeight()-used, 0)))
	return b.String()
}

func (m *Model) hints() string {
	switch m.mode {
	case modeSearch:
		return "type to filter  up/down move  enter open  esc back  ctrl+c quit"
	case modeDetail:
		if m.help {
			return "p show/hide password  c copy password  u copy username  l copy URI  t copy TOTP  esc back  ctrl+c quit"
		}
		return "p show  c copy pw  u user  l uri  t totp  esc back  ? help"
	default:
		if m.help {
			return "j/k move  enter open  h/backspace parent  / search  c copy password  u copy username  r reload  q quit"
		}
		return "enter open  h back  / search  c copy pw  u user  r reload  q quit  ? help"
	}
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return string(r[:1])
	}
	return string(r[:width-1]) + "~"
}

func pad(s string, width int) string {
	s = truncate(s, width)
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
