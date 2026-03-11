package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	SessionName    = "c9s"
	DashboardWindow = "dashboard"
)

// Available returns true if tmux is installed.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// InSession returns true if we're running inside a tmux session.
func InSession() bool {
	return os.Getenv("TMUX") != ""
}

// InC9sSession returns true if we're inside the c9s tmux session.
func InC9sSession() bool {
	if !InSession() {
		return false
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == SessionName
}

// SessionExists returns true if the c9s tmux session exists.
func SessionExists() bool {
	return exec.Command("tmux", "has-session", "-t", SessionName).Run() == nil
}

// Bootstrap creates the c9s tmux session and attaches to it, re-executing
// the given binary with --inside-tmux inside the session. This replaces the
// current process (exec) on success.
func Bootstrap(selfBin string, args []string, keys NavKeys, colors StatusColors, version string, scrollSpeed int) error {
	// Create new tmux session with dashboard window running c9s --inside-tmux.
	cmdArgs := append([]string{selfBin, "--inside-tmux"}, args...)
	cmd := strings.Join(cmdArgs, " ")

	err := exec.Command("tmux", "new-session", "-d",
		"-s", SessionName,
		"-n", DashboardWindow,
		cmd,
	).Run()
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}

	// Customize the status bar for c9s.
	ConfigureStatusBar(keys, colors, version, scrollSpeed)

	// Attach to the session (this takes over the terminal).
	return Attach()
}

// Attach attaches the current terminal to the c9s tmux session.
func Attach() error {
	tmuxBin, _ := exec.LookPath("tmux")
	return execSyscall(tmuxBin, []string{"tmux", "attach-session", "-t", SessionName})
}

// NewWindow creates a new tmux window in the c9s session with the given
// name and command. When the command exits, the window auto-returns to
// the dashboard. Returns the window ID.
func NewWindow(name, shellCmd, workDir string) (string, error) {
	// Wrap command: run claude, then switch back to dashboard when it exits.
	wrapped := fmt.Sprintf(
		`echo "Press Ctrl+b then b to return to dashboard"; %s; tmux select-window -t %s:%s 2>/dev/null`,
		shellCmd, SessionName, DashboardWindow,
	)

	args := []string{"new-window", "-t", SessionName, "-n", name, "-P", "-F", "#{window_id}"}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}
	args = append(args, "sh", "-c", wrapped)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SelectWindow switches to the given window in the c9s session.
func SelectWindow(windowID string) error {
	return exec.Command("tmux", "select-window", "-t", windowID).Run()
}

// SelectDashboard switches back to the dashboard window.
func SelectDashboard() error {
	return exec.Command("tmux", "select-window", "-t",
		fmt.Sprintf("%s:%s", SessionName, DashboardWindow),
	).Run()
}

// KillWindow kills the given window.
func KillWindow(windowID string) error {
	return exec.Command("tmux", "kill-window", "-t", windowID).Run()
}

// RenameWindow renames the given tmux window.
func RenameWindow(windowID, name string) error {
	return exec.Command("tmux", "rename-window", "-t", windowID, name).Run()
}

// ListWindows returns window names and IDs in the c9s session.
func ListWindows() ([]WindowInfo, error) {
	out, err := exec.Command("tmux", "list-windows",
		"-t", SessionName,
		"-F", "#{window_id}\t#{window_name}\t#{pane_current_command}",
	).Output()
	if err != nil {
		return nil, err
	}

	var windows []WindowInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		w := WindowInfo{ID: parts[0], Name: parts[1]}
		if len(parts) >= 3 {
			w.Command = parts[2]
		}
		windows = append(windows, w)
	}
	return windows, nil
}

// WindowInfo describes a tmux window.
type WindowInfo struct {
	ID      string // e.g. @1
	Name    string // window name
	Command string // current pane command
}

// PaneStatus represents the state of a claude session inside a tmux pane.
type PaneStatus int

const (
	PaneProcessing PaneStatus = iota // claude is generating output
	PaneWaiting                      // claude is waiting for user input
	PaneDone                         // claude process has exited
)

func (s PaneStatus) String() string {
	switch s {
	case PaneWaiting:
		return "waiting"
	case PaneProcessing:
		return "processing"
	default:
		return "done"
	}
}

// WindowExists returns true if the given window ID still exists.
func WindowExists(windowID string) bool {
	return exec.Command("tmux", "list-panes", "-t", windowID).Run() == nil
}

// CapturePaneTail captures the last N lines of the pane in the given window.
func CapturePaneTail(windowID string, lines int) (string, error) {
	out, err := exec.Command("tmux", "capture-pane",
		"-t", windowID,
		"-p",
		"-S", fmt.Sprintf("-%d", lines),
	).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// IsAtMainPrompt checks whether the Claude Code pane is showing the main
// input prompt (❯ between ─── box lines). This means Claude has finished
// its task and is ready for a new message — i.e., "done".
//
// When Claude needs user input (tool approval, questions), it shows a
// different UI (buttons, [Y/n], etc.) — NOT the main ❯ prompt.
func IsAtMainPrompt(windowID string) bool {
	content, err := CapturePaneTail(windowID, 10)
	if err != nil {
		return false
	}
	return classifyPrompt(content)
}

// classifyPrompt returns true if the pane content shows the main ❯ prompt
// (between ─── box-drawing lines), meaning Claude is done and idle.
func classifyPrompt(content string) bool {
	lines := strings.Split(content, "\n")

	// Walk from bottom up, looking for ❯ between ─── lines.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// The main prompt is: ───\n❯\n───\n-- INSERT --
		// Check for ❯ with a ─── line above it.
		if strings.HasPrefix(line, "❯") {
			// Check if there's a box line above.
			for j := i - 1; j >= 0 && j >= i-2; j-- {
				prev := strings.TrimSpace(lines[j])
				if prev != "" {
					return isBoxLine(prev)
				}
			}
			return false
		}

		// Skip -- INSERT -- / -- NORMAL -- and ─── lines.
		if strings.HasPrefix(line, "-- INSERT --") || strings.HasPrefix(line, "-- NORMAL --") {
			continue
		}
		if isBoxLine(line) {
			continue
		}

		// Any other content at the bottom means it's NOT at the main prompt.
		return false
	}
	return false
}

// isBoxLine returns true if the line consists entirely of box-drawing horizontal chars.
func isBoxLine(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '─' && r != '━' {
			return false
		}
	}
	return true
}

// NavKeys holds the configurable tmux keybindings.
type NavKeys struct {
	Dashboard   string // tmux key for return to dashboard (e.g. "C-d")
	NextSession string // tmux key for next session (e.g. "C-n")
	PrevSession string // tmux key for previous session (e.g. "C-p")
}

// DefaultNavKeys returns the default navigation keybindings.
func DefaultNavKeys() NavKeys {
	return NavKeys{Dashboard: "C-d", NextSession: "C-n", PrevSession: "C-p"}
}

// StatusColors holds configurable tmux status bar colors.
type StatusColors struct {
	Bg     string // background (e.g. "#1b1b2f")
	Fg     string // foreground (e.g. "#8888aa")
	Accent string // c9s label (e.g. "#bb86fc")
	Dim    string // separator/hints (e.g. "#555577")
}

// DefaultStatusColors returns the default status bar colors.
func DefaultStatusColors() StatusColors {
	return StatusColors{Bg: "#1b1b2f", Fg: "#8888aa", Accent: "#bb86fc", Dim: "#555577"}
}

// keyDisplayName converts a tmux key like "C-d" to a human-readable form like "ctrl+d".
func keyDisplayName(tmuxKey string) string {
	if strings.HasPrefix(tmuxKey, "C-") {
		return "ctrl+" + tmuxKey[2:]
	}
	return tmuxKey
}

// ConfigureStatusBar customizes the tmux status bar and prefix for the c9s session.
// scrollSpeed controls lines per mouse wheel event (0 or negative = tmux default).
func ConfigureStatusBar(keys NavKeys, colors StatusColors, version string, scrollSpeed int) {
	t := func(option, value string) {
		exec.Command("tmux", "set-option", "-t", SessionName, option, value).Run()
	}

	// Disable the default tmux prefix in the c9s session so it doesn't
	// interfere with the user's own tmux bindings or terminal shortcuts.
	t("prefix", "None")
	t("prefix2", "None")

	// Enable mouse support so users can scroll through Claude session
	// history with the mouse wheel / trackpad.
	// Hold Shift (or Option in iTerm2) to select text for copying.
	t("mouse", "on")

	// Configure scroll speed (lines per wheel event).
	if scrollSpeed > 0 {
		// Build tmux bind-key args that chain N scroll commands with ";" separators.
		// tmux expects: bind-key -T copy-mode WheelUpPane send-keys -X scroll-up \; send-keys -X scroll-up ...
		buildArgs := func(table, key, scrollDir string) []string {
			args := []string{"bind-key", "-T", table, key}
			for i := 0; i < scrollSpeed; i++ {
				if i > 0 {
					args = append(args, ";")
				}
				args = append(args, "send-keys", "-X", scrollDir)
			}
			return args
		}
		exec.Command("tmux", buildArgs("copy-mode", "WheelUpPane", "scroll-up")...).Run()
		exec.Command("tmux", buildArgs("copy-mode", "WheelDownPane", "scroll-down")...).Run()
	}

	// Enable extended keys so modifiers like Ctrl+Enter pass through
	// correctly to applications (e.g., Claude Code uses Ctrl+Enter for newline).
	// "always" sends CSI u sequences even if the app doesn't request them.
	t("extended-keys", "always")
	// Allow applications to request extended key mode via CSI u sequences.
	t("allow-passthrough", "on")

	// Use status-format to take full control — no default window list.
	t("status-style", fmt.Sprintf("bg=%s,fg=%s", colors.Bg, colors.Fg))
	t("status-position", "bottom")
	// Prevent tmux from auto-renaming or truncating window names.
	w := func(option, value string) {
		exec.Command("tmux", "set-window-option", "-t", SessionName, option, value).Run()
	}
	w("automatic-rename", "off")
	w("allow-rename", "off")

	nextPrev := keyDisplayName(keys.NextSession) + "/" + keyDisplayName(keys.PrevSession)[len("ctrl+"):]
	dash := keyDisplayName(keys.Dashboard)
	// On dashboard: show version right-aligned.
	// On session windows: show nav hints right-aligned.
	t("status-format[0]",
		fmt.Sprintf("#[fg=%s,bold] c9s #[fg=%s]│ #[fg=%s]#W ", colors.Accent, colors.Dim, colors.Fg)+
			"#[align=right]"+
			fmt.Sprintf("#{?#{==:#W,%s},#[fg=%s]%s ,#[fg=%s]%s switch  %s ← dashboard }",
				DashboardWindow, colors.Dim, version,
				colors.Dim, nextPrev, dash))
}

// SetupNavigationKeys binds configurable keys for the c9s session (root table, no prefix).
// All bindings use if-shell to only activate inside the c9s session;
// in other sessions the keys pass through normally.
func SetupNavigationKeys(keys NavKeys) error {
	sessionCheck := fmt.Sprintf("#{==:#{session_name},%s}", SessionName)

	// Dashboard key → back to dashboard
	if err := exec.Command("tmux", "bind-key",
		"-n", keys.Dashboard,
		"if-shell", "-F", sessionCheck,
		fmt.Sprintf("select-window -t %s:%s ; refresh-client", SessionName, DashboardWindow),
		fmt.Sprintf("send-keys %s", keys.Dashboard),
	).Run(); err != nil {
		return err
	}

	// Next session key → next window, skip dashboard
	nextCmd := fmt.Sprintf(
		"next-window ; if-shell -F '#{==:#W,%s}' next-window ; refresh-client",
		DashboardWindow,
	)
	if err := exec.Command("tmux", "bind-key",
		"-n", keys.NextSession,
		"if-shell", "-F", sessionCheck,
		nextCmd,
		fmt.Sprintf("send-keys %s", keys.NextSession),
	).Run(); err != nil {
		return err
	}

	// Previous session key → previous window, skip dashboard
	prevCmd := fmt.Sprintf(
		"previous-window ; if-shell -F '#{==:#W,%s}' previous-window ; refresh-client",
		DashboardWindow,
	)
	return exec.Command("tmux", "bind-key",
		"-n", keys.PrevSession,
		"if-shell", "-F", sessionCheck,
		prevCmd,
		fmt.Sprintf("send-keys %s", keys.PrevSession),
	).Run()
}

// CleanupNavigationKeys removes the c9s key bindings.
func CleanupNavigationKeys(keys NavKeys) error {
	exec.Command("tmux", "unbind-key", "-n", keys.Dashboard).Run()
	exec.Command("tmux", "unbind-key", "-n", keys.NextSession).Run()
	exec.Command("tmux", "unbind-key", "-n", keys.PrevSession).Run()
	return nil
}

// KillSession kills the entire c9s tmux session.
// This detaches all clients and destroys all windows.
func KillSession() error {
	return exec.Command("tmux", "kill-session", "-t", SessionName).Run()
}
