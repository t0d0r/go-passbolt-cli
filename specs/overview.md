# Overview

## Goal

A local fork of the Passbolt CLI (`passbolt/go-passbolt-cli`) that can be
used interactively many times a day without typing the private key
passphrase and the TOTP code on every command, while keeping MFA and the
key passphrase meaningful (no secrets stored in plain text).

## Base

- Upstream: https://github.com/passbolt/go-passbolt-cli
- Base tag: `v0.5.2` (commit `5ad2e0f1b19c`, signed tag, GitHub "verified")
- Library: `github.com/passbolt/go-passbolt` v0.8.3 (unchanged, not forked)
- Branch: `local/session-cache`

## Rules for changes

- Change only the CLI; keep the library unmodified so upstream updates are
  a rebase of a small patch set.
- New behaviour is behind flags/config keys that also exist on the command
  line, following the existing viper pattern in `internal/cmd/root.go`.
- Never store the TOTP seed or the key passphrase in plain text. The
  upstream options `userPassword` and `mfaMode = noninteractive-totp` with
  `mfaTotpToken` do exactly that and are not the solution here.
- Builds happen on a workstation, never on a production host. Linux
  binaries are produced with `make build-linux`.

## Targets

- Workstation: macOS (arm64).
- Linux hosts: amd64, headless (no desktop keyring), shared by many
  sessions of the same user.

## Security Note

The CLI holds the user private key, which decrypts every secret the user
can read. Any caching must be per user, time-limited, and must not write
the passphrase or decrypted key to disk or swap where avoidable.
