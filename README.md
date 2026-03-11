# c9s

Terminal dashboard for Claude Code -- like k9s, but for your AI sessions.

![Tests](https://github.com/StefanoGuerrini/c9s/actions/workflows/test.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue)

![c9s dashboard](docs/dashboard.png)

> **Beta** -- c9s is in early development and still needs validation. Feel free to use it! It reads local files only -- it never accesses your Claude account, never stores credentials, and adds zero cost to your Claude usage.

## Why c9s?

I tried [agent-deck](https://github.com/asheshgoplani/agent-deck), [ntm](https://github.com/Dicklesworthstone/ntm), and other tools for managing Claude Code sessions. They're powerful, but way more complex than what my workflow needed. I wanted something like [k9s](https://k9scli.io/) -- simple, keyboard-driven, zero setup. Launch one command, see all your sessions, open or resume any of them. That's it.

c9s reads directly from `~/.claude/`. No API calls, no network, no daemon. One binary + tmux.

## Features

- **Zero setup** -- reads directly from `~/.claude/`, no configuration needed
- **All sessions in one view** -- across every project directory
- **tmux integration** -- open/resume sessions in tmux windows, switch seamlessly
- **Live status** -- see which sessions are processing, waiting for input, or done
- **Session backup & restore** -- back up session JSONL files, auto-restore archived sessions
- **Effort picker** -- choose effort level (low/medium/high/max) when creating new sessions
- **Preview panel** -- toggle a detail panel showing tokens, messages, first prompt, and more
- **In-app config editor** -- press `c` to customize keybindings, colors, and refresh interval
- **Search & filter** -- find sessions by name, project, or ID
- **Group by project or status** -- cycle grouping modes with `Tab`
- **Token usage** -- see total tokens per session
- **Rename sessions** -- give sessions meaningful names
- **Fully configurable** -- colors, keybindings, refresh interval via `~/.c9s/config.json`

## Install

### Homebrew

```bash
brew install stefanoguerrini/tap/c9s
```

### Go install

```bash
go install github.com/stefanoguerrini/c9s@latest
```

### From source

```bash
git clone https://github.com/stefanoguerrini/c9s
cd c9s
go build -o c9s .
```

## Quick start

```bash
c9s
```

That's it. c9s auto-creates a tmux session, launches the dashboard, and you're ready to go. If a c9s session already exists, it re-attaches to it.

Want to try it without real sessions? Run `c9s --demo` to see the dashboard with sample data.

## Keybindings

### Dashboard

| Key | Action |
|-----|--------|
| `j/k` or `Up/Down` | Navigate sessions |
| `Enter` | Open/resume selected session |
| `n` | New Claude session (in selected project dir) |
| `N` | New session with effort picker (low/medium/high/max) |
| `x` | Close managed tmux window |
| `R` | Rename session |
| `b` | Back up session JSONL file |
| `/` | Search sessions |
| `Esc` | Clear search filter |
| `Tab` | Cycle grouping: none / project / status |
| `p` | Toggle preview panel |
| `t` | Toggle token column |
| `c` | Open config editor |
| `q` / `Ctrl+c` | Quit (kills tmux session) |

### Inside a Claude session window

| Key | Action |
|-----|--------|
| `Ctrl+d` | Return to dashboard |
| `Ctrl+n` | Next session window |
| `Ctrl+p` | Previous session window |

These navigation keys are configurable via the config editor or `~/.c9s/config.json`.

When Claude exits, the window automatically returns to the dashboard.

## Session status

c9s shows the lifecycle state of each session:

| Status | Meaning |
|--------|---------|
| **active** | Session JSONL modified in the last 5 minutes |
| **idle** | Claude process running but not recently active |
| **resumable** | Session file exists on disk, can be resumed |
| **archived** | Only in history, no file on disk |

For sessions opened through c9s, you also see real-time pane status:

| Pane status | Meaning |
|-------------|---------|
| **processing** | Claude is actively generating output |
| **waiting** | Claude needs your input (tool approval, question) |
| **done** | Task completed, at the main prompt |

## Configuration

![c9s config editor](docs/config.png)

c9s stores its config at `~/.c9s/config.json`. You can edit it directly or use the built-in config editor (press `c` on the dashboard).

Configurable settings:

- **Refresh interval** -- how often the dashboard polls for updates (1-10 seconds)
- **Navigation keys** -- tmux keybindings for dashboard/next/prev session (default: `Ctrl+d`, `Ctrl+n`, `Ctrl+p`)
- **Color theme** -- switch between `default` and `custom`, then tweak individual colors
- **All colors** -- title, header, status indicators, preview panel, tmux status bar

Example config:

```json
{
  "theme": "default",
  "refresh_seconds": 3,
  "keys": {
    "dashboard": "C-d",
    "next_session": "C-n",
    "prev_session": "C-p"
  }
}
```

## How it works

c9s reads Claude Code's local data files:

- `~/.claude/history.jsonl` -- discovers all sessions and projects
- `~/.claude/projects/<path>/sessions-index.json` -- session titles, summaries
- `~/.claude/projects/<path>/<session>.jsonl` -- token usage, file mtime for status

No API calls. No network access. Everything is local.

Process detection uses `ps` + `lsof` to find running Claude processes and match them to sessions. File mtimes are cached to keep the dashboard fast.

## Requirements

- **macOS or Linux** (tmux doesn't run natively on Windows)
- [tmux](https://github.com/tmux/tmux) -- installed automatically when using Homebrew, otherwise `brew install tmux` or `apt install tmux`
- Go 1.24+ (only needed to build from source)

## Related projects

- [agent-deck](https://github.com/asheshgoplani/agent-deck) -- a more feature-rich multi-agent dashboard
- [ntm](https://github.com/Dicklesworthstone/ntm) -- tmux session manager for orchestrating multiple AI coding agents in parallel

## License

[MIT](LICENSE)
