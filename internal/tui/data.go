// Package tui implements "passbolt tui", a terminal UI to browse folders and
// resources and copy credentials.
package tui

import (
	"context"
	"sort"
	"strings"
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

// Store loads data from Passbolt. Implementations must be safe to call from
// a goroutine other than the UI loop.
type Store interface {
	Load(ctx context.Context) ([]Folder, []Entry, error)
	Secret(ctx context.Context, resourceID string) (Secret, error)
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

// sanitize removes control characters so server-provided text cannot inject
// terminal escape sequences; tabs and newlines become spaces.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return -1
		case r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			return -1 // bidi controls
		}
		return r
	}, s)
}
