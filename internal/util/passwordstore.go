package util

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/passbolt/go-passbolt/api"
)

// PasswordStoreKeyring keeps the private key passphrase in the OS keyring:
// macOS Keychain, Secret Service (D-Bus) on Linux, Credential Manager on
// Windows. It is opt-in (default "none").
const PasswordStoreKeyring = "keyring"

// keyringService is the service name of the keyring item.
const keyringService = "go-passbolt-cli"

// keyFingerprint returns the fingerprint of an armored (locked) key.
func keyFingerprint(userPrivateKey string) (string, error) {
	key, err := crypto.NewKeyFromArmored(userPrivateKey)
	if err != nil {
		return "", fmt.Errorf("reading user key: %w", err)
	}
	return key.GetFingerprint(), nil
}

// keyringAccount names the keyring item for one server and one user key, so
// different servers or keys never share a passphrase. It holds no secret.
func keyringAccount(serverAddress, userPrivateKey string) (string, error) {
	fp, err := keyFingerprint(userPrivateKey)
	if err != nil {
		return "", err
	}
	return fp + "@" + normalizeServer(serverAddress), nil
}

// ForgetPassword removes the stored passphrase for serverAddress and the user
// key. It returns false if nothing was stored.
func ForgetPassword(serverAddress, userPrivateKey string) (bool, error) {
	account, err := keyringAccount(serverAddress, userPrivateKey)
	if err != nil {
		return false, err
	}
	if err := keyringBackend.Delete(keyringService, account); err != nil {
		if errors.Is(err, errPasswordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ForgetAll removes every stored passphrase of this CLI and every cached MFA
// cookie, for all servers and keys.
func ForgetAll() error {
	var errs []error
	if err := keyringBackend.DeleteAll(keyringService); err != nil {
		errs = append(errs, fmt.Errorf("keyring: %w", err))
	}
	if dir, err := os.UserCacheDir(); err == nil {
		if err := os.RemoveAll(filepath.Join(dir, "go-passbolt-cli", "mfa")); err != nil {
			errs = append(errs, fmt.Errorf("MFA cache: %w", err))
		}
	}
	return errors.Join(errs...)
}

// isUnlockError reports whether err from api.NewClient means the passphrase
// did not unlock the key (as opposed to, for example, a bad server address).
func isUnlockError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unlock")
}

// newUnlockedClient creates the API client, which unlocks the private key
// locally. The passphrase comes from, in order: configPassword, the keyring
// (if store is PasswordStoreKeyring), the prompt. A keyring passphrase that no
// longer unlocks the key is removed and the user is prompted once. A prompted
// passphrase is saved to the keyring only after it unlocked the key.
func newUnlockedClient(httpClient *http.Client, serverAddress, userPrivateKey, configPassword, store string, prompt func() (string, error)) (*api.Client, error) {
	if _, err := url.Parse(serverAddress); err != nil {
		return nil, fmt.Errorf("parsing serverAddress: %w", err)
	}
	if configPassword != "" {
		return api.NewClient(httpClient, "", serverAddress, userPrivateKey, configPassword)
	}

	var account string
	if store == PasswordStoreKeyring {
		var err error
		account, err = keyringAccount(serverAddress, userPrivateKey)
		if err != nil {
			Eprintf("Warning: keyring disabled: %v\n", err)
		}
	}

	if account != "" {
		stored, err := keyringBackend.Get(keyringService, account)
		switch {
		case err == nil:
			client, cerr := api.NewClient(httpClient, "", serverAddress, userPrivateKey, stored)
			if cerr == nil {
				return client, nil
			}
			if !isUnlockError(cerr) {
				return nil, cerr
			}
			Eprintln("Stored password no longer unlocks the key, removing it from the keyring")
			if derr := keyringBackend.Delete(keyringService, account); derr != nil && !errors.Is(derr, errPasswordNotFound) {
				Eprintf("Warning: removing stored password: %v\n", derr)
			}
		case errors.Is(err, errPasswordNotFound):
		default:
			Eprintf("Warning: reading %s: %v\n", keyringBackend.Name(), err)
			account = ""
		}
	}

	password, err := prompt()
	if err != nil {
		return nil, fmt.Errorf("reading Password: %w", err)
	}
	client, err := api.NewClient(httpClient, "", serverAddress, userPrivateKey, password)
	if err != nil {
		return nil, err
	}
	if account != "" {
		if err := keyringBackend.Set(keyringService, account, password); err != nil {
			Eprintf("Warning: saving password to %s: %v\n", keyringBackend.Name(), err)
		}
	}
	return client, nil
}
