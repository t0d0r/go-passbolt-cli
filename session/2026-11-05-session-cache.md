# 2026-11-05 session-cache

## Done

- Cloned upstream `passbolt/go-passbolt-cli`, checked out signed tag
  `v0.5.2` (commit `5ad2e0f1b19c`), branch `local/session-cache`.
- Added fork scaffolding: `AGENTS.md`, `Makefile`, `specs/`, `todo/`,
  `session/`, `.gitignore` additions (`Makefile` un-ignored, `bin/`,
  `.env*`, `*.pem`, `*.key`).
- MFA remember-me (specs/auth-cache.md, part 1):
  - `internal/util/mfacache.go`: `MFACookieCache` (0600 file under the user
    cache dir, keyed by server + key fingerprint), `mfaChallengeResponse`
    with `remember`, transport wrapper that injects the cached cookie.
  - `internal/util/client.go`: interactive-totp callback sends `remember`,
    saves the persistent cookie, loads it on startup.
  - `internal/cmd/root.go`: flag/config key `mfaRemember` (default true).
  - `internal/util/mfacache_test.go`: unit tests.

## Decisions

- Library `go-passbolt` stays unmodified; the cached MFA cookie is injected
  through an `http.RoundTripper` because the library keeps it private.
- Plain-text `userPassword` and `noninteractive-totp` with a stored TOTP
  seed are rejected as solutions (specs/overview.md).

## Open

- `make check` passes (gofmt, go vet, golangci-lint 0 issues, all unit
  tests). Two gosec findings were false positives: request cookie attributes
  (set anyway) and the 0700 cache directory (annotated `nolint:gosec`).
- Not yet tried against a real Passbolt server.
- Passphrase cache design (keyring vs agent) not decided.
- Server-side behaviour (policy enabled, cookie survives logout) to be
  confirmed against the real server.

## Files touched

- AGENTS.md, Makefile, .gitignore
- specs/README.md, specs/overview.md, specs/auth-cache.md
- todo/todo.md, session/2026-11-05-session-cache.md
- internal/util/mfacache.go, internal/util/mfacache_test.go
- internal/util/client.go, internal/cmd/root.go

## How to run / verify

```sh
make check          # gofmt, go vet, golangci-lint, unit tests
make build          # bin/passbolt for this machine
make build-linux    # bin/passbolt-linux-amd64 + sha256
```

## Next step

Decide the passphrase cache design (specs/auth-cache.md, part 2), then try
the build against the real server (todo item 3).

## Security Note

No secrets in this repository. The MFA cookie cache lives outside the tree
in the user cache directory with mode 0600.
