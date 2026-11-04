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

## Part 2: passphrase cache (open, design not decided)

The passphrase is needed on every run because the CLI decrypts the private
key locally. Options:

| Option | How | Pros | Cons |
|---|---|---|---|
| A. Kernel keyring / OS keychain | Linux: `keyctl` user keyring entry with a timeout; macOS: Keychain item | No daemon, kernel memory is not swapped, TTL enforced by kernel | Two platform backends; any process of the same uid can read it during the TTL |
| B. Agent | `passbolt agent` holds the passphrase in memory with a TTL, unix socket 0600 in `$XDG_RUNTIME_DIR` (like ssh-agent) | One code path for all platforms, can lock on demand | Daemon to manage; process memory may be swapped unless `mlock`ed |
| C. Plain config `userPassword` | Already supported upstream | No code | Passphrase on disk next to the private key; rejected |

Recommendation: A, with a default TTL of 8 hours and a `passbolt lock`
command that removes the entry.

## Security Note

- The remembered MFA cookie is a bearer token for the second factor only;
  an attacker also needs the private key and passphrase. It is stored 0600
  and can be revoked server-side by resetting MFA for the user.
- Never combine the MFA cookie cache with a stored passphrase in the same
  plain-text file; that would reduce the login to "read one file".
- On shared hosts, every process running as the same user can read both the
  cache file and (option A/B) the cached passphrase during its TTL.
