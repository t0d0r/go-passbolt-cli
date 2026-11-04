# Todo

- [x] fork scaffolding: AGENTS.md, Makefile, specs, todo, session (specs/overview.md)
- [x] MFA remember-me + cookie cache (specs/auth-cache.md, part 1) - unit tests pass, not yet tried against a real server
- [ ] confirm remember-me on the Passbolt server in use: policy "Remember a month" enabled, cookie survives logout (specs/auth-cache.md)
- [ ] `passbolt lock`: clear the MFA cookie cache and the passphrase cache (specs/auth-cache.md)
- [ ] choose and implement the passphrase cache (specs/auth-cache.md, part 2)
- [ ] `make build-linux`, copy to the Linux host, compare sha256, install to /usr/local/bin (with host audit trail)
