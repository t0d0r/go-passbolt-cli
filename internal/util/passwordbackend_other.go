//go:build !darwin || !cgo

package util

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/zalando/go-keyring"
)

// goKeyring uses Secret Service (D-Bus) on Linux and the Credential Manager
// on Windows. On macOS without cgo it falls back to /usr/bin/security,
// whose items any process of the user can read; builds for macOS should use
// cgo (passwordbackend_darwin.go).
type goKeyring struct{}

func newPlatformBackend() passwordBackend { return goKeyring{} }

func (goKeyring) Name() string { return "OS keyring" }

func (goKeyring) Get(service, account string) (string, error) {
	s, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errPasswordNotFound
	}
	return s, err
}

func (goKeyring) Set(service, account, secret string) error {
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(os.Stderr, "Warning: built without cgo, the Keychain item is readable by any process of this user; rebuild with cgo (make build)")
	}
	return keyring.Set(service, account, secret)
}

func (goKeyring) Delete(service, account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return errPasswordNotFound
	}
	return err
}

func (goKeyring) DeleteAll(service string) error {
	err := keyring.DeleteAll(service)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
