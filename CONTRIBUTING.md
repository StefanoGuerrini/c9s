# Contributing to c9s

Contributions are welcome! Before opening a PR, please read the philosophy below.

## Philosophy

c9s is intentionally simple. It's a dashboard, not a platform. Every feature should earn its place by being immediately useful without adding complexity.

Before contributing, ask yourself:

- **Does this keep c9s simple?** If it needs a config page to explain, it might be too complex.
- **Is it read-heavy?** c9s observes and organizes — it doesn't orchestrate or automate.
- **Does it work with zero setup?** New features should just work. No extra config, no dependencies.
- **Is it the minimum needed?** Three lines of straightforward code is better than a clever abstraction.

## Guidelines

- **Always write tests.** No code lands without corresponding test coverage.
- **Keep dependencies minimal.** Bubbletea, lipgloss, tmux. That's it.
- **No daemon, no IPC.** Direct file reads, simple CLI shelling.
- **Performance matters.** Cache by mtime, batch external commands, skip unnecessary re-renders.
- **Error handling over panicking.** Return errors, show them in the status bar.

## Process

1. Open an issue first to discuss the change
2. Fork and create a feature branch
3. Write code + tests
4. Run `go test ./... -count=1` and `go vet ./...`
5. Open a PR — all contributions will be reviewed

## What we're looking for

- Bug fixes
- Performance improvements
- Quality-of-life improvements that keep the spirit of simplicity
- Better test coverage

## What we'll likely decline

- Features that add significant complexity for niche use cases
- Daemon processes, background services, or IPC mechanisms
- Heavy dependencies or framework additions
- Changes that break the zero-config experience
