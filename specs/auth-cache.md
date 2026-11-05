# Auth cache: MFA remember-me and passphrase cache

## Problem

Every command (`passbolt list resource`, `get`, ...) logs in from scratch:

1. it asks for the private key passphrase (`Enter Password:`), needed both
   for the GPGAuth login and for decrypting secrets locally;
2. after login the first API call returns an MFA challenge and the CLI asks
   for a TOTP code (`Enter TOTP:`).

## Part 1: MFA remember-me (implemented)

### Server behaviour (Passbolt API, MultiFactorAuthentication plugin)

- `POST /mfa/verify/totp.json` accepts `{"totp": "...", "remember": true}`.
- When the organisation policy "Remember a month" is enabled, the server
  returns a `passbolt_mfa` cookie with an expiry 30 days ahead
  (`MfaVerifiedCookie::MAX_DURATION_IN_DAYS = 30`). A remembered token is
  not bound to the session id, so it is valid for later logins.
- When the policy is disabled, `remember` is ignored and the cookie has no
  expiry (session cookie, bound to the session).
- Logout does not invalidate remembered tokens. Changing MFA settings or the
  policy does.

Source checked: passbolt_api `master`, files
`MfaVerifyController.php`, `MfaVerifiedToken.php`, `MfaVerifiedCookie.php`.
Still to confirm against the server version in use.

### CLI behaviour

- New flag / config key `mfaRemember` (bool, default `true`), used only in
  `mfaMode = interactive-totp`.
- On a successful TOTP verification the CLI sends `remember: true`. If the
  returned cookie is persistent (Expires or Max-Age), it is stored in
  `$UserCacheDir/go-passbolt-cli/mfa/<id>.json`:
  - `<id>` = first 16 bytes of sha256(serverAddress NUL keyFingerprint),
    so each server and user key has its own entry;
  - directory mode 0700, file mode 0600, written via temp file + rename;
  - content: cookie value and expiry only.
- If the cookie is session-only, any old entry is removed and the CLI
  prints a note that the server did not grant remember-me.
- On startup a valid (not expired) cached cookie is added to every request
  to the server host that has no `passbolt_mfa` cookie yet (transport
  wrapper; the library keeps its MFA cookie in a private field).
- If the server rejects the cached cookie, the normal MFA prompt runs and
  the new cookie replaces the old entry.
- Expired or corrupt entries are deleted on load.

### Acceptance

- First command: passphrase + TOTP prompt. Next commands within 30 days:
  passphrase prompt only.
- `--mfaRemember=false`: upstream behaviour (TOTP every time, nothing
  cached, `remember` not sent).
- Unit tests: save/load, Max-Age, expiry removal, session cookie not saved,
  corrupt file, file/dir permissions, transport adds/keeps cookie.

## Part 2: passphrase in the OS keyring (implemented)

### Decision

Use the OS keyring through `github.com/zalando/go-keyring` v0.2.8 (no cgo):
macOS Keychain via `/usr/bin/security`, Secret Service over D-Bus on Linux,
Credential Manager on Windows. Chosen over an own agent because there is no
daemon to run. On macOS the password is written through `security -i` on
stdin, so it never appears in a process argument list.

Rejected: plain `userPassword` in the config file (on disk next to the
private key). An own agent (ssh-agent style) stays an option for headless
Linux hosts, where no Secret Service is running.

### CLI behaviour

- New flag / config key `passwordStore`: `none` (default, upstream
  behaviour) or `keyring`.
- Order: `userPassword` from config/flag wins and is never copied to the
  keyring; else the keyring item; else the prompt.
- Item: service `go-passbolt-cli`, account `<key fingerprint>@<serverAddress>`.
- `api.NewClient` unlocks the private key locally, so the password is checked
  without network access:
  - a prompted password is saved only after it unlocked the key;
  - a stored password that no longer unlocks the key (passphrase changed) is
    removed and the user is prompted once.
- If the keyring is unavailable (e.g. no D-Bus session), the CLI prints a
  warning and falls back to the prompt.
- `passbolt lock` removes the keyring item and the MFA cookie cache entry.

### Limits

- No TTL: the Keychain item stays until `passbolt lock` or manual removal.
  It is protected by the login keychain, which is unlocked while the user
  is logged in.
- Items created by `/usr/bin/security` are readable by that tool without a
  prompt, so any process running as the same user can read the password
  while the login keychain is unlocked.

### Acceptance

- `passwordStore = 'keyring'`: first command prompts, later commands do not.
- Changed passphrase: one prompt, the item is replaced.
- Unit tests (mock keyring): prompt once then keyring, wrong password not
  stored, stale item replaced, `none` does not touch the keyring, config
  password wins, `ForgetPassword`.
- Regression test: `GetClient` sends the cached MFA cookie on the first
  request (the transport must be installed before `api.NewClient` copies the
  http.Client).

## Security Note

- The remembered MFA cookie is a bearer token for the second factor only;
  an attacker also needs the private key and passphrase. It is stored 0600
  and can be revoked server-side by resetting MFA for the user.
- Never combine the MFA cookie cache with a stored passphrase in the same
  plain-text file; that would reduce the login to "read one file".
- Every process running as the same user can read the MFA cache file and,
  while the login keychain is unlocked, the keyring item. Run `passbolt lock`
  when leaving the machine for long.

## Security review changes (2026-11-05)

- Defaults: `mfaRemember=false`, `passwordStore=none` (opt-in); the wizard
  explains the risk before asking. Remember-me only for https servers.
- macOS: native Keychain via `github.com/keybase/go-keychain` (cgo): the item
  trusts only the creating binary, `AccessibleWhenUnlockedThisDeviceOnly`, no
  iCloud sync. Verified on macOS with a dummy item: the decrypt ACL lists
  only the creating binary, not `/usr/bin/security`. Without cgo the
  go-keyring fallback warns. Backend interface `passwordBackend`
  (`passwordbackend*.go`); tests use an in-memory backend.
- Keyring account and MFA cache id use the normalized server address
  (scheme/host lower case, default port and trailing slash removed).
- A stored password is deleted only when it fails to unlock the key, not on
  other client errors.
- `passbolt lock --all` removes every item of the service and the MFA cache
  directory; help text says server-side tokens stay valid until MFA reset.
