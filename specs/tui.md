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
with key caps.

Preview of the selected row (never secrets), toggled with `v`:

- `top` (default): a strip of 6 lines between the header and the list,
  fields in two columns (username, URI, folder | modified, created, your
  access) and the description or key hints on the last row. The list below
  it uses the full width. Hidden automatically below 16 terminal lines.
- `right`: a pane next to the list, a third of the width, 40 to 60 columns;
  only from 100 columns.
- `off`: list only.

When sorting by name the list shows the modified date as a column if the
row has room for it after name, username and URI. Colours
adapt to light and dark backgrounds. `?` toggles the full key list. Text is
truncated ANSI- and wide-character-aware so no line exceeds the terminal
width. All displayed text from the server
is stripped of control characters (no terminal escape injection through
resource names).

### Folder view (start screen, root folder)

Rows: sub-folders (shown as `name/` with the item count) first, then
resources of the folder. A header row names the columns: name, username,
the date when sorting by date, and URI (folder path in search). Column
widths follow the content: if everything fits, each column is as wide as
its longest value; otherwise username and URI get up to a quarter of the
row each and the name the rest. The URI column is dropped when it would be
cut below 12 columns.

| Key | Action |
|---|---|
| up/down, k/j, pgup/pgdown, g/G | move |
| enter, l, right | open folder / open resource detail |
| backspace (DEL or ^H), h, left | parent folder |
| / | search |
| v | cycle the preview: top, right, off |
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

### Access (who a resource is shared with)

- The list load asks for the current user's permission
  (`contain[permission]`); the preview shows it as "Your access".
- Opening the detail view loads `GET /permissions/resource/<id>` (not a
  secret, so no key press is needed) and shows: your access, "Shared: no
  (only you)" when there is a single permission, otherwise the number of
  users and groups and one line per permission - level (owner, can update,
  can read), user or group, full name and email. Owners first, then users
  before groups, then by name. Users and groups are looked up once per load
  (`GetUsers`, `GetGroups`); unknown ids are shown as "unknown user <id>".
- A result for a resource that is no longer open is ignored.

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

## Terminal safety

- `sanitize` removes Unicode control (Cc) and format (Cf) characters -
  escape sequences, zero-width characters, joiners, soft hyphen, BOM, bidi
  controls - and turns tabs, newlines, NEL and line/paragraph separators into
  spaces.
- The frame leaves the last terminal column empty and is clipped to the
  terminal height, so a character the terminal draws wider than computed
  cannot wrap a line, scroll the screen and duplicate borders.
- The MFA challenge of the first API call is triggered before the UI starts
  (`GetMe`), so a TOTP prompt never appears inside the UI. Inside the UI the
  MFA callback returns an error ("restart passbolt tui") instead of
  prompting.

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
