# Upstream contribution

How changes from this fork go back to `passbolt/go-passbolt-cli`, and how
the branches are laid out to make that possible.

## Upstream rules (checked 2026-11-07)

- No CONTRIBUTING.md, code of conduct or PR template in the upstream repo.
  The org-wide "Contribute Code" link points at passbolt_api's
  CONTRIBUTING.md, which covers PHP only.
- License: MIT. Security issues go through Passbolt's responsible disclosure
  process (SECURITY.md), never a public issue or PR.
- Every PR must pass the CLA assistant check (cla-assistant.io, one-time
  signature; the "Passbolt Individual Contributor License Agreement" is at
  gist.github.com/fd4d894872fc1503fbbe8e30f3492116, Passbolt SA, Luxembourg
  law, non-exclusive copyright and patent license, contributor keeps
  ownership, outbound license limited to OSI-approved licenses).
- Code must pass golangci-lint (.golangci.yml), staticcheck and the gitleaks
  pre-commit hook. Since v0.5.3 forbidigo bans fmt.Print*/Fprint* and the
  raw os.Stdout/os.Stderr streams: output goes through util.Print*/Eprint*
  and util.Out/util.Err so server text is sanitized.
- PRs target `main`. There is no develop branch on GitHub.
- Since March 2026 the repo is driven by a Passbolt employee from an internal
  GitLab tracker (PB-xxxxx tickets); GitHub receives mirrored releases.
  Every external PR since then was closed, not merged: already fixed
  internally, tracked by an internal ticket, duplicate, or intentional
  behavior. Each got a polite reply and sometimes an internal ticket.

Decision: ask first. Open an issue or a community.passbolt.com thread per
feature before sending a PR, then send one small PR per feature. The fork
stays its own distribution either way (upstream lists community-maintained
packages as an accepted pattern).

## Branch layout

| Branch | Base | Content |
|---|---|---|
| `local/session-cache` | `upstream/main` | everything: all features plus fork scaffolding (AGENTS.md, Makefile, specs, todo, session). The branch for daily use. |
| `feat/mfa-remember` | `upstream/main` | one commit: MFA remember-me and cookie cache (identity.go, mfacache.go, client.go, root.go, README section) |
| `feat/password-keyring` | `feat/mfa-remember` | plus one commit: keyring password store, `passbolt lock`, passwordbackend*.go |
| `feat/wizard` | `feat/password-keyring` | plus one commit: `passbolt wizard` |
| `feat/tui` | `upstream/main` | one commit: `passbolt tui`, docs/tui.gif, docs/tui.tape |

Rules:

- A PR branch holds product files only: no specs/, todo/, session/,
  AGENTS.md, Makefile. Its README text is written for upstream (no fork
  note).
- A PR branch is one squashed commit per feature, taken from the final
  state of the files on `local/session-cache`, not a cherry-pick of the
  fork history. Shared files (client.go, root.go, go.mod) are the final
  state on the branch that is highest in the stack.
- The stack mfa-remember -> password-keyring -> wizard exists because the
  keyring code reuses the identity helpers and the lock command clears the
  MFA cookie, and the wizard sets both options. The PR for a stacked branch
  says which PR it builds on. `feat/tui` has no dependency on the others.
- Every branch passes `go vet ./...`, `golangci-lint run ./...` and
  `go test ./...` on its own (the PR branches have no Makefile).
- To refresh a PR branch after changes on `local/session-cache`: reset it to
  its base, `git checkout local/session-cache -- <its files>`, `go mod
  tidy`, pin `github.com/charmbracelet/x/ansi` to the version in
  `local/session-cache` (tidy otherwise picks a release incompatible with
  bubbletea v1.3.10), re-apply the README section, verify, commit.
- `backup/session-cache-pre-rebase` tags the branch as it was before the
  rebase onto v0.5.3 (fork history based on v0.5.2).

## Security Note

PR branches are public once pushed: they hold no config, keys or server
addresses. The CLA grants Passbolt a license to the contributed code; read
it before the first PR. Security-relevant findings in the fork's credential
handling go to Passbolt's disclosure process, not into a PR description.
