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

- Passphrase in the OS keyring (specs/auth-cache.md, part 2):
  `internal/util/passwordstore.go` (`newUnlockedClient`, `ForgetPassword`),
  flag/config key `passwordStore` (`none`|`keyring`), new command
  `passbolt lock` (`internal/cmd/lock.go`), dependency
  `github.com/zalando/go-keyring` v0.2.8.
- Bug fixed: the MFA cookie transport was installed after `api.NewClient`,
  which copies the http.Client, so the cached cookie was never sent. The
  regression test `TestGetClient_SendsCachedMFACookie` fails on the old code
  and passes on the new.

- `passbolt wizard` (specs/wizard.md, `internal/cmd/wizard.go`): server
  address + reachability check, TLS, private key (keep or load from file,
  validated, fingerprint shown), password storage, MFA mode/remember,
  summary, optional login test, save with mode 0600. Existing settings are
  defaults. Network, login and save are injected; 9 unit tests. The binary
  was smoke-tested against the real server up to the key step (aborts
  without saving on bad input).
- `passbolt list folder` works on macOS with `passwordStore = keyring`.

- `passbolt tui` (specs/tui.md): package `internal/tui` (data/tree,
  PassboltStore with re-login retry, clipboard backends pbcopy / wl-copy /
  xclip / xsel / OSC 52, bubbletea model with browse, search and detail
  modes), command `internal/cmd/tui.go`. Dependencies bubbletea v1.3.10,
  lipgloss v1.1.0. Secrets fetched per resource on demand; copied
  passwords/TOTP cleared after 30 s and on quit only if unchanged; server
  text stripped of control and bidi characters. 15 tests, `-race` clean.
  Not yet run against the real server.
- tui: Backspace did nothing in terminals that send ^H (bubbletea reports
  it as ctrl+h, only DEL as backspace). ctrl+h is now handled like
  backspace in every mode; backspace on an empty search query leaves search;
  ctrl+w / alt+backspace delete a word.
- tui redesign: `internal/tui/view.go` with rounded-border boxes, header
  bar, two panes from 100 columns (list + preview), status line with
  clipboard countdown, key-cap hints, adaptive colours. Sort `s`/`tab`:
  name, last modified, last created; created/modified shown in preview,
  detail and as a list column. Clipboard clear now driven by a one-second
  clock (also drives the live TOTP code in the detail view).
- tui spacing: on wide terminals the name column took 75% of the row and
  pushed usernames far right. Columns are now sized from the content (see
  `distribute` in view.go), with a column header row, item counts on
  folder rows, a 40-80 column preview pane and two columns of right margin
  in the header and status line.
- tui access: your permission in the preview; the detail view loads the
  resource permissions and names users/groups (owners first). Store method
  `Permissions`, users/groups cached per load.
- tui frame fix: borders were sometimes duplicated (frame shifted by one
  line). Defences: stricter `sanitize` (all Cc/Cf, separators to spaces),
  last column kept free and the frame clipped to the terminal height
  (`TestFrameNeverOverflows`), and the MFA challenge triggered before the UI
  starts so no TOTP prompt can write into it. The exact character in the
  real data that caused the wrap is not known.
- tui layout: the right preview left too much empty space and cut the list
  columns. Default is now a 6-line preview strip on top with the list below
  at full width (`v` cycles top / right / off); modified date shown as a
  column when there is room.

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
- Keyring has no TTL on macOS; `passbolt lock` is the manual lock.
- Headless Linux hosts have no Secret Service; the keyring falls back to the
  prompt there.
- Server-side behaviour (policy enabled, cookie survives logout) to be
  confirmed against the real server.

## Files touched

- AGENTS.md, Makefile, .gitignore
- specs/README.md, specs/overview.md, specs/auth-cache.md
- todo/todo.md, session/2026-11-05-session-cache.md
- internal/util/mfacache.go, internal/util/mfacache_test.go
- internal/util/client.go, internal/cmd/root.go
- internal/util/passwordstore.go, internal/util/passwordstore_test.go
- internal/cmd/lock.go, go.mod, go.sum
- internal/cmd/wizard.go, internal/cmd/wizard_test.go, specs/wizard.md
- internal/tui/*.go, internal/cmd/tui.go, specs/tui.md

## How to run / verify

```sh
make check          # gofmt, go vet, golangci-lint, unit tests
make build          # bin/passbolt for this machine
make build-linux    # bin/passbolt-linux-amd64 + sha256
```

## Next step

Run `passbolt tui` against the real server; run `passbolt wizard` end to
end on a fresh config; then decide the password store for headless Linux
hosts (specs/auth-cache.md).

## Security Note

No secrets in this repository. The MFA cookie cache lives outside the tree
in the user cache directory with mode 0600.
