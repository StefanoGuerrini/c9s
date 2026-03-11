# c9s — Claude Code Dashboard

A simple terminal dashboard for Claude Code. Run `c9s`, see all your projects and sessions, create new ones, attach to running ones. Zero setup — it reads directly from `~/.claude/`.

Inspired by k9s: simple, fast, keyboard-driven.

## Vision

- **Dashboard-first**: Launch c9s → see all your Claude Code projects and sessions across all directories
- **Zero setup**: Discovers existing sessions from `~/.claude/history.jsonl` and `~/.claude/projects/`
- **tmux for sessions**: Real interactive Claude Code terminals via tmux windows (no in-process terminal emulator)
- **Simple**: One binary + tmux. No daemon, no plugins, no agent mail. Just a dashboard.
- **Future**: Extend to other CLI agents (Codex, Gemini) — but Claude Code first.

## Data sources (all local, no API)

| What | Where |
|------|-------|
| All sessions + projects | `~/.claude/history.jsonl` |
| Token usage per session | `~/.claude/projects/<path>/<session>.jsonl` → sum `message.usage` |
| Session name/title | `sessions-index.json` → `customTitle`, or `history.jsonl` → `display` |
| Session status | JSONL file mtime + `ps`/`lsof` process detection |

## Testing

**Always write tests for every change.** No code lands without corresponding test coverage.

- Tests live next to the code they test (`foo_test.go` alongside `foo.go`)
- Run all tests: `go test ./... -count=1`
- Run a specific package: `go test ./internal/claude/ -v`
- Use `t.TempDir()` for any file I/O in tests — never write to real paths
- Table-driven tests preferred for functions with multiple input/output cases
- Test both happy paths and edge cases (missing files, empty inputs, etc.)

## Architecture

```
c9s/
├── main.go                          # Bubbletea TUI + tmux bootstrap + keybindings
├── internal/
│   ├── claude/
│   │   ├── sessions.go              # Session discovery, token reading, process detection
│   │   └── sessions_test.go         # Tests
│   ├── config/
│   │   ├── config.go                # Config loading from ~/.c9s/config.json
│   │   └── config_test.go           # Tests
│   └── tmux/
│       ├── tmux.go                  # tmux CLI wrapper, pane status, status bar
│       ├── tmux_test.go             # Tests
│       ├── exec_unix.go             # syscall.Exec (Unix)
│       └── exec_windows.go          # Stub (Windows)
```

### Key design decisions

- **No daemon**: Direct file reads from `~/.claude/`. No IPC, no persistent background process.
- **Mtime-based caching**: History, tokens, and process lists are cached and only re-read when files change. This keeps the dashboard fast even with many sessions.
- **tmux windows**: Each claude session runs in its own tmux window (not pane). Dashboard is window 0. `Ctrl+d` returns to dashboard.
- **Custom prefix**: c9s disables the tmux prefix in its session to avoid interfering with user's tmux config.
- **Hybrid status detection**: File mtime for processing state, pane content capture for done vs waiting distinction.
- **Auto-bootstrap**: Running `./c9s` outside tmux auto-creates a `c9s` tmux session and attaches.

### Session status model

| Status | How detected | Meaning |
|--------|-------------|---------|
| `active` | JSONL mtime < 5 min | Session recently used |
| `idle` | claude process running (ps/lsof) | Process alive but not recent |
| `resumable` | JSONL file exists on disk | Can be resumed with `--resume` |
| `archived` | No JSONL file | Only in history, not on disk |

For managed windows (opened via c9s), additional pane statuses:

| Pane Status | How detected | Meaning |
|------------|-------------|---------|
| `processing` | JSONL mtime < 10s | Claude is actively generating |
| `done` | Main `❯` prompt visible | Task completed |
| `waiting` | Not at main prompt | Claude needs user input (approval, question) |

### Data flow

```
~/.claude/history.jsonl  →  readHistory() [cached by mtime]
        ↓
    SessionInfo[]
        ↓  enrich from sessions-index.json
        ↓  stat JSONL files → status + mtime
        ↓  readTokenUsage() [cached by mtime]
        ↓  listClaudeProcesses() [cached 5s TTL, batched lsof]
        ↓
    ListAllSessions() → model.sessions → filtered() → items() → View()
```

### tmux flow

```
./c9s (outside tmux)
  → tmux new-session -s c9s -n dashboard "c9s --inside-tmux"
  → tmux attach-session -t c9s

Enter on session → tmux new-window "claude --resume <id>"
n key            → tmux new-window "claude" (in project dir)
Ctrl+d           → select-window to dashboard
Ctrl+n/p         → switch between session windows
claude exits     → auto-returns to dashboard
q on dashboard   → kills entire c9s tmux session
```

### Packages

| Package | Responsibility | Dependencies |
|---------|---------------|-------------|
| `claude` | Session discovery, tokens, process detection | None |
| `config` | Config loading from `~/.c9s/config.json` | None |
| `tmux` | tmux CLI wrapper, pane status, status bar | None |
| `main` | Bubbletea TUI, tmux bootstrap, keybindings | `claude`, `config`, `tmux` |

## Keybindings

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate |
| `Enter` | Open/resume selected session |
| `n` | New claude session (in selected project dir) |
| `x` | Close managed tmux window |
| `R` | Rename session (writes to sessions-index.json) |
| `/` | Search sessions |
| `Esc` | Clear search filter |
| `Tab` | Cycle grouping: none → project → status |
| `p` | Toggle session preview panel |
| `t` | Toggle token column |
| `r` | Cycle refresh interval (1s/2s/3s/5s) |
| `q` / `Ctrl+c` | Quit |
| `Ctrl+n/p` | Next/previous session window (from claude window) |
| `Ctrl+d` | Return to dashboard (from claude window) |

## Code guidelines

- **Keep it simple**: No daemon, no IPC, no abstractions for hypothetical futures.
- **Performance**: Cache everything by mtime. Batch external commands (single lsof for all PIDs).
- **tmux interaction**: All tmux operations go through `internal/tmux/`. Shell out via `os/exec`.
- **Error handling**: Prefer returning errors over panicking. TUI shows errors in status bar.
- **Cross-platform**: tmux is Unix-only. Windows stub exists for exec.

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [bubbles](https://github.com/charmbracelet/bubbles) — Text input component
- [lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [tmux](https://github.com/tmux/tmux) — Terminal multiplexer (runtime dependency)

## Nice to have

- **Sandboxing**: Run Claude Code sessions in bubblewrap or similar sandbox
- **Multi-agent**: Support Codex, Gemini CLI agents alongside Claude Code
- **Session details view**: Expand a session to see full token breakdown, duration, git branch
- **Sort controls**: Sort by name, status, modified, tokens

## Build & run

```bash
go build -o c9s .    # build
./c9s                # run (auto-bootstraps into tmux)
go test ./... -v     # test
go vet ./...         # lint
```

### Prerequisites

- Go 1.24+
- tmux (`brew install tmux`)
