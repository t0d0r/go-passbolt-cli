package tui

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DemoStore serves fixed, made-up data so the UI can be shown or tried
// without a Passbolt server (passbolt tui --demo). Nothing leaves the
// process; favorite changes live only in memory.
type DemoStore struct {
	mu   sync.Mutex
	favs map[string]string
}

// NewDemoStore returns the demo data set.
func NewDemoStore() *DemoStore {
	return &DemoStore{favs: map[string]string{"r-git": "fav-r-git", "r-vpn": "fav-r-vpn"}}
}

var demoFolders = []Folder{
	{ID: "f-infra", Name: "infrastructure"},
	{ID: "f-dns", ParentID: "f-infra", Name: "dns"},
	{ID: "f-mon", ParentID: "f-infra", Name: "monitoring"},
	{ID: "f-cloud", Name: "cloud"},
	{ID: "f-office", Name: "office"},
}

func demoTime(daysAgo int) time.Time {
	return time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
}

var demoEntries = []Entry{
	{ID: "r-git", Name: "Git server", Username: "admin", URI: "https://git.example.com", Description: "Self-hosted Git, admin account", Modified: demoTime(2), Created: demoTime(400), MyAccess: accessOwner},
	{ID: "r-vpn", Name: "Office VPN", Username: "vpn-admin@example.com", URI: "https://vpn.example.com/admin", Modified: demoTime(1), Created: demoTime(220), MyAccess: accessOwner},
	{ID: "r-wifi", Name: "Guest Wi-Fi", Username: "guest", URI: "wifi://example-guest", Modified: demoTime(30), Created: demoTime(600), MyAccess: accessUpdate},
	{ID: "r-mail", Name: "Mail admin", Username: "postmaster@example.com", URI: "https://mail.example.com", Modified: demoTime(12), Created: demoTime(900), MyAccess: accessOwner},
	{ID: "r-ns1", FolderID: "f-dns", Name: "ns1.example.com", Username: "root", URI: "ssh://ns1.example.com", Modified: demoTime(5), Created: demoTime(700), MyAccess: accessOwner},
	{ID: "r-ns2", FolderID: "f-dns", Name: "ns2.example.com", Username: "root", URI: "ssh://ns2.example.com", Modified: demoTime(5), Created: demoTime(700), MyAccess: accessOwner},
	{ID: "r-registrar", FolderID: "f-dns", Name: "Domain registrar", Username: "billing@example.com", URI: "https://registrar.example.net/login", Modified: demoTime(90), Created: demoTime(1200), MyAccess: accessRead},
	{ID: "r-grafana", FolderID: "f-mon", Name: "Grafana", Username: "admin", URI: "https://grafana.example.com", Modified: demoTime(3), Created: demoTime(300), MyAccess: accessUpdate},
	{ID: "r-uptime", FolderID: "f-mon", Name: "Uptime monitor", Username: "ops@example.com", URI: "https://status.example.com", Modified: demoTime(8), Created: demoTime(150), MyAccess: accessOwner},
	{ID: "r-aws", FolderID: "f-cloud", Name: "AWS root account", Username: "aws-root@example.com", URI: "https://console.aws.amazon.com", Description: "Break-glass only, MFA on hardware key", Modified: demoTime(60), Created: demoTime(1500), MyAccess: accessOwner},
	{ID: "r-do", FolderID: "f-cloud", Name: "DigitalOcean", Username: "ops@example.com", URI: "https://cloud.digitalocean.com", Modified: demoTime(20), Created: demoTime(800), MyAccess: accessUpdate},
	{ID: "r-cf", FolderID: "f-cloud", Name: "Cloudflare", Username: "dns@example.com", URI: "https://dash.cloudflare.com", Modified: demoTime(15), Created: demoTime(500), MyAccess: accessUpdate},
	{ID: "r-printer", FolderID: "f-office", Name: "Printer admin", Username: "admin", URI: "http://192.0.2.15", Modified: demoTime(200), Created: demoTime(1000), MyAccess: accessRead},
	{ID: "r-alarm", FolderID: "f-office", Name: "Alarm system", Username: "installer", URI: "https://alarm.example.com", Modified: demoTime(45), Created: demoTime(1100), MyAccess: accessOwner},
}

// Load returns the demo folders and resources with the current favorites.
func (d *DemoStore) Load(context.Context) ([]Folder, []Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entries := append([]Entry(nil), demoEntries...)
	for i := range entries {
		entries[i].FavoriteID = d.favs[entries[i].ID]
	}
	return append([]Folder(nil), demoFolders...), entries, nil
}

// Secret returns a made-up password; the Git server also has a TOTP.
func (d *DemoStore) Secret(_ context.Context, id string) (Secret, error) {
	s := Secret{Password: "demo-" + id + "-Not-A-Real-Password"}
	if id == "r-git" {
		s.TOTPKey, s.TOTPSupported = "JBSWY3DPEHPK3PXP", true
	}
	return s, nil
}

// Permissions returns made-up colleagues and groups.
func (d *DemoStore) Permissions(_ context.Context, id string) ([]Share, error) {
	if id == "r-wifi" {
		return []Share{{Name: "You", Type: accessUpdate}}, nil
	}
	shares := []Share{
		{Name: "Alex Example", Detail: "alex@example.com", Type: accessOwner},
		{Group: true, Name: "Operations", Type: accessUpdate},
		{Name: "Sam Example", Detail: "sam@example.com", Type: accessRead},
	}
	sortShares(shares)
	return shares, nil
}

// SetFavorite toggles a favorite in memory.
func (d *DemoStore) SetFavorite(_ context.Context, id, _ string, add bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !add {
		delete(d.favs, id)
		return "", nil
	}
	d.favs[id] = fmt.Sprintf("fav-%s", id)
	return d.favs[id], nil
}
