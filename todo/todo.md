# Todo

- [x] fork scaffolding: AGENTS.md, Makefile, specs, todo, session (specs/overview.md)
- [x] MFA remember-me + cookie cache (specs/auth-cache.md, part 1) - unit tests pass, not yet tried against a real server
- [ ] confirm remember-me on the Passbolt server in use: policy "Remember a month" enabled, cookie survives logout (specs/auth-cache.md)
- [x] `passbolt lock`: clear the MFA cookie cache and the passphrase cache (specs/auth-cache.md)
- [x] passphrase in the OS keyring, `passwordStore = keyring` (specs/auth-cache.md, part 2)
- [x] fix: install the MFA cookie transport before api.NewClient copies the http.Client, regression test added
- [ ] try on macOS against the real server: Keychain item created, no prompts on the second run
- [ ] headless Linux hosts: no Secret Service; decide agent or kernel keyring (specs/auth-cache.md)
- [ ] `make build-linux`, copy to the Linux host, compare sha256, install to /usr/local/bin (with host audit trail)
