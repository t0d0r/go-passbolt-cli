package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colors adapt to light and dark terminal backgrounds.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5B4BDB", Dark: "#A78BFA"}
	colSubtle = lipgloss.AdaptiveColor{Light: "#C9C9D6", Dark: "#4B4B63"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6B6B80", Dark: "#9090A8"}
	colText   = lipgloss.AdaptiveColor{Light: "#1F1F2E", Dark: "#E6E6F0"}
	colOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#4ADE80"}
	colErr    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F87171"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FBBF24"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#E4DEFF", Dark: "#3B2F6B"}

	styleBadge    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colAccent).Padding(0, 1)
	styleMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styleText     = lipgloss.NewStyle().Foreground(colText)
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleFolder   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(colText).Background(colSelBg)
	styleLabel    = lipgloss.NewStyle().Foreground(colMuted)
	styleName     = lipgloss.NewStyle().Bold(true).Foreground(colText)
	styleSecret   = lipgloss.NewStyle().Bold(true).Foreground(colWarn)
	styleKey      = lipgloss.NewStyle().Foreground(colText).Background(colSubtle).Padding(0, 1)
	styleOK       = lipgloss.NewStyle().Foreground(colOK)
	styleErr      = lipgloss.NewStyle().Bold(true).Foreground(colErr)
	styleWarn     = lipgloss.NewStyle().Foreground(colWarn)
)

const (
	dateLayout    = "2006-01-02 15:04"
	twoPaneWidth  = 100
	headerLines   = 1
	footerLines   = 2
	minMainHeight = 5

	topPreviewHeight    = 6  // border + 4 rows
	topPreviewMinHeight = 16 // terminals lower than this hide the top preview
	rightPreviewMax     = 60
)

// previewMode places the preview of the selected row.
type previewMode int

const (
	previewTop previewMode = iota
	previewRight
	previewOff
)

func (p previewMode) String() string {
	switch p {
	case previewRight:
		return "right"
	case previewOff:
		return "off"
	default:
		return "top"
	}
}

// effectivePreview is the preview mode that fits the terminal.
func (m *Model) effectivePreview() previewMode {
	w, h := m.size()
	switch {
	case m.preview == previewTop && h < topPreviewMinHeight:
		return previewOff
	case m.preview == previewRight && w < twoPaneWidth:
		return previewOff
	}
	return m.preview
}

func (m *Model) size() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// mainHeight is the height of the list box (or the detail card).
func (m *Model) mainHeight() int {
	_, h := m.size()
	h -= headerLines + footerLines
	if m.mode != modeDetail && m.effectivePreview() == previewTop {
		h -= topPreviewHeight
	}
	return max(h, minMainHeight)
}

// listRows is the number of list rows inside the list box (minus borders
// and the column header).
func (m *Model) listRows() int { return max(m.mainHeight()-3, 1) }

// fit truncates s to width w (ANSI and wide-character aware) and pads it.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "~")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// box draws a rounded border of w x h with a title in the top border and a
// footer in the bottom border.
func box(title, footer string, lines []string, w, h int, focused bool) string {
	b := lipgloss.RoundedBorder()
	border := lipgloss.NewStyle().Foreground(colSubtle)
	if focused {
		border = lipgloss.NewStyle().Foreground(colAccent)
	}
	inner := max(w-2, 1)

	edge := func(left, fill, right, label string, style lipgloss.Style) string {
		if label == "" || ansi.StringWidth(label)+4 > inner {
			return border.Render(left + strings.Repeat(fill, inner) + right)
		}
		label = " " + label + " "
		rest := inner - 1 - ansi.StringWidth(label)
		return border.Render(left+fill) + style.Render(label) + border.Render(strings.Repeat(fill, rest)+right)
	}

	var out strings.Builder
	out.WriteString(edge(b.TopLeft, b.Top, b.TopRight, ansi.Truncate(title, max(inner-6, 0), "~"), styleTitle) + "\n")
	for i := range max(h-2, 0) {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out.WriteString(border.Render(b.Left) + fit(line, inner) + border.Render(b.Right) + "\n")
	}
	if footer == "" {
		out.WriteString(edge(b.BottomLeft, b.Bottom, b.BottomRight, "", styleMuted))
	} else {
		// footer right-aligned in the bottom border
		label := " " + footer + " "
		fill := inner - 1 - ansi.StringWidth(label)
		if fill < 1 {
			out.WriteString(edge(b.BottomLeft, b.Bottom, b.BottomRight, "", styleMuted))
		} else {
			out.WriteString(border.Render(b.BottomLeft+strings.Repeat(b.Bottom, fill)) + styleMuted.Render(label) + border.Render(b.Bottom+b.BottomRight))
		}
	}
	return out.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(dateLayout)
}

// View renders the screen. The last terminal column stays empty and the
// frame is clipped to the terminal height: a line drawn wider than computed
// (ambiguous-width or emoji characters) must not wrap, because a wrapped
// line scrolls the screen and leaves stale frame parts behind.
func (m *Model) View() string {
	termW, termH := m.size()
	w := max(termW-1, 1)
	var main string
	switch {
	case m.mode == modeDetail:
		main = m.viewDetail(w)
	case m.effectivePreview() == previewTop:
		main = lipgloss.JoinVertical(lipgloss.Left, m.viewPreviewTop(w), m.viewList(w))
	case m.effectivePreview() == previewRight:
		pw := min(max(w/3, 40), rightPreviewMax)
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.viewList(w-pw), m.viewPreview(pw))
	default:
		main = m.viewList(w)
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(w), main, m.viewStatus(w), m.viewHints(w))
	lines := strings.Split(frame, "\n")
	if len(lines) > termH {
		lines = lines[:termH]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) viewHeader(w int) string {
	left := styleBadge.Render("passbolt") + " " + styleMuted.Render(m.server)
	right := ""
	if !m.loading || len(m.t.folders) > 0 || m.t.all != nil {
		// two trailing spaces keep the text clear of terminal overlays
		right = styleMuted.Render(fmt.Sprintf("%d folders | %d resources  ", len(m.t.folders), len(m.t.all)))
	}
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) viewList(w int) string {
	h := m.mainHeight()
	inner := w - 2
	rows := m.rows()

	title := m.t.path(m.cwd)
	if m.mode == modeSearch {
		title = "Search: " + m.query + "_"
	}
	footer := "sort: " + m.sort.String()
	if len(rows) > 0 {
		footer += fmt.Sprintf(" | %d/%d", m.cursor+1, len(rows))
	}

	var lines []string
	switch {
	case m.loading && m.t.all == nil && len(m.t.folders) == 0:
		lines = []string{"", styleMuted.Render("  Loading folders and resources...")}
	case m.loadErr != nil && m.t.all == nil && len(m.t.folders) == 0:
		lines = []string{"", styleErr.Render("  Could not load data."), styleMuted.Render("  Press r to retry, q to quit.")}
	case len(rows) == 0 && m.mode == modeSearch:
		lines = []string{"", styleMuted.Render("  No match")}
	case len(rows) == 0:
		lines = []string{"", styleMuted.Render("  (empty folder)")}
	default:
		lines = m.listLines(rows, inner)
	}
	return box(title, footer, lines, w, h, true)
}

// columns holds the list column widths for one render.
type columns struct {
	name, user, date, extra int
}

const colGap = 2

func (m *Model) entryTime(e *Entry) time.Time {
	if m.sort == sortCreated {
		return e.Created
	}
	return e.Modified
}

func (m *Model) entryExtra(e *Entry) string {
	if m.mode == modeSearch {
		return m.t.path(e.FolderID)
	}
	return sanitize(e.URI)
}

func folderCount(t *tree, id string) string {
	n := len(t.children[id]) + len(t.entries[id])
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// distribute fits name, user and extra widths into space. If everything
// fits, each column is as wide as its longest value; otherwise user and
// extra get up to a quarter each, the name the rest, and space the name
// does not need goes back to user, then extra.
func distribute(space, name, user, extra int) (int, int, int) {
	if name+user+extra <= space {
		return name, user, extra
	}
	u, x := min(user, space/4), min(extra, space/4)
	n := space - u - x
	if n > name {
		spare := n - name
		n = name
		add := min(user-u, spare)
		u += add
		x += min(extra-x, spare-add)
	}
	return max(n, 4), u, x
}

// layoutColumns sizes the columns from the content of all rows, so widths
// stay stable while scrolling. The extra column (URI, or folder path in
// search) is dropped when it would be cut below 12 columns.
func (m *Model) layoutColumns(rows []row, avail int) columns {
	maxName, maxUser, maxExtra := len("Name"), len("Username"), 0
	for _, r := range rows {
		if r.folder != nil {
			maxName = max(maxName, ansi.StringWidth(sanitize(r.folder.Name))+1)
			maxUser = max(maxUser, ansi.StringWidth(folderCount(m.t, r.folder.ID)))
			continue
		}
		maxName = max(maxName, ansi.StringWidth(sanitize(r.entry.Name)))
		maxUser = max(maxUser, ansi.StringWidth(sanitize(r.entry.Username)))
		maxExtra = max(maxExtra, ansi.StringWidth(m.entryExtra(r.entry)))
	}
	var c columns
	space := avail - colGap
	if m.sort != sortName {
		c.date = len(dateLayout)
		space -= c.date + colGap
	}
	if maxExtra > 0 {
		maxExtra = max(maxExtra, len("Folder"))
		n, u, x := distribute(space-colGap, maxName, maxUser, maxExtra)
		if x >= min(12, maxExtra) {
			c.name, c.user, c.extra = n, u, x
			return m.addSpareDate(c, avail)
		}
	}
	c.name, c.user, _ = distribute(space, maxName, maxUser, 0)
	return m.addSpareDate(c, avail)
}

// addSpareDate shows the modified date when sorting by name and the row
// still has room for it.
func (m *Model) addSpareDate(c columns, avail int) columns {
	if c.date > 0 {
		return c
	}
	used := c.name + colGap + c.user
	if c.extra > 0 {
		used += colGap + c.extra
	}
	if avail-used >= colGap+len(dateLayout) {
		c.date = len(dateLayout)
	}
	return c
}

func (c columns) join(name, user, date, extra string) string {
	parts := []string{fit(name, c.name), fit(user, c.user)}
	if c.date > 0 {
		parts = append(parts, fit(date, c.date))
	}
	if c.extra > 0 {
		parts = append(parts, fit(extra, c.extra))
	}
	return strings.Join(parts, strings.Repeat(" ", colGap))
}

func (m *Model) listLines(rows []row, inner int) []string {
	height := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}

	avail := inner - 2 // "> " marker
	c := m.layoutColumns(rows, avail)

	dateTitle := "Modified"
	if m.sort == sortCreated {
		dateTitle = "Created"
	}
	extraTitle := "URI"
	if m.mode == modeSearch {
		extraTitle = "Folder"
	}
	lines := []string{"  " + styleLabel.Render(c.join("Name", "Username", dateTitle, extraTitle))}

	for i := m.offset; i < len(rows) && len(lines) <= height; i++ {
		r := rows[i]
		sel := i == m.cursor
		marker := "  "
		if sel {
			marker = "> "
		}

		if r.folder != nil {
			text := c.join(sanitize(r.folder.Name)+"/", folderCount(m.t, r.folder.ID), "", "")
			if sel {
				lines = append(lines, styleSelected.Render(fit(marker+text, inner)))
				continue
			}
			name := fit(sanitize(r.folder.Name)+"/", c.name)
			lines = append(lines, marker+styleFolder.Render(name)+strings.Repeat(" ", colGap)+styleMuted.Render(folderCount(m.t, r.folder.ID)))
			continue
		}

		e := r.entry
		date := ""
		if c.date > 0 {
			date = formatTime(m.entryTime(e))
		}
		if sel {
			lines = append(lines, styleSelected.Render(fit(marker+c.join(sanitize(e.Name), sanitize(e.Username), date, m.entryExtra(e)), inner)))
			continue
		}
		gap := strings.Repeat(" ", colGap)
		line := marker + styleText.Render(fit(sanitize(e.Name), c.name)) + gap + styleMuted.Render(fit(sanitize(e.Username), c.user))
		if c.date > 0 {
			line += gap + styleMuted.Render(fit(date, c.date))
		}
		if c.extra > 0 {
			line += gap + styleMuted.Render(fit(m.entryExtra(e), c.extra))
		}
		lines = append(lines, line)
	}
	return lines
}

func field(label, value string) string {
	return styleLabel.Render(fmt.Sprintf("%-10s", label)) + " " + value
}

// wrap breaks text into lines of at most w columns.
func wrap(text string, w int) []string {
	if w <= 0 {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(text), "\n")
}

// viewPreviewTop renders the preview as a strip above the list: fields in
// two columns, the description (or key hints) on the last row.
func (m *Model) viewPreviewTop(w int) string {
	inner := w - 2
	colW := max(inner/2, 20)
	two := func(l1, v1, l2, v2 string) string {
		return fit(" "+field(l1, v1), colW) + " " + field(l2, v2)
	}
	r, ok := m.selected()
	switch {
	case !ok:
		return box("Preview", "v layout", []string{"", styleMuted.Render(" Nothing selected")}, w, topPreviewHeight, false)
	case r.folder != nil:
		f := r.folder
		lines := []string{
			two("Path", m.t.path(f.ID), "Contains", fmt.Sprintf("%d folders, %d resources", len(m.t.children[f.ID]), len(m.t.entries[f.ID]))),
			"",
			"",
			" " + styleMuted.Render("enter open | h back | v layout"),
		}
		return box(sanitize(f.Name)+"/", "v layout", lines, w, topPreviewHeight, false)
	}
	e := r.entry
	access := "-"
	if e.MyAccess != 0 {
		access = accessName(e.MyAccess)
	}
	last := " " + styleMuted.Render("enter details | c copy password | u copy user | v layout")
	if e.Description != "" {
		last = " " + field("Notes", sanitize(e.Description))
	}
	lines := []string{
		two("Username", sanitize(e.Username), "Modified", formatTime(e.Modified)),
		two("URI", sanitize(e.URI), "Created", formatTime(e.Created)),
		two("Folder", m.t.path(e.FolderID), "Access", access),
		last,
	}
	return box(sanitize(e.Name), "v layout", lines, w, topPreviewHeight, false)
}

func (m *Model) viewPreview(w int) string {
	h := m.mainHeight()
	inner := w - 2
	r, ok := m.selected()
	var lines []string
	title := "Preview"
	switch {
	case !ok:
		lines = []string{"", styleMuted.Render(" Nothing selected")}
	case r.folder != nil:
		title = "Folder"
		folders, entries := len(m.t.children[r.folder.ID]), len(m.t.entries[r.folder.ID])
		lines = []string{
			"",
			" " + styleFolder.Render(sanitize(r.folder.Name)+"/"),
			"",
			" " + field("Path", m.t.path(r.folder.ID)),
			" " + field("Contains", fmt.Sprintf("%d folders, %d resources", folders, entries)),
			"",
			" " + styleMuted.Render("enter to open"),
		}
	default:
		e := r.entry
		lines = []string{
			"",
			" " + styleName.Render(sanitize(e.Name)),
			"",
			" " + field("Folder", m.t.path(e.FolderID)),
			" " + field("Username", sanitize(e.Username)),
			" " + field("URI", sanitize(e.URI)),
			" " + field("Modified", formatTime(e.Modified)),
			" " + field("Created", formatTime(e.Created)),
		}
		if e.MyAccess != 0 {
			lines = append(lines, " "+field("Your access", accessName(e.MyAccess)))
		}
		if e.Description != "" {
			lines = append(lines, "", " "+styleLabel.Render("Description"))
			for _, l := range wrap(sanitize(e.Description), inner-2) {
				lines = append(lines, " "+l)
			}
		}
		lines = append(lines, "", " "+styleMuted.Render("enter details | c copy password | u copy user"))
	}
	return box(title, "", lines, w, h, false)
}

func (m *Model) viewDetail(w int) string {
	h := m.mainHeight()
	inner := w - 2
	e := m.detail

	password := styleMuted.Render("********") + "  " + styleMuted.Render("(p to show)")
	if m.showPassword && m.secret != nil {
		password = styleSecret.Render(sanitize(m.secret.Password))
	}
	lines := []string{
		"",
		"  " + styleName.Render(sanitize(e.Name)),
		"",
		"  " + field("Folder", m.t.path(e.FolderID)),
		"  " + field("Username", sanitize(e.Username)),
		"  " + field("URI", sanitize(e.URI)),
		"  " + field("Password", password),
	}
	if m.secret != nil {
		lines = append(lines, "  "+field("TOTP", m.totpLine()))
	}
	lines = append(lines,
		"  "+field("Modified", formatTime(e.Modified)),
		"  "+field("Created", formatTime(e.Created)),
	)
	if e.Description != "" {
		lines = append(lines, "", "  "+styleLabel.Render("Description"))
		for _, l := range wrap(sanitize(e.Description), inner-4) {
			lines = append(lines, "  "+l)
		}
	}
	lines = append(lines, m.accessLines(inner-4)...)
	if room := h - 2; len(lines) > room && room > 1 {
		more := len(lines) - room + 1
		lines = append(lines[:room-1], "  "+styleMuted.Render(fmt.Sprintf("... %d more lines (enlarge the terminal)", more)))
	}
	return box("Resource", "esc back", lines, w, h, true)
}

// accessLines renders who has access to the resource in the detail view.
func (m *Model) accessLines(width int) []string {
	out := []string{"", "  " + styleLabel.Render("Access")}
	if m.detail.MyAccess != 0 {
		out = append(out, "  "+field("You", accessName(m.detail.MyAccess)))
	}
	switch {
	case m.sharesBusy:
		return append(out, "  "+styleMuted.Render("loading..."))
	case m.sharesErr != nil:
		return append(out, "  "+styleErr.Render(fit("could not load: "+sanitize(m.sharesErr.Error()), width)))
	case len(m.shares) <= 1:
		return append(out, "  "+field("Shared", "no (only you)"))
	}
	users, groups := 0, 0
	for _, sh := range m.shares {
		if sh.Group {
			groups++
		} else {
			users++
		}
	}
	out = append(out, "  "+field("Shared", fmt.Sprintf("with %d users, %d groups", users, groups)))
	for _, sh := range m.shares {
		kind := "user "
		if sh.Group {
			kind = "group"
		}
		who := sanitize(sh.Name)
		if sh.Detail != "" {
			who += " " + styleMuted.Render("<"+sanitize(sh.Detail)+">")
		}
		out = append(out, "    "+styleMuted.Render(fmt.Sprintf("%-10s %s", accessName(sh.Type), kind))+"  "+who)
	}
	return out
}

func (m *Model) totpLine() string {
	s := m.secret
	switch {
	case s.TOTPKey == "":
		return styleMuted.Render("none")
	case !s.TOTPSupported:
		return styleMuted.Render("unsupported parameters")
	case !m.showPassword:
		return styleMuted.Render("available (t to copy, p to show)")
	}
	code, err := m.totpCode(*s)
	if err != nil {
		return styleErr.Render("invalid TOTP secret")
	}
	left := 30 - m.now().Unix()%30
	if len(code) == 6 {
		code = code[:3] + " " + code[3:]
	}
	return styleSecret.Render(code) + "  " + styleMuted.Render(fmt.Sprintf("(%ds)", left))
}

func (m *Model) viewStatus(w int) string {
	var left string
	switch m.statusKind {
	case statusOK:
		left = styleOK.Render(m.status)
	case statusError:
		left = styleErr.Render(m.status)
	default:
		left = styleMuted.Render(m.status)
	}
	right := ""
	if m.clipArmed {
		left := max(int(m.clipExpires.Sub(m.now()).Round(time.Second)/time.Second), 0)
		right = styleWarn.Render(fmt.Sprintf("%s in clipboard, clears in %ds  ", strings.ToLower(m.clipLabel), left))
	}
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if gap < 1 {
		return fit(" "+left, w)
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

type hint struct{ key, desc string }

func (m *Model) hintList() []hint {
	switch m.mode {
	case modeSearch:
		return []hint{{"type", "filter"}, {"up/down", "move"}, {"enter", "open"}, {"tab", "sort"}, {"esc", "back"}}
	case modeDetail:
		if m.help {
			return []hint{{"p", "show/hide password"}, {"c", "copy password"}, {"u", "copy username"}, {"l", "copy URI"}, {"t", "copy TOTP"}, {"esc", "back"}, {"ctrl+c", "quit"}}
		}
		return []hint{{"p", "show"}, {"c", "password"}, {"u", "user"}, {"l", "uri"}, {"t", "totp"}, {"esc", "back"}, {"?", "help"}}
	default:
		if m.help {
			return []hint{{"j/k", "move"}, {"enter", "open"}, {"h", "parent"}, {"/", "search"}, {"s", "sort"}, {"v", "preview " + m.preview.String()}, {"c", "copy password"}, {"u", "copy username"}, {"r", "reload"}, {"q", "quit"}}
		}
		return []hint{{"enter", "open"}, {"h", "back"}, {"/", "search"}, {"s", "sort"}, {"v", "layout"}, {"c", "password"}, {"u", "user"}, {"q", "quit"}, {"?", "help"}}
	}
}

func (m *Model) viewHints(w int) string {
	var parts []string
	for _, h := range m.hintList() {
		parts = append(parts, styleKey.Render(h.key)+" "+styleMuted.Render(h.desc))
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), w, "")
}
