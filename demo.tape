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

# Start demo — clean view, no tokens, no worktrees
Type "./c9s --demo --inside-tmux"
Enter
Sleep 2s

# Navigate sessions
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
Sleep 500ms

# Open a session (Enter)
Enter
Sleep 2.5s

# Return to dashboard (any key)
Escape
Sleep 1s

# Search for a session
Type "/"
Sleep 300ms
Type "docker"
Sleep 1s
Escape
Sleep 800ms

# Preview panel
Type "p"
Sleep 1.5s
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 500ms
Type "p"
Sleep 800ms

# Toggle tokens on
Type "t"
Sleep 1.5s

# Group by project
Tab
Sleep 1.5s

# Group by status
Tab
Sleep 1.5s

# Back to flat
Tab
Sleep 800ms

# Toggle tokens off
Type "t"
Sleep 500ms

# Toggle worktree rows on
Type "w"
Sleep 1.5s
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms

# Toggle worktrees off
Type "w"
Sleep 800ms

# Usage history — daily
Type "u"
Sleep 2s

# Weekly
Type "w"
Sleep 1.5s

# Monthly
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
Down
Sleep 200ms
Escape
Sleep 800ms

# Rename a session
Type "R"
Sleep 500ms
Type "My important task"
Sleep 800ms
Escape
Sleep 1s

# Quit
Type "q"
Sleep 300ms
