package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/stefanoguerrini/c9s/internal/tmux"
)

// runCycle handles `c9s _cycle <next|prev>`. It walks all c9s session
// windows ordered by state priority — waiting first (needs you most),
// then processing, then done, then unknown — and selects the next or
// previous one relative to the current pane.
//
// Bound to Ctrl+n / Ctrl+p in SetupNavigationKeys via `run-shell`. Falls
// back to plain tmux next-window / previous-window order when no state
// files exist (every window's @c9s-state is empty → all "unknown" →
// stable ordering by tmux window index).
func runCycle(direction string) int {
	if direction != "next" && direction != "prev" {
		fmt.Fprintf(os.Stderr, "c9s _cycle: expected next|prev, got %q\n", direction)
		return 1
	}

	out, err := exec.Command("tmux", "list-windows",
		"-t", tmux.SessionName,
		"-F", "#{window_id}\t#{window_name}\t#{@c9s-state}",
	).Output()
	if err != nil {
		return 0 // tmux not running or session gone — silent.
	}

	windows := parseCycleWindows(string(out))
	if len(windows) <= 1 {
		return 0
	}

	current := os.Getenv("TMUX_PANE") // pane ID, not window ID — fall back to active
	currentWindowID := currentWindowID()
	sort.SliceStable(windows, func(i, j int) bool {
		pi, pj := statePriority(windows[i].State), statePriority(windows[j].State)
		if pi != pj {
			return pi < pj
		}
		return windows[i].Index < windows[j].Index
	})

	idx := -1
	for i, w := range windows {
		if w.ID == currentWindowID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Not in any tracked window — jump to the first one.
		idx = 0
	} else if direction == "next" {
		idx = (idx + 1) % len(windows)
	} else {
		idx = (idx - 1 + len(windows)) % len(windows)
	}

	_ = exec.Command("tmux", "select-window", "-t", windows[idx].ID).Run()
	_ = current
	return 0
}

// cycleWindow is a tmux window enriched with its c9s state badge.
type cycleWindow struct {
	ID    string
	Name  string
	State string // "waiting" / "processing" / "done" / "unknown" / ""
	Index int    // arrival order from tmux list-windows
}

func parseCycleWindows(s string) []cycleWindow {
	var out []cycleWindow
	for i, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		// Skip the dashboard window — cycling never lands there.
		if parts[1] == tmux.DashboardWindow {
			continue
		}
		w := cycleWindow{ID: parts[0], Name: parts[1], Index: i}
		if len(parts) >= 3 {
			// The state is wrapped in tmux format directives (e.g.
			// "#[fg=...]● waiting#[default]"); extract the bare keyword.
			w.State = extractStateKeyword(parts[2])
		}
		out = append(out, w)
	}
	return out
}

// extractStateKeyword pulls "waiting" / "processing" / "done" / "unknown"
// out of the rendered tmux badge string. The badge contains tmux format
// escapes plus a "● <state>" body; we just look for the keyword.
func extractStateKeyword(badge string) string {
	for _, kw := range []string{"waiting", "processing", "done", "unknown"} {
		if strings.Contains(badge, kw) {
			return kw
		}
	}
	return ""
}

// statePriority ranks pane states for cycling: lower = higher priority.
func statePriority(state string) int {
	switch state {
	case "waiting":
		return 0
	case "processing":
		return 1
	case "done":
		return 2
	default: // unknown or unset
		return 3
	}
}

// currentWindowID returns the tmux window id of the active pane, or empty
// if it can't be resolved.
func currentWindowID() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{window_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
