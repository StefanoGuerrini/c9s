# c9s demo — terminal dashboard for Claude Code
# Run: vhs demo.tape

Output demo.gif
Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1200
Set Height 650
Set Padding 16
Set TypingSpeed 50ms
Set Shell zsh

# Start demo — clean view
Type "./c9s --demo --inside-tmux"
Enter
Sleep 2.5s

# Navigate sessions slowly
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms
Up
Sleep 600ms
Up
Sleep 600ms
Sleep 1s

# Open a session
Enter
Sleep 4s

# Return to dashboard
Escape
Sleep 1.5s

# Search
Type "/"
Sleep 500ms
Type "docker"
Sleep 1.5s
Escape
Sleep 1.2s

# Preview panel
Type "p"
Sleep 2s
Down
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 800ms
Type "p"
Sleep 1.2s

# Tokens on
Type "t"
Sleep 2s

# Group by project
Tab
Sleep 2s

# Group by status
Tab
Sleep 2s

# Back to flat
Tab
Sleep 1.2s

# Tokens off
Type "t"
Sleep 800ms

# Worktrees
Type "w"
Sleep 2s
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 500ms
Type "w"
Sleep 1.2s

# Usage history
Type "u"
Sleep 2.5s
Type "w"
Sleep 2s
Type "m"
Sleep 2s
Escape
Sleep 1.2s

# Config
Type "c"
Sleep 2s
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 400ms
Escape
Sleep 1.2s

# Rename
Type "R"
Sleep 800ms
Type "My important task"
Sleep 1.2s
Escape
Sleep 1.5s

# Quit
Type "q"
Sleep 500ms
