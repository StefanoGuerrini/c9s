# c9s

Terminal dashboard for Claude Code.

![Tests](https://github.com/StefanoGuerrini/c9s/actions/workflows/test.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue)

![c9s demo](docs/demo.gif)

> **Beta** -- c9s is in early development and still needs validation. Feel free to use it! It reads local files and optionally fetches your usage percentage via Claude Code's OAuth token. It never stores credentials and adds zero cost to your Claude usage. Feedback and contributions are welcome!

## Why c9s?

If you use Claude Code daily, you know the problem: dozens of sessions scattered across projects, no easy way to see what's running, what's waiting for input, or what you left off yesterday. Context switching between sessions means hunting through terminals, remembering session IDs, and losing track of what's where.

I tried [agent-deck](https://github.com/asheshgoplani/agent-deck), [ntm](https://github.com/Dicklesworthstone/ntm), and other tools for managing Claude Code sessions. They're powerful, but way more complex than what my workflow needed. I wanted something like [k9s](https://k9scli.io/) -- simple, keyboard-driven, zero setup. Launch one command, see all your sessions, jump between them instantly. That's it.

c9s gives you a single dashboard for every session on your machine. See at a glance which ones are actively processing, which need your attention, and which are ready to resume. Switch between sessions in a keystroke. No more lost context, no more forgotten sessions.

It reads directly from `~/.claude/`. No API calls, no network, no daemon. One binary + tmux.

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
- **Git worktree awareness** *(beta)* -- see worktrees per session, open sessions in any worktree
- **API usage tracking** -- see your 5-hour and 7-day utilization in the status bar and a dedicated history screen
- **Mouse scroll** -- scroll through Claude conversation history, hold Shift/Option to copy text
- **Persistent dashboard state** -- toggles (tokens, preview, grouping, worktrees) survive restarts
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
| `w` | Toggle worktree sub-rows (when enabled) |
| `u` | Usage history screen |
| `c` | Open config editor |
| `q` / `Ctrl+c` | Quit (or detach if keep_alive is on) |

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

## Git worktrees (beta)

If you use git worktrees for parallel development, c9s can show them in the dashboard. This feature is **off by default** -- enable it in the config editor (`c` → Worktrees → Mode).

| Mode | Behavior |
|------|----------|
| `off` | Worktrees disabled (default) |
| `auto` | Show worktrees when a project has 2+ worktrees |
| `always` | Always show worktrees |

Once enabled, press `w` to toggle worktree sub-rows beneath sessions. Select a worktree and press `Enter` to start a new Claude session in that directory.

This feature is in beta -- we're still evaluating the best experience while keeping c9s simple.

## API usage tracking

c9s can show your Anthropic API utilization directly in the tmux status bar and in a dedicated history screen. It reads from Claude Code's OAuth token (stored locally) -- no API keys or extra setup needed.

### Status bar

The status bar shows your current 5-hour utilization percentage. A `?` suffix indicates the data is stale (cached after a failed refresh). Configure what to show via the `status_usage` setting:

| Value | What's shown |
|-------|-------------|
| `percent` | 5-hour utilization % (default) |
| `cost` | Estimated session cost |
| `tokens` | Total tokens across sessions |
| `all` | Tokens, cost, and percent |
| `off` | Nothing |

Combine values with commas, e.g. `cost,percent`.

### Usage history

Press `u` on the dashboard to open the usage history screen. It shows your utilization over time with visual bars:

- **5h peak** -- highest 5-hour utilization seen in that period
- **7d last** -- last recorded 7-day utilization
- **Tokens** -- net token delta for the period (max minus min snapshot)

Switch between views:

| Key | View |
|-----|------|
| `d` | Daily (default) |
| `w` | Weekly |
| `m` | Monthly |
| `7` / `1` / `3` | Last 7 / 14 / 30 days |
| `q` / `Esc` | Back to dashboard |

Usage data is recorded to `~/.c9s/usage-history.jsonl` every 5 minutes while c9s is running. You can disable recording or reset history in the config editor (`c` → Usage history).

## Configuration

![c9s config editor](docs/config.png)

c9s stores its config at `~/.c9s/config.json`. You can edit it directly or use the built-in config editor (press `c` on the dashboard).

Configurable settings:

- **Refresh interval** -- how often the dashboard polls for updates (1-10 seconds)
- **Scroll speed** -- lines per mouse scroll event in session windows (1-10)
- **Work directory** -- default directory for new sessions (empty = current directory)
- **Keep alive** -- when on, quitting c9s detaches instead of killing sessions. Claude keeps running in the background, re-run `c9s` to re-attach
- **Status bar usage** -- what metrics to show in the tmux status bar (percent/cost/tokens/all/off)
- **Usage history** -- enable/disable recording, reset history
- **Worktrees** -- mode (off/auto/always) and expand behavior (all/selected)
- **Navigation keys** -- tmux keybindings for dashboard/next/prev session (default: `Ctrl+d`, `Ctrl+n`, `Ctrl+p`)
- **Color theme** -- switch between `default` and `custom`, then tweak individual colors
- **All colors** -- title, header, status indicators, preview panel, tmux status bar

Press `?` in the config editor to see descriptions for each setting.

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

No API keys needed. Session data is entirely local. The only network call is an optional usage API request (using Claude Code's existing OAuth token) to show your utilization percentage.

Process detection uses `ps` + `lsof` to find running Claude processes and match them to sessions. File mtimes are cached to keep the dashboard fast.

## Requirements

- **macOS or Linux** (tmux doesn't run natively on Windows)
- [tmux](https://github.com/tmux/tmux) -- installed automatically when using Homebrew, otherwise `brew install tmux` or `apt install tmux`
- Go 1.24+ (only needed to build from source)

## Known limitations

**Flickering in large sessions** -- Claude Code generates thousands of scroll events per second when streaming output, which can cause visible flickering in tmux. This is a [known Claude Code + tmux issue](https://github.com/anthropics/claude-code/issues/9935), not specific to c9s.

c9s applies performance optimizations automatically (`escape-time 0`, `monitor-activity off`, increased scrollback buffer). When tmux 3.7 is released with synchronized output (DEC mode 2026), c9s will auto-enable it — this eliminates flickering entirely.

**Workarounds for tmux 3.6:**
- [claude-chill](https://github.com/davidbeesley/claude-chill) -- a PTY proxy that wraps Claude's output in synchronized frames
- [Ghostty](https://ghostty.org/) -- terminal with native synchronized output support
- Build tmux from [git master](https://github.com/tmux/tmux) for early mode 2026 support

## Related projects

- [agent-deck](https://github.com/asheshgoplani/agent-deck) -- a more feature-rich multi-agent dashboard
- [ntm](https://github.com/Dicklesworthstone/ntm) -- tmux session manager for orchestrating multiple AI coding agents in parallel

## License

[MIT](LICENSE)
