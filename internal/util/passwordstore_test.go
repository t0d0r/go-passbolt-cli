package util

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/spf13/viper"
)

const testServer = "https://passbolt.example.com"

// testLockedKey returns an armored private key locked with passphrase.
func testLockedKey(t *testing.T, passphrase string) string {
	t.Helper()
	pgp := crypto.PGP()
	key, err := pgp.KeyGeneration().AddUserId("test", "test@example.com").New().GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	locked, err := pgp.LockKey(key, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatal(err)
	}
	return armored
}

// countingPrompt returns a prompt that answers with answers in order and
// counts the calls.
func countingPrompt(calls *int, answers ...string) func() (string, error) {
	return func() (string, error) {
		*calls++
		if *calls > len(answers) {
			return "", errors.New("unexpected prompt")
		}
		return answers[*calls-1], nil
	}
}

func stored(t *testing.T, key string) (string, bool) {
	t.Helper()
	account, err := keyringAccount(testServer, key)
	if err != nil {
		t.Fatal(err)
	}
	pw, err := keyringBackend.Get(keyringService, account)
	if errors.Is(err, errPasswordNotFound) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return pw, true
}

func TestNewUnlockedClient_PromptsOnceThenUsesKeyring(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "right")
	calls := 0

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", PasswordStoreKeyring, countingPrompt(&calls, "right")); err != nil {
		t.Fatal(err)
	}
	if pw, ok := stored(t, key); !ok || pw != "right" {
		t.Fatalf("keyring = %q, %v; want right, true", pw, ok)
	}

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", PasswordStoreKeyring, countingPrompt(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("prompt called %d times, want 1", calls)
	}
}

func TestNewUnlockedClient_WrongPromptNotStored(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "right")
	calls := 0

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", PasswordStoreKeyring, countingPrompt(&calls, "wrong")); err == nil {
		t.Fatal("expected error for wrong password")
	}
	if _, ok := stored(t, key); ok {
		t.Error("wrong password was stored")
	}
}

func TestNewUnlockedClient_StaleKeyringReplaced(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "new")
	account, err := keyringAccount(testServer, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyringBackend.Set(keyringService, account, "old"); err != nil {
		t.Fatal(err)
	}
	calls := 0

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", PasswordStoreKeyring, countingPrompt(&calls, "new")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("prompt called %d times, want 1", calls)
	}
	if pw, ok := stored(t, key); !ok || pw != "new" {
		t.Errorf("keyring = %q, %v; want new, true", pw, ok)
	}
}

func TestNewUnlockedClient_StoreNoneDoesNotTouchKeyring(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "right")
	calls := 0

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", "none", countingPrompt(&calls, "right")); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored(t, key); ok {
		t.Error("password stored with passwordStore=none")
	}
}

func TestNewUnlockedClient_ConfigPasswordWins(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "right")
	calls := 0

	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "right", PasswordStoreKeyring, countingPrompt(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("prompt called %d times, want 0", calls)
	}
	if _, ok := stored(t, key); ok {
		t.Error("config password was copied to the keyring")
	}
}

func TestForgetPassword(t *testing.T) {
	useMemBackend(t)
	key := testLockedKey(t, "right")
	calls := 0
	if _, err := newUnlockedClient(&http.Client{}, testServer, key, "", PasswordStoreKeyring, countingPrompt(&calls, "right")); err != nil {
		t.Fatal(err)
	}

	removed, err := ForgetPassword(testServer, key)
	if err != nil || !removed {
		t.Fatalf("ForgetPassword = %v, %v; want true, nil", removed, err)
	}
	removed, err = ForgetPassword(testServer, key)
	if err != nil || removed {
		t.Errorf("second ForgetPassword = %v, %v; want false, nil", removed, err)
	}
}

// TestGetClient_SendsCachedMFACookie guards the order in GetClient: the MFA
// cookie transport must be installed before api.NewClient copies the
// http.Client, otherwise the cached cookie is never sent.
// firstMFACookie runs GetClient against a test server and returns the MFA
// cookie value of the first request ("" if none).
func firstMFACookie(t *testing.T, tls bool) string {
	t.Helper()
	useMemBackend(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Cleanup(viper.Reset)

	seen := make(chan string, 10)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := ""
		if c, err := r.Cookie(MFACookieName); err == nil {
			v = c.Value
		}
		seen <- v
		http.Error(w, "test server", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(h)
	if tls {
		srv.Close()
		srv = httptest.NewTLSServer(h)
	}
	defer srv.Close()

	key := testLockedKey(t, "pw")
	cache, err := NewMFACookieCache(srv.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := cache.Save(http.Cookie{Name: MFACookieName, Value: "cached", Expires: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}

	viper.Set("serverAddress", srv.URL)
	viper.Set("userPrivateKey", key)
	viper.Set("userPassword", "pw")
	viper.Set("mfaMode", "interactive-totp")
	viper.Set("mfaRemember", true)
	viper.Set("tlsSkipVerify", true) // httptest certificate

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := GetClient(ctx); err == nil {
		t.Fatal("expected login error from the test server")
	}
	select {
	case v := <-seen:
		return v
	default:
		t.Fatal("test server received no request")
		return ""
	}
}

// TestGetClient_SendsCachedMFACookie guards the order in GetClient: the MFA
// cookie transport must be installed before api.NewClient copies the
// http.Client, otherwise the cached cookie is never sent.
func TestGetClient_SendsCachedMFACookie(t *testing.T) {
	if v := firstMFACookie(t, true); v != "cached" {
		t.Errorf("first request carried MFA cookie %q, want cached", v)
	}
}

// TestGetClient_NoMFACookieOverHTTP: the remembered cookie is a bearer token
// for the second factor and is never sent in clear text.
func TestGetClient_NoMFACookieOverHTTP(t *testing.T) {
	if v := firstMFACookie(t, false); v != "" {
		t.Errorf("MFA cookie %q sent over http", v)
	}
}

// memBackend is an in-memory passwordBackend for tests.
type memBackend map[string]string

func (m memBackend) Name() string { return "test keyring" }

func (m memBackend) Get(service, account string) (string, error) {
	v, ok := m[service+"\x00"+account]
	if !ok {
		return "", errPasswordNotFound
	}
	return v, nil
}

func (m memBackend) Set(service, account, secret string) error {
	m[service+"\x00"+account] = secret
	return nil
}

func (m memBackend) Delete(service, account string) error {
	k := service + "\x00" + account
	if _, ok := m[k]; !ok {
		return errPasswordNotFound
	}
	delete(m, k)
	return nil
}

func (m memBackend) DeleteAll(service string) error {
	for k := range m {
		if strings.HasPrefix(k, service+"\x00") {
			delete(m, k)
		}
	}
	return nil
}

// useMemBackend replaces the OS keyring for the duration of a test.
func useMemBackend(t *testing.T) memBackend {
	t.Helper()
	old := keyringBackend
	m := memBackend{}
	keyringBackend = m
	t.Cleanup(func() { keyringBackend = old })
	return m
}

func TestNormalizeServer(t *testing.T) {
	same := []string{"https://pb.example.com", "https://pb.example.com/", "HTTPS://PB.example.com", "https://pb.example.com:443/", " https://pb.example.com "}
	for _, s := range same {
		if got := normalizeServer(s); got != "https://pb.example.com" {
			t.Errorf("normalizeServer(%q) = %q", s, got)
		}
	}
	if got := normalizeServer("https://pb.example.com:8443/sub/"); got != "https://pb.example.com:8443/sub" {
		t.Errorf("port/path: %q", got)
	}
}

func TestKeyringAccountIgnoresSpelling(t *testing.T) {
	key := testLockedKey(t, "pw")
	a, _ := keyringAccount("https://pb.example.com/", key)
	b, _ := keyringAccount("https://PB.example.com", key)
	if a != b {
		t.Errorf("accounts differ: %q vs %q", a, b)
	}
}

func TestStoredPasswordKeptOnNonUnlockError(t *testing.T) {
	m := useMemBackend(t)
	key := testLockedKey(t, "right")
	account, _ := keyringAccount(testServer, key)
	m[keyringService+"\x00"+account] = "right"
	calls := 0
	// a broken key (not an unlock failure) must not delete the stored password
	if _, err := newUnlockedClient(&http.Client{}, testServer, "not a key", "", PasswordStoreKeyring, countingPrompt(&calls, "right")); err == nil {
		t.Fatal("expected error for a broken key")
	}
	if _, ok := m[keyringService+"\x00"+account]; !ok {
		t.Error("stored password removed on a non-unlock error")
	}
}

func TestForgetAll(t *testing.T) {
	m := useMemBackend(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	m[keyringService+"\x00a"] = "1"
	m[keyringService+"\x00b"] = "2"
	m["other\x00c"] = "3"
	key := testLockedKey(t, "pw")
	cache, err := NewMFACookieCache(testServer, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Save(http.Cookie{Name: MFACookieName, Value: "v", Expires: time.Now().Add(time.Hour)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ForgetAll(); err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["other\x00c"] != "3" {
		t.Errorf("keyring after ForgetAll = %v", m)
	}
	if _, ok := cache.Load(time.Now()); ok {
		t.Error("MFA cookie survived ForgetAll")
	}
}
