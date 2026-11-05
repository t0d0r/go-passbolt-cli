package util

import (
	"errors"
	"net/url"
	"strings"
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

// normalizeServer makes equivalent server addresses equal, so keyring items
// and cache entries do not depend on spelling: lower-case scheme and host,
// default ports and trailing slashes removed.
func normalizeServer(addr string) string {
	u, err := url.Parse(strings.TrimSpace(addr))
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.TrimSpace(addr), "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	defaultPort := (scheme == "https" && u.Port() == "443") || (scheme == "http" && u.Port() == "80")
	if port := u.Port(); port != "" && !defaultPort {
		host += ":" + port
	}
	return scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/")
}
