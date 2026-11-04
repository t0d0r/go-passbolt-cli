package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

// MFACookieName is the cookie Passbolt sets after a successful MFA verification.
const MFACookieName = "passbolt_mfa"

// mfaChallengeResponse is the body for POST /mfa/verify/totp.json. The
// upstream api.MFAChallengeResponse has no "remember" field.
type mfaChallengeResponse struct {
	TOTP     string `json:"totp"`
	Remember bool   `json:"remember,omitempty"`
}

type cachedMFACookie struct {
	Value   string    `json:"value"`
	Expires time.Time `json:"expires"`
}

// MFACookieCache stores the long-lived ("remember this device") MFA cookie for
// one server and one user key in a 0600 file under the user cache directory.
type MFACookieCache struct {
	path string
}

// NewMFACookieCache returns the cache for serverAddress and the user key. The
// file name is derived from the server address and the key fingerprint, so
// different servers or users never share a cookie.
func NewMFACookieCache(serverAddress, userPrivateKey string) (*MFACookieCache, error) {
	key, err := crypto.NewKeyFromArmored(userPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("reading user key: %w", err)
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("finding cache directory: %w", err)
	}
	sum := sha256.Sum256([]byte(serverAddress + "\x00" + key.GetFingerprint()))
	name := hex.EncodeToString(sum[:16]) + ".json"
	return &MFACookieCache{path: filepath.Join(dir, "go-passbolt-cli", "mfa", name)}, nil
}

// Load returns the cached cookie if it exists and has not expired. An expired
// or unreadable entry is removed.
func (m *MFACookieCache) Load(now time.Time) (http.Cookie, bool) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return http.Cookie{}, false
	}
	var c cachedMFACookie
	if err := json.Unmarshal(data, &c); err != nil || c.Value == "" || !now.Before(c.Expires) {
		_ = m.Clear()
		return http.Cookie{}, false
	}
	// Only Name and Value are sent on requests; the attributes satisfy linters.
	return http.Cookie{Name: MFACookieName, Value: c.Value, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}, true
}

// Save stores cookie if the server made it persistent (Expires or Max-Age
// set). A session-only cookie means the server did not grant remember-me; the
// old entry is then removed and false is returned.
func (m *MFACookieCache) Save(cookie http.Cookie, now time.Time) (bool, error) {
	expires := cookie.Expires
	if cookie.MaxAge > 0 {
		expires = now.Add(time.Duration(cookie.MaxAge) * time.Second)
	}
	if cookie.Value == "" || cookie.MaxAge < 0 || !now.Before(expires) {
		return false, m.Clear()
	}

	data, err := json.Marshal(cachedMFACookie{Value: cookie.Value, Expires: expires.UTC()})
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a directory needs the execute bit; 0700 is owner-only
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".mfa-*")
	if err != nil {
		return false, err
	}
	// After a successful rename the temp name no longer exists; ignore that error.
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), m.path)
}

// Clear removes the cached cookie. A missing file is not an error.
func (m *MFACookieCache) Clear() error {
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// mfaCookieTransport adds a cached MFA cookie to requests for the Passbolt
// host that do not carry one yet. go-passbolt keeps its MFA cookie in a
// private field, so the cached cookie is injected at the transport level.
type mfaCookieTransport struct {
	base   http.RoundTripper
	host   string
	cookie http.Cookie
}

func (t *mfaCookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.host {
		if _, err := req.Cookie(MFACookieName); err != nil {
			req = req.Clone(req.Context())
			req.AddCookie(&t.cookie)
		}
	}
	return t.base.RoundTrip(req)
}

// withMFACookie wraps the client transport so requests to serverAddress carry
// cookie.
func withMFACookie(httpClient *http.Client, serverAddress string, cookie http.Cookie) error {
	u, err := url.Parse(serverAddress)
	if err != nil {
		return fmt.Errorf("parsing serverAddress: %w", err)
	}
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient.Transport = &mfaCookieTransport{base: base, host: u.Host, cookie: cookie}
	return nil
}
