// Package tui implements "passbolt tui", a terminal UI to browse folders and
// resources and copy credentials.
package tui

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Folder is a Passbolt folder; ParentID is "" for the root.
type Folder struct {
	ID       string
	ParentID string
	Name     string
}

// Entry is a resource without its secret.
type Entry struct {
	ID          string
	FolderID    string
	Name        string
	Username    string
	URI         string
	Description string
	Created     time.Time
	Modified    time.Time
	// FavoriteID is the id of the user's favorite mark, "" if not a favorite.
	FavoriteID string
	// MyAccess is the current user's permission type (see accessName), 0 if
	// unknown.
	MyAccess int
}

// Secret is the decrypted secret part of one resource.
type Secret struct {
	Password string
	// TOTPKey is the base32 TOTP secret, "" if the resource has none.
	TOTPKey string
	// TOTPSupported is false if the TOTP uses parameters other than
	// SHA1 / 6 digits / 30 seconds.
	TOTPSupported bool
}

// Passbolt permission types.
const (
	accessRead   = 1
	accessUpdate = 7
	accessOwner  = 15
)

func accessName(t int) string {
	switch t {
	case accessOwner:
		return "owner"
	case accessUpdate:
		return "can update"
	case accessRead:
		return "can read"
	}
	return "unknown"
}

// Share is one permission on a resource: a user or a group and its type.
type Share struct {
	Group  bool
	Name   string // full name or group name
	Detail string // email for users
	Type   int
}

// Store loads data from Passbolt. Implementations must be safe to call from
// a goroutine other than the UI loop.
type Store interface {
	Load(ctx context.Context) ([]Folder, []Entry, error)
	Secret(ctx context.Context, resourceID string) (Secret, error)
	// Permissions lists who has access to a resource, owners first.
	Permissions(ctx context.Context, resourceID string) ([]Share, error)
	// SetFavorite marks (add) or unmarks a resource; favoriteID is the
	// current mark when removing. It returns the new mark id ("" if removed).
	SetFavorite(ctx context.Context, resourceID, favoriteID string, add bool) (string, error)
}

// sortShares orders by permission (owner first), users before groups, then
// by name.
func sortShares(shares []Share) {
	sort.SliceStable(shares, func(i, j int) bool {
		a, b := shares[i], shares[j]
		if a.Type != b.Type {
			return a.Type > b.Type
		}
		if a.Group != b.Group {
			return !a.Group
		}
		return lessFold(a.Name, b.Name)
	})
}

// tree indexes folders and entries for navigation.
type tree struct {
	folders  map[string]Folder
	children map[string][]Folder // parent id -> sub-folders, sorted
	entries  map[string][]Entry  // folder id -> entries, sorted
	all      []Entry             // every entry, sorted by path then name
	paths    map[string]string   // folder id -> "/a/b"
}

func newTree(folders []Folder, entries []Entry) *tree {
	t := &tree{
		folders:  make(map[string]Folder, len(folders)),
		children: make(map[string][]Folder),
		entries:  make(map[string][]Entry),
		paths:    make(map[string]string, len(folders)),
	}
	for _, f := range folders {
		t.folders[f.ID] = f
	}
	for _, f := range folders {
		parent := f.ParentID
		if _, ok := t.folders[parent]; !ok {
			parent = "" // unknown parent (not shared with us): show at root
		}
		t.children[parent] = append(t.children[parent], f)
	}
	for _, e := range entries {
		folder := e.FolderID
		if _, ok := t.folders[folder]; !ok {
			folder = ""
		}
		e.FolderID = folder
		t.entries[folder] = append(t.entries[folder], e)
		t.all = append(t.all, e)
	}
	for k := range t.children {
		sort.Slice(t.children[k], func(i, j int) bool { return lessFold(t.children[k][i].Name, t.children[k][j].Name) })
	}
	for k := range t.entries {
		sort.Slice(t.entries[k], func(i, j int) bool { return lessFold(t.entries[k][i].Name, t.entries[k][j].Name) })
	}
	sort.SliceStable(t.all, func(i, j int) bool {
		pi, pj := t.path(t.all[i].FolderID), t.path(t.all[j].FolderID)
		if pi != pj {
			return lessFold(pi, pj)
		}
		return lessFold(t.all[i].Name, t.all[j].Name)
	})
	return t
}

func lessFold(a, b string) bool { return strings.ToLower(a) < strings.ToLower(b) }

// path returns "/a/b" for a folder id and "/" for the root. A parent cycle
// or a chain deeper than 64 is cut off.
func (t *tree) path(id string) string {
	if id == "" {
		return "/"
	}
	if p, ok := t.paths[id]; ok {
		return p
	}
	var parts []string
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur] && len(parts) < 64; {
		f, ok := t.folders[cur]
		if !ok {
			break
		}
		seen[cur] = true
		parts = append(parts, sanitize(f.Name))
		cur = f.ParentID
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	p := "/" + strings.Join(parts, "/")
	t.paths[id] = p
	return p
}

// favorites returns the favorite entries of every folder, by name.
func (t *tree) favorites() []Entry {
	var out []Entry
	for _, e := range t.all {
		if e.FavoriteID != "" {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return lessFold(out[i].Name, out[j].Name) })
	return out
}

// setFavorite updates the favorite mark of one entry everywhere.
func (t *tree) setFavorite(id, favoriteID string) {
	for k := range t.entries {
		for i := range t.entries[k] {
			if t.entries[k][i].ID == id {
				t.entries[k][i].FavoriteID = favoriteID
			}
		}
	}
	for i := range t.all {
		if t.all[i].ID == id {
			t.all[i].FavoriteID = favoriteID
		}
	}
}

func (t *tree) parent(id string) string {
	f, ok := t.folders[id]
	if !ok {
		return ""
	}
	if _, ok := t.folders[f.ParentID]; !ok {
		return ""
	}
	return f.ParentID
}

// search returns entries where every term matches name, username, URI or
// folder path (case-insensitive).
func (t *tree) search(query string) []Entry {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return t.all
	}
	var out []Entry
	for _, e := range t.all {
		hay := strings.ToLower(strings.Join([]string{e.Name, e.Username, e.URI, t.path(e.FolderID)}, "\x00"))
		match := true
		for _, term := range terms {
			if !strings.Contains(hay, term) {
				match = false
				break
			}
		}
		if match {
			out = append(out, e)
		}
	}
	return out
}

// sanitize makes server-provided text safe and predictable to draw: it
// removes control and format characters (escape sequences, zero-width
// characters, joiners, soft hyphens, BOM, bidi controls) and turns tabs,
// newlines and Unicode line/paragraph separators into spaces. A stray
// character that the terminal draws with another width than computed would
// otherwise wrap a line and shift the whole screen.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			return ' '
		case unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

type sortMode int

const (
	sortName sortMode = iota
	sortModified
	sortCreated
)

func (s sortMode) String() string {
	switch s {
	case sortModified:
		return "last modified"
	case sortCreated:
		return "last created"
	default:
		return "name"
	}
}

func (s sortMode) next() sortMode { return (s + 1) % 3 }

// sortEntries returns a sorted copy; time modes put the newest first and
// fall back to the name for equal times. sortName keeps the given order.
func sortEntries(in []Entry, mode sortMode) []Entry {
	out := append([]Entry(nil), in...)
	if mode == sortName {
		return out
	}
	key := func(e Entry) time.Time {
		if mode == sortCreated {
			return e.Created
		}
		return e.Modified
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := key(out[i]), key(out[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return lessFold(out[i].Name, out[j].Name)
	})
	return out
}
