# Wizard: interactive configuration

## Goal

`passbolt wizard` walks through the configuration step by step. Values that
already exist (config file, flags, environment) are offered as defaults, so
a partial configuration is completed rather than replaced. Pressing Enter
keeps the shown default.

## Steps

1. **Server address** - default: current `serverAddress`. Must be an
   `https://` or `http://` URL with a host; `http://` needs an explicit
   confirmation. A trailing `/` is removed. The wizard then calls
   `GET <server>/healthcheck/status.json` with the configured TLS settings
   and reports the result; a failure asks whether to continue.
2. **TLS** - "Allow self-signed certificates?" default: current
   `tlsSkipVerify` (false). Asked before the reachability check.
3. **Private key** - if a key is configured, show its fingerprint and user
   ID and ask to keep it (default yes). Otherwise ask for the path of an
   armored private key file (`~` is expanded). The file must parse as a
   private key; fingerprint and user ID are shown. The key content is
   stored in `userPrivateKey`, like `configure --userPrivateKeyFile` does.
4. **Password storage** - `keyring` (default unless `none` is configured)
   or `none`. If the config holds a plain-text `userPassword`, offer to
   remove it (default yes). The wizard never asks for or writes a password
   to the config.
5. **MFA** - `interactive-totp` (default) or `none`. A configured
   `noninteractive-totp` is offered as "keep". The wizard never asks for a
   TOTP seed. For `interactive-totp`: "Remember MFA for 30 days?" default:
   current `mfaRemember` (true).
6. **Summary** - server, key fingerprint and user ID, password storage,
   MFA mode and remember, TLS skip-verify. No secrets are printed.
7. **Test login** (default yes) - runs a real login with the new settings
   (asks for the password and TOTP; with `keyring` the password is stored).
   On failure: "Save anyway?" (default no).
8. **Save** - "Save to <path>?" (default yes). Writes the config file with
   mode 0600 (new file in the default location, or the file in use /
   `--config`).
9. Hint: run `passbolt verify` to enable server verification if it is not
   set up.

## Non-goals

- No TOTP seed, no plain-text password, no mTLS setup in the wizard (mTLS
  values already in the config are kept unchanged).
- No backup copy of the old config: it may contain a plain-text password.

## Acceptance

- Empty config + answers -> config with all values set.
- Partial config (e.g. only serverAddress) -> Enter keeps it, the rest is
  asked.
- `userPassword` present + "remove" -> written config has an empty
  `userpassword`.
- Invalid server URL or key file -> the step is asked again (at most 3
  times, then the wizard stops without saving).
- Unit tests drive the wizard with scripted input; network and login steps
  are injected so tests do not need a server.

## Security Note

The wizard reads the private key file but never prints key material, the
password or tokens. The config file keeps mode 0600.
