//go:build darwin && cgo

package util

import (
	"errors"

	"github.com/keybase/go-keychain"
)

// nativeKeychain uses the Security framework directly. Items it creates
// trust only the creating application (this binary), unlike items created
// through /usr/bin/security, which any process can read with
// "security find-generic-password -w" without a prompt. Items are kept on
// this device only (no iCloud sync) and are readable only while unlocked.
type nativeKeychain struct{}

func newPlatformBackend() passwordBackend { return nativeKeychain{} }

func (nativeKeychain) Name() string { return "macOS Keychain" }

func (nativeKeychain) Get(service, account string) (string, error) {
	data, err := keychain.GetGenericPassword(service, account, "", "")
	if err != nil {
		if errors.Is(err, keychain.ErrorItemNotFound) {
			return "", errPasswordNotFound
		}
		return "", err
	}
	if data == nil {
		return "", errPasswordNotFound
	}
	return string(data), nil
}

func (k nativeKeychain) Set(service, account, secret string) error {
	item := keychain.NewGenericPassword(service, account, service, []byte(secret), "")
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlockedThisDeviceOnly)
	err := keychain.AddItem(item)
	if errors.Is(err, keychain.ErrorDuplicateItem) {
		// Replace instead of update, so the item gets the access list of
		// the current binary.
		if err := k.Delete(service, account); err != nil && !errors.Is(err, errPasswordNotFound) {
			return err
		}
		err = keychain.AddItem(item)
	}
	return err
}

func (nativeKeychain) Delete(service, account string) error {
	err := keychain.DeleteGenericPasswordItem(service, account)
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return errPasswordNotFound
	}
	return err
}

func (k nativeKeychain) DeleteAll(service string) error {
	accounts, err := keychain.GetGenericPasswordAccounts(service)
	if err != nil && !errors.Is(err, keychain.ErrorItemNotFound) {
		return err
	}
	for _, a := range accounts {
		if err := k.Delete(service, a); err != nil && !errors.Is(err, errPasswordNotFound) {
			return err
		}
	}
	return nil
}
