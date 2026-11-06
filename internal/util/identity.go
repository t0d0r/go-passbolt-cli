package util

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

// keyFingerprint returns the fingerprint of an armored (locked) key.
func keyFingerprint(userPrivateKey string) (string, error) {
	key, err := crypto.NewKeyFromArmored(userPrivateKey)
	if err != nil {
		return "", fmt.Errorf("reading user key: %w", err)
	}
	return key.GetFingerprint(), nil
}

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
