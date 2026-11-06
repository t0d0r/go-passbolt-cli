package util

import (
	"errors"
)

// errPasswordNotFound is returned by a passwordBackend when no item exists.
var errPasswordNotFound = errors.New("password not found")

// passwordBackend stores one secret per service and account in the OS
// keyring. The implementation is chosen per platform (passwordbackend_*.go);
// tests replace it with an in-memory one.
type passwordBackend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
	DeleteAll(service string) error
	Name() string
}

var keyringBackend = newPlatformBackend()
