# 2026-11-07 upstream-rebase

## Done

- `local/session-cache` rebased from v0.5.2 onto `upstream/main` (v0.5.3,
  64 upstream commits). Tag `backup/session-cache-pre-rebase` keeps the
  pre-rebase history. Conflicts were in client.go (MFA callback: upstream
  sanitizing Printf plus the fork's remember flag; password prompt moved
  into newUnlockedClient), go.mod and go.sum (upstream dependency bumps
  kept, fork dependencies added, `go mod tidy`).
- Lint fix: upstream's forbidigo rule bans fmt.Print*/Fprint* and raw
  os.Stdout/os.Stderr. lock, tui, wizard and the keyring store now print
  through util.Print*/Eprint*/util.Out; one exempted os.Stdout check stays
  in tui.go because bubbletea draws on the raw terminal.
- Refactor: normalizeServer and keyFingerprint moved from
  passwordbackend.go / passwordstore.go into internal/util/identity.go
  (tests in identity_test.go), so the MFA cache builds without the keyring
  code.
- `make check` passes on `local/session-cache` (0 lint issues, all tests
  ok).
- Four PR branches created from upstream main, product files only, one
  squashed commit per feature, each passing vet, golangci-lint and tests:
  `feat/mfa-remember` (1 commit), `feat/password-keyring` (stacked, 2),
  `feat/wizard` (stacked, 3), `feat/tui` (independent, 1). Layout, rules
  and the refresh procedure are in specs/upstream.md.
- Upstream contribution rules recorded in specs/upstream.md: CLA required,
  lint rules, PRs to `main`, external PRs closed since March 2026 in favour
  of internal tickets. Decision: ask upstream per feature before a PR.
- README: fork note now says based on v0.5.3.

## Open

- Nothing is pushed. `local/session-cache` on the fork remote still has the
  old history; pushing it needs --force-with-lease.
- The `feat/tui` go.mod pins github.com/charmbracelet/x/ansi v0.10.1;
  `go mod tidy` from upstream's go.mod otherwise picks v0.11.9, which does
  not compile with bubbletea v1.3.10's cellbuf.
- Upstream v0.5.3 release binaries are built with CGO disabled; on macOS the
  keyring feature then falls back to /usr/bin/security (documented in the
  keyring README section). Worth raising in the keyring PR.
- Earlier open items (real-server runs of tui and wizard, headless Linux
  keyring, tmux OSC 52, stale lines in vhs) are unchanged.

## Files touched

- internal/util/client.go, internal/util/identity.go,
  internal/util/identity_test.go, internal/util/passwordbackend.go,
  internal/util/passwordstore.go, internal/util/passwordstore_test.go
- internal/cmd/lock.go, internal/cmd/tui.go, internal/cmd/wizard.go
- go.mod, go.sum, README.md
- specs/upstream.md, specs/README.md, todo/todo.md, this note

## How to run / verify

```sh
make check          # on local/session-cache: lint and tests
# on a feat/* branch (no Makefile):
go vet ./... && golangci-lint run ./... && go test ./...
```

## Next step

Sign the CLA, open one upstream issue or forum thread per feature, then
push `feat/mfa-remember` and `feat/tui` first and open their PRs; push
`local/session-cache` with --force-with-lease.

## Security Note

No secrets in the repository or the branches. The PR branches carry the
fork's credential-handling code (keyring, MFA cookie cache); upstream
review of it is welcome. The CLA licenses the contributed code to Passbolt.
