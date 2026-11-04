package util

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/passbolt/go-passbolt/api"
	"github.com/zalando/go-keyring"
)

// PasswordStoreKeyring keeps the private key passphrase in the OS keyring:
// macOS Keychain, Secret Service (D-Bus) on Linux, Credential Manager on
// Windows.
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
	return fp + "@" + serverAddress, nil
}

// ForgetPassword removes the stored passphrase for serverAddress and the user
// key. It returns false if nothing was stored.
func ForgetPassword(serverAddress, userPrivateKey string) (bool, error) {
	account, err := keyringAccount(serverAddress, userPrivateKey)
	if err != nil {
		return false, err
	}
	if err := keyring.Delete(keyringService, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// newUnlockedClient creates the API client, which unlocks the private key
// locally. The passphrase comes from, in order: configPassword, the keyring
// (if store is PasswordStoreKeyring), the prompt. A keyring passphrase that no
// longer unlocks the key is removed and the user is prompted once. A prompted
// passphrase is saved to the keyring only after it unlocked the key.
func newUnlockedClient(httpClient *http.Client, serverAddress, userPrivateKey, configPassword, store string, prompt func() (string, error)) (*api.Client, error) {
	if configPassword != "" {
		return api.NewClient(httpClient, "", serverAddress, userPrivateKey, configPassword)
	}

	var account string
	if store == PasswordStoreKeyring {
		var err error
		account, err = keyringAccount(serverAddress, userPrivateKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: keyring disabled: %v\n", err)
		}
	}

	if account != "" {
		stored, err := keyring.Get(keyringService, account)
		switch {
		case err == nil:
			client, cerr := api.NewClient(httpClient, "", serverAddress, userPrivateKey, stored)
			if cerr == nil {
				return client, nil
			}
			fmt.Fprintln(os.Stderr, "Stored password no longer unlocks the key, removing it from the keyring")
			if derr := keyring.Delete(keyringService, account); derr != nil {
				fmt.Fprintf(os.Stderr, "Warning: removing stored password: %v\n", derr)
			}
		case errors.Is(err, keyring.ErrNotFound):
		default:
			fmt.Fprintf(os.Stderr, "Warning: reading keyring: %v\n", err)
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
		if err := keyring.Set(keyringService, account, password); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: saving password to keyring: %v\n", err)
		}
	}
	return client, nil
}
