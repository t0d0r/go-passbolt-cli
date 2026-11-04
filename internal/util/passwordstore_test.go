package util

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
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
	pw, err := keyring.Get(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return pw, true
}

func TestNewUnlockedClient_PromptsOnceThenUsesKeyring(t *testing.T) {
	keyring.MockInit()
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
	keyring.MockInit()
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
	keyring.MockInit()
	key := testLockedKey(t, "new")
	account, err := keyringAccount(testServer, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(keyringService, account, "old"); err != nil {
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
	keyring.MockInit()
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
	keyring.MockInit()
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
	keyring.MockInit()
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
func TestGetClient_SendsCachedMFACookie(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Cleanup(viper.Reset)

	seen := make(chan string, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := ""
		if c, err := r.Cookie(MFACookieName); err == nil {
			v = c.Value
		}
		seen <- v
		http.Error(w, "test server", http.StatusInternalServerError)
	}))
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := GetClient(ctx); err == nil {
		t.Fatal("expected login error from the test server")
	}

	select {
	case v := <-seen:
		if v != "cached" {
			t.Errorf("first request carried MFA cookie %q, want cached", v)
		}
	default:
		t.Fatal("test server received no request")
	}
}
