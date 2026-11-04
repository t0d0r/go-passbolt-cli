# TUI: interactive navigation

## Goal

`passbolt tui` opens a full-screen terminal UI to browse folders and
resources, search, and copy a username, URI, password or TOTP code without
typing IDs. It uses the same login as every other command (keyring password,
remembered MFA cookie).

## Library

`github.com/charmbracelet/bubbletea` v1.3.10 and
`github.com/charmbracelet/lipgloss` v1.1.0 (stable v1 line). bubbletea v2
was released one week before this decision and is not used yet.

## Data

- On start: one login, then `GetFolders` and `GetResources` without secrets.
  v5 metadata (name, username, URI, description) is decrypted with
  `helper.GetResourceFieldMaps(..., decryptSecret=false)`, v4 fields are
  read as is. Unsupported resource types are skipped and counted.
- Secrets are fetched only on demand (show or copy) for one resource, kept
  in memory only while its detail view is open, never written to disk.
- If a request fails (for example an expired session), the client logs in
  again once and retries; the unlocked key is still in memory, so no prompt.
- `r` reloads folders and resources.

## Screens and keys

Header: server address and the current folder path. Footer: status message
and key hints. `?` toggles a help line. All displayed text from the server
is stripped of control characters (no terminal escape injection through
resource names).

### Folder view (start screen, root folder)

Rows: sub-folders (shown as `name/`) first, then resources of the folder,
each sorted by name. Columns: name, username, URI.

| Key | Action |
|---|---|
| up/down, k/j, pgup/pgdown, g/G | move |
| enter, l, right | open folder / open resource detail |
| backspace, h, left | parent folder |
| / | search |
| c / u | copy password / username of the selected resource |
| r | reload |
| q, ctrl+c | quit |

### Search

Typing filters all resources (every folder) by case-insensitive substring;
each space-separated term must match name, username, URI or folder path.
Rows show the folder path. up/down move, enter opens the detail, esc leaves
search, backspace edits the query.

### Detail

Shows name, folder path, username, URI, description and the password masked
as `********`.

| Key | Action |
|---|---|
| p | show / hide the password |
| c | copy password |
| u | copy username |
| l | copy URI |
| t | copy the current TOTP code (SHA1, 6 digits, 30 s only) |
| esc, backspace, h, left, q | back |

## Clipboard

- Backends, first available: macOS `pbcopy`/`pbpaste`; Wayland
  `wl-copy`/`wl-paste`; X11 `xclip`, then `xsel`; otherwise OSC 52 escape
  sequence to the terminal (works over SSH in terminals that allow it).
- Text is passed on stdin, never as a command argument.
- Copied passwords and TOTP codes are cleared after 30 seconds, and on quit,
  but only if the clipboard still holds the copied value (compared via
  SHA-256; with OSC 52 the clipboard cannot be read and is cleared
  unconditionally). Usernames and URIs are not cleared.

## Non-goals (for now)

Creating, editing, sharing or deleting resources; opening URIs in a browser.

## Acceptance

- Model tests with a fake store and fake clipboard: folder navigation,
  parent, search filtering and path display, detail show/hide, copy
  password/username, clipboard clear only when unchanged, stale clear timer
  ignored, control characters stripped, TOTP code copied.
- Store tests: folder path building with a parent cycle guard.

## Security Note

Passwords are masked by default and fetched per resource. The clipboard is
visible to other applications and clipboard managers while it holds a
password; the 30 s clear limits but does not remove that exposure.
