package util

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCache(t *testing.T) *MFACookieCache {
	t.Helper()
	return &MFACookieCache{path: filepath.Join(t.TempDir(), "mfa", "test.json")}
}

func TestMFACookieCache_SaveLoad(t *testing.T) {
	m := testCache(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	saved, err := m.Save(http.Cookie{Name: MFACookieName, Value: "tok", Expires: now.Add(30 * 24 * time.Hour)}, now)
	if err != nil || !saved {
		t.Fatalf("Save = %v, %v; want true, nil", saved, err)
	}

	info, err := os.Stat(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(m.path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}

	c, ok := m.Load(now.Add(time.Hour))
	if !ok || c.Value != "tok" || c.Name != MFACookieName {
		t.Errorf("Load = %+v, %v; want tok, true", c, ok)
	}
}

func TestMFACookieCache_MaxAge(t *testing.T) {
	m := testCache(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if saved, err := m.Save(http.Cookie{Name: MFACookieName, Value: "tok", MaxAge: 3600}, now); err != nil || !saved {
		t.Fatalf("Save = %v, %v", saved, err)
	}
	if _, ok := m.Load(now.Add(59 * time.Minute)); !ok {
		t.Error("cookie should still be valid")
	}
	if _, ok := m.Load(now.Add(61 * time.Minute)); ok {
		t.Error("cookie should have expired")
	}
}

func TestMFACookieCache_ExpiredIsRemoved(t *testing.T) {
	m := testCache(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if _, err := m.Save(http.Cookie{Name: MFACookieName, Value: "tok", Expires: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Load(now.Add(2 * time.Hour)); ok {
		t.Fatal("expired cookie returned")
	}
	if _, err := os.Stat(m.path); !os.IsNotExist(err) {
		t.Errorf("expired cache file still exists: %v", err)
	}
}

func TestMFACookieCache_SessionCookieNotSaved(t *testing.T) {
	m := testCache(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if _, err := m.Save(http.Cookie{Name: MFACookieName, Value: "old", Expires: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	saved, err := m.Save(http.Cookie{Name: MFACookieName, Value: "session-only"}, now)
	if err != nil || saved {
		t.Fatalf("Save = %v, %v; want false, nil", saved, err)
	}
	if _, ok := m.Load(now); ok {
		t.Error("old cookie should have been cleared")
	}
}

func TestMFACookieCache_LoadMissingAndCorrupt(t *testing.T) {
	m := testCache(t)
	if _, ok := m.Load(time.Now()); ok {
		t.Error("Load on missing file returned ok")
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Load(time.Now()); ok {
		t.Error("Load on corrupt file returned ok")
	}
}

func TestMFACookieTransport(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(MFACookieName)
		if err != nil {
			got = append(got, "")
			return
		}
		got = append(got, c.Value)
	}))
	defer srv.Close()

	hc := &http.Client{}
	if err := withMFACookie(hc, srv.URL, http.Cookie{Name: MFACookieName, Value: "cached"}); err != nil {
		t.Fatal(err)
	}

	// No MFA cookie on the request: the cached one is added.
	req, _ := http.NewRequest("GET", srv.URL+"/a", nil)
	if _, err := hc.Do(req); err != nil {
		t.Fatal(err)
	}

	// The request already has a (fresh) MFA cookie: it is kept as is.
	req, _ = http.NewRequest("GET", srv.URL+"/b", nil)
	req.AddCookie(&http.Cookie{Name: MFACookieName, Value: "fresh"})
	if _, err := hc.Do(req); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0] != "cached" || got[1] != "fresh" {
		t.Errorf("server saw %q, want [cached fresh]", got)
	}
}
