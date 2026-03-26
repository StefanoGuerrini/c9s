# c9s demo — terminal dashboard for Claude Code
# Run: vhs demo.tape

Output demo.gif
Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1200
Set Height 650
Set Padding 16
Set TypingSpeed 40ms
Set Shell zsh

# Start demo
Type "./c9s --demo --inside-tmux"
Enter
Sleep 1.5s

# Navigate sessions
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Up
Sleep 300ms
Up
Sleep 300ms
Up
Sleep 300ms
Sleep 500ms

# Preview panel
Type "p"
Sleep 1.5s
Down
Sleep 500ms
Down
Sleep 500ms
Type "p"
Sleep 800ms

# Search
Type "/"
Sleep 300ms
Type "websocket"
Sleep 1s
Escape
Sleep 800ms

# Group by project
Tab
Sleep 1.5s

# Group by status
Tab
Sleep 1.5s

# Back to flat
Tab
Sleep 800ms

# Toggle tokens off/on
Type "t"
Sleep 1s
Type "t"
Sleep 800ms

# Worktree rows
Type "w"
Sleep 1.5s
Type "w"
Sleep 800ms

# Usage history
Type "u"
Sleep 2s

# Switch to weekly view
Type "w"
Sleep 1.5s

# Switch to monthly view
Type "m"
Sleep 1.5s

# Back to dashboard
Escape
Sleep 800ms

# Config screen
Type "c"
Sleep 1.5s
Down
Sleep 200ms
Down
Sleep 200ms
Down
Sleep 200ms
Escape
Sleep 800ms

# Quit
Type "q"
Sleep 300ms
