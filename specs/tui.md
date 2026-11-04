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

Layout: a header bar (badge, server, folder/resource counts), the main
area in rounded-border boxes (title in the top border, sort mode and
position in the bottom border), a status line (coloured by kind; while a
secret is in the clipboard it shows "clears in Ns") and a key-hint line
with key caps. From 100 columns the browse and search screens show two
panes: the list (3/5) and a preview of the selected row (folder contents,
or resource fields, created/modified, description; never secrets). Colours
adapt to light and dark backgrounds. `?` toggles the full key list. Text is
truncated ANSI- and wide-character-aware so no line exceeds the terminal
width. All displayed text from the server
is stripped of control characters (no terminal escape injection through
resource names).

### Folder view (start screen, root folder)

Rows: sub-folders (shown as `name/`) first, then resources of the folder,
each sorted by name. Columns: name, username, URI.

| Key | Action |
|---|---|
| up/down, k/j, pgup/pgdown, g/G | move |
| enter, l, right | open folder / open resource detail |
| backspace (DEL or ^H), h, left | parent folder |
| / | search |
| s, tab | cycle sort: name, last modified, last created (newest first; folders stay first, by name) |
| c / u | copy password / username of the selected resource |
| r | reload |
| q, ctrl+c | quit |

### Search

Typing filters all resources (every folder) by case-insensitive substring;
each space-separated term must match name, username, URI or folder path.
Rows show the folder path. up/down move, enter opens the detail, esc leaves
search. tab cycles the sort (search with an empty query and sort by last
modified lists the most recently changed resources across all folders).
Backspace (DEL or ^H) deletes a character and leaves search when the
query is empty; ctrl+w and alt+backspace delete a word.

### Detail

Shows name, folder path, username, URI, password masked as `********`,
TOTP state, modified and created time, description. With the password
shown (`p`) a supported TOTP shows the live code and the seconds left.

| Key | Action |
|---|---|
| p | show / hide the password |
| c | copy password |
| u | copy username |
| l | copy URI |
| t | copy the current TOTP code (SHA1, 6 digits, 30 s only) |
| esc, backspace (DEL or ^H), h, left, q | back |

## Clipboard

- Backends, first available: macOS `pbcopy`/`pbpaste`; Wayland
  `wl-copy`/`wl-paste`; X11 `xclip`, then `xsel`; otherwise OSC 52 escape
  sequence to the terminal (works over SSH in terminals that allow it).
- Text is passed on stdin, never as a command argument.
- Copied passwords and TOTP codes are cleared after 30 seconds (a one-second
  clock drives the countdown and the clear; copying again restarts the
  30 s), and on quit,
  but only if the clipboard still holds the copied value (compared via
  SHA-256; with OSC 52 the clipboard cannot be read and is cleared
  unconditionally). Usernames and URIs are not cleared.

## Non-goals (for now)

Creating, editing, sharing or deleting resources; opening URIs in a browser.

## Acceptance

- Model tests with a fake store and fake clipboard: folder navigation,
  parent, search filtering and path display, detail show/hide, copy
  password/username, clipboard clear only when unchanged, copy again
  restarts the timer, countdown text, control characters stripped, TOTP code
  copied and shown live, sort modes, two-pane and narrow layouts never wider
  than the terminal, Backspace as DEL and ^H.
- Store tests: folder path building with a parent cycle guard.

## Security Note

Passwords are masked by default and fetched per resource. The clipboard is
visible to other applications and clipboard managers while it holds a
password; the 30 s clear limits but does not remove that exposure.
