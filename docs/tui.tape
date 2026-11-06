# Animated GIF of "passbolt tui" with made-up data
# (passbolt tui --demo: no server, no real secrets).
# Regenerate with: go build -o bin/passbolt . && vhs docs/tui.tape

Output docs/tui.gif

Set Shell "bash"
Set FontSize 15
Set Width 1300
Set Height 680
Set Padding 16
Set TypingSpeed 150ms
Set Theme "Catppuccin Mocha"

Hide
Type "./bin/passbolt tui --demo" Enter
Sleep 2s
Show
Sleep 2s

# root folder: Favorites/, folders, resources; the preview follows the cursor
Down@700ms 4
Sleep 3s

# search across all folders
Type "/"
Type "dns"
Sleep 3s
Escape
Sleep 1.5s

# detail view of "Git server" with the access list
Down@700ms 4
Sleep 1s
Enter
Sleep 4s
Escape
Sleep 1.5s
Type "q"
Sleep 1s
