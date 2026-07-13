package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	DashboardWindow = "dashboard"
)

// SessionName is the tmux session this c9s process is bound to. Defaults to
// "c9s" — the legacy single-session name — but each terminal that bootstraps
// a fresh c9s gets its own name ("c9s-2", "c9s-3", …) so two dashboards in
// two terminals don't mirror each other. Set via SetCurrentSession before
// any tmux call. The `--inside-tmux` child resolves it from tmux itself via
// InC9sSession.
var SessionName = "c9s"

// SetCurrentSession sets the tmux session name this process targets.
func SetCurrentSession(name string) {
	if name == "" {
		return
	}
	SessionName = name
}

// CurrentSession returns the tmux session name this process targets.
func CurrentSession() string {
	return SessionName
}

// DryRun disables all tmux command execution. Used in tests to prevent
// side effects against a real tmux session.
var DryRun bool

// listSessionsFn and listClientsFn back ListC9sSessions / SessionHasClient.
// Tests replace these to exercise PickSessionName without a real tmux server.
var listSessionsFn = realListSessions
var listClientsFn = realListClients

func realListSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func realListClients(name string) (string, error) {
	out, err := exec.Command("tmux", "list-clients", "-t", name, "-F", "#{client_name}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Available returns true if tmux is installed.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// InSession returns true if we're running inside a tmux session.
func InSession() bool {
	return os.Getenv("TMUX") != ""
}

// InC9sSession returns true if we're inside a c9s tmux session (named "c9s"
// or "c9s-<N>"). As a side effect it sets SessionName to the detected name
// so subsequent calls target the right session.
func InC9sSession() bool {
	if !InSession() {
		return false
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(out))
	if !isC9sSessionName(name) {
		return false
	}
	SetCurrentSession(name)
	return true
}

// SessionExists returns true if a tmux session with the given name exists.
// With no arguments it checks the current SessionName, preserving the
// pre-multi-instance call shape.
func SessionExists(name ...string) bool {
	n := SessionName
	if len(name) > 0 && name[0] != "" {
		n = name[0]
	}
	return exec.Command("tmux", "has-session", "-t", n).Run() == nil
}

// ListC9sSessions returns the names of every "c9s" / "c9s-<N>" tmux session
// the local server knows about, in stable (numeric) order: "c9s" first, then
// "c9s-2", "c9s-3", …
func ListC9sSessions() []string {
	names, err := listSessionsFn()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		if isC9sSessionName(n) {
			out = append(out, n)
		}
	}
	sortC9sNames(out)
	return out
}

// SessionHasClient reports whether any tmux client is currently attached to
// the given session.
func SessionHasClient(name string) bool {
	out, err := listClientsFn(name)
	if err != nil {
		return false
	}
	return out != ""
}

// PickSessionName chooses the tmux session name a new c9s process should
// target.
//
//   - If force is true, always return the lowest free c9s* name (fresh session).
//   - Otherwise, if a c9s* session exists with no attached client, return that
//     (so re-running c9s after a detach reattaches).
//   - Otherwise, return the lowest free c9s* name.
//
// Pure logic over ListC9sSessions / SessionHasClient — see those for the
// underlying tmux calls.
func PickSessionName(force bool) string {
	existing := ListC9sSessions()
	if !force {
		for _, n := range existing {
			if !SessionHasClient(n) {
				return n
			}
		}
	}
	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n] = true
	}
	if !taken["c9s"] {
		return "c9s"
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("c9s-%d", i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// isC9sSessionName matches the bare "c9s" name and "c9s-<N>" variants.
func isC9sSessionName(name string) bool {
	if name == "c9s" {
		return true
	}
	if !strings.HasPrefix(name, "c9s-") {
		return false
	}
	suffix := name[len("c9s-"):]
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sortC9sNames sorts c9s session names with "c9s" first, then by numeric
// suffix ascending.
func sortC9sNames(names []string) {
	idx := func(s string) int {
		if s == "c9s" {
			return 1
		}
		n, err := strconv.Atoi(s[len("c9s-"):])
		if err != nil {
			return 1 << 30
		}
		return n
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && idx(names[j]) < idx(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

// Bootstrap creates the c9s tmux session and attaches to it, re-executing
// the given binary with --inside-tmux inside the session. This replaces the
// current process (exec) on success.
func Bootstrap(selfBin string, args []string, keys NavKeys, colors StatusColors, version string, scrollSpeed, refreshSeconds int) error {
	// Create new tmux session with dashboard window running c9s --inside-tmux.
	// Shell-quote each argument to handle paths with spaces or metacharacters.
	cmdArgs := append([]string{selfBin, "--inside-tmux"}, args...)
	cmd := shellQuoteJoin(cmdArgs)

	err := exec.Command("tmux", "new-session", "-d",
		"-s", SessionName,
		"-n", DashboardWindow,
		cmd,
	).Run()
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}

	// Customize the status bar for c9s.
	ConfigureStatusBar(keys, colors, version, scrollSpeed, refreshSeconds)

	// Attach to the session (this takes over the terminal).
	return Attach()
}

// CreateDashboardWindow creates a new dashboard window in the existing c9s
// tmux session. Used when re-attaching after keep_alive detach.
func CreateDashboardWindow(selfBin string, args []string) error {
	cmdArgs := append([]string{selfBin, "--inside-tmux"}, args...)
	cmd := shellQuoteJoin(cmdArgs)
	return exec.Command("tmux", "new-window", "-t", SessionName, "-n", DashboardWindow, cmd).Run()
}

// Attach attaches the current terminal to the c9s tmux session,
// targeting the dashboard window so it always opens on the dashboard.
func Attach() error {
	tmuxBin, _ := exec.LookPath("tmux")
	return execSyscall(tmuxBin, []string{"tmux", "attach-session", "-t", SessionName + ":" + DashboardWindow})
}

// NewWindow creates a new tmux window in the c9s session with the given
// name and command. The command is launched directly (via `sh -c` to honor
// env-var prefixes like `ANTHROPIC_MODEL=...`) — no wrapping shell adds
// hints or trailing tmux calls. When the command exits the window closes,
// and the `pane-exited` session hook installed by SetupNavigationKeys
// selects the dashboard. Returns the window ID.
func NewWindow(name, shellCmd, workDir string) (string, error) {
	if DryRun {
		return "@dry", nil
	}
	args := []string{"new-window", "-a", "-t", SessionName, "-n", name, "-P", "-F", "#{window_id}"}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}
	args = append(args, "sh", "-c", shellCmd)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		detail := ""
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			detail = ": " + strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("tmux new-window in %q%s", workDir, detail)
	}
	return strings.TrimSpace(string(out)), nil
}

// SetWindowEnv sets a per-window tmux user option (accessible in format strings as #{@key}).
// Uses set-window-option so each window has its own value (set-option would set session-level).
func SetWindowEnv(windowID, key, value string) {
	if DryRun {
		return
	}
	exec.Command("tmux", "set-window-option", "-t", windowID, "@"+key, value).Run()
}

// SelectWindow switches to the given window in the c9s session.
func SelectWindow(windowID string) error {
	if DryRun {
		return nil
	}
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
	if DryRun {
		return nil
	}
	return exec.Command("tmux", "kill-window", "-t", windowID).Run()
}

// RenameWindow renames the given tmux window.
func RenameWindow(windowID, name string) error {
	if DryRun {
		return nil
	}
	return exec.Command("tmux", "rename-window", "-t", windowID, name).Run()
}

// shellQuoteJoin quotes each argument for safe shell interpolation and joins them.
// Handles paths with spaces, quotes, and other shell metacharacters.
func shellQuoteJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		// Single-quote the arg, escaping any embedded single quotes.
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return strings.Join(quoted, " ")
}

// ListWindows returns window names, IDs, and tagged session IDs in the c9s session.
func ListWindows() ([]WindowInfo, error) {
	out, err := exec.Command("tmux", "list-windows",
		"-t", SessionName,
		"-F", "#{window_id}\t#{window_name}\t#{pane_current_command}\t#{@session-id}",
	).Output()
	if err != nil {
		return nil, err
	}

	var windows []WindowInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 2 {
			continue
		}
		w := WindowInfo{ID: parts[0], Name: parts[1]}
		if len(parts) >= 3 {
			w.Command = parts[2]
		}
		if len(parts) >= 4 {
			w.SessionID = parts[3]
		}
		windows = append(windows, w)
	}
	return windows, nil
}

// WindowInfo describes a tmux window.
type WindowInfo struct {
	ID        string // e.g. @1
	Name      string // window name
	Command   string // current pane command
	SessionID string // @session-id user option (empty if not set)
}

// PaneStatus represents the state of a claude session inside a tmux pane.
// Values are sourced from per-session state files written by Claude Code
// hooks (see internal/sessionstate). A managed window with no state file
// shows as PaneUnknown until the hooks are installed via `c9s install`.
type PaneStatus int

const (
	PaneProcessing PaneStatus = iota // claude is generating output
	PaneWaiting                      // claude is waiting for user input
	PaneDone                         // claude has finished a turn
	PaneUnknown                      // no state file for this session
)

func (s PaneStatus) String() string {
	switch s {
	case PaneWaiting:
		return "waiting"
	case PaneProcessing:
		return "processing"
	case PaneDone:
		return "done"
	default:
		return "unknown"
	}
}

// WindowExists returns true if the given window ID still exists.
func WindowExists(windowID string) bool {
	return exec.Command("tmux", "list-panes", "-t", windowID).Run() == nil
}

// GetPanePID returns the PID of the shell process in the given tmux window's pane.
func GetPanePID(windowID string) (int, error) {
	out, err := exec.Command("tmux", "display-message", "-t", windowID, "-p", "#{pane_pid}").Output()
	if err != nil {
		return 0, err
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &pid)
	if pid == 0 {
		return 0, fmt.Errorf("no pane pid for window %s", windowID)
	}
	return pid, nil
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

// SupportsSyncOutput returns true if tmux supports DEC mode 2026 (synchronized output).
// Available in tmux >= 3.7 / git master builds after Dec 2025.
func SupportsSyncOutput() bool {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return false
	}
	return parseTmuxVersionSupportsSync(strings.TrimSpace(string(out)))
}

// parseTmuxVersionSupportsSync checks if a tmux version string indicates mode 2026 support.
func parseTmuxVersionSupportsSync(version string) bool {
	// Format: "tmux 3.6a", "tmux 3.7", "tmux next-3.7", etc.
	version = strings.TrimPrefix(version, "tmux ")
	if strings.Contains(version, "next") {
		return true // dev/master builds
	}
	// Strip letter suffix (e.g., "3.6a" → "3.6").
	ver := strings.TrimRight(version, "abcdefghijklmnopqrstuvwxyz")
	parts := strings.SplitN(ver, ".", 2)
	if len(parts) != 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 3 || (major == 3 && minor >= 7)
}

// ConfigureStatusBar customizes the tmux status bar and prefix for the c9s session.
// scrollSpeed controls lines per mouse wheel event (0 or negative = tmux default).
// refreshSeconds controls the tmux status-interval for env var updates.
func ConfigureStatusBar(keys NavKeys, colors StatusColors, version string, scrollSpeed, refreshSeconds int) {
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

	// Enable extended keys *on demand*: tmux waits for the inner app to
	// request CSI u / kitty kbd protocol before sending extended encodings.
	// Claude Code requests them itself when it wants Ctrl+Enter for newline,
	// so "on" preserves that. The previous value "always" forced CSI u for
	// every key, which broke plain Esc inside Claude Code's /agents prompt
	// — `\x1b` arrived as `\x1b[27u` and the input parser ignored it.
	t("extended-keys", "on")
	// Allow applications to request extended key mode via CSI u sequences.
	t("allow-passthrough", "on")

	// Performance: reduce input lag and avoid unnecessary redraws.
	t("escape-time", "0")
	t("focus-events", "on")
	t("default-terminal", "tmux-256color")
	t("history-limit", "250000")

	// Synchronized output (DEC mode 2026) used to be advertised here as a
	// tmux terminal-feature so Claude Code would emit sync begin/end
	// sequences. In practice the interaction between Claude's modern redraw
	// pattern, tmux next-3.7's sync handling, and iTerm2 produced cursor-
	// position drift that left overlapping text in the pane. Modern tmux
	// already handles sync correctly without the hint, so we no longer set
	// it. If a future tmux/terminal combo regresses, surface this as an
	// opt-in setting rather than a default.

	// Use status-format to take full control — no default window list.
	t("status-style", fmt.Sprintf("bg=%s,fg=%s", colors.Bg, colors.Fg))
	t("status-position", "bottom")
	// Prevent tmux from auto-renaming or truncating window names.
	w := func(option, value string) {
		exec.Command("tmux", "set-window-option", "-t", SessionName, option, value).Run()
	}
	w("automatic-rename", "off")
	w("allow-rename", "off")
	w("monitor-activity", "off")

	// Sync status bar refresh with c9s tick interval so usage updates promptly.
	if refreshSeconds > 0 {
		t("status-interval", fmt.Sprintf("%d", refreshSeconds))
	}

	nextPrev := keyDisplayName(keys.NextSession) + "/" + keyDisplayName(keys.PrevSession)[len("ctrl+"):]
	dash := keyDisplayName(keys.Dashboard)
	// On dashboard: show usage + version right-aligned.
	// On session windows: show usage + nav hints right-aligned. The
	// processing/waiting/done state badge lives only in the dashboard
	// table — keeping it out of the tmux bar reduces visual noise inside
	// claude windows. #{@c9s-usage} is set per-window by the tick handler.
	usageFmt := fmt.Sprintf("#{?#{@c9s-usage},#[fg=%s]#{@c9s-usage}  ,}", colors.Fg)
	t("status-format[0]",
		fmt.Sprintf("#[fg=%s,bold] c9s #[fg=%s]│ #[fg=%s]#W ", colors.Accent, colors.Dim, colors.Fg)+
			"#[align=right]"+
			fmt.Sprintf("#{?#{==:#W,%s},%s#[fg=%s]%s ,%s#[fg=%s]%s switch  %s ← dashboard }",
				DashboardWindow, usageFmt, colors.Dim, version,
				usageFmt, colors.Dim, nextPrev, dash))
}

// SetupNavigationKeys binds configurable keys for the c9s session (root table, no prefix).
// All bindings use if-shell to only activate inside the c9s session;
// in other sessions the keys pass through normally. Also installs a
// `pane-exited` session hook that returns to the dashboard when any
// non-dashboard window exits — the fallback for auto-return when Claude
// Code hooks aren't installed.
func SetupNavigationKeys(keys NavKeys) error {
	// Match both the bare "c9s" session and any "c9s-<N>" instance so
	// navigation bindings work in every concurrent c9s dashboard.
	sessionCheck := "#{||:#{==:#{session_name},c9s},#{m:c9s-*,#{session_name}}}"

	// pane-exited: when a session window's pane dies (claude exits, shell
	// exits, etc.), jump back to the dashboard. Guarded so the dashboard's
	// own exit doesn't loop. Scoped to the c9s session via `-t SessionName`.
	exec.Command("tmux", "set-hook", "-t", SessionName, "pane-exited",
		fmt.Sprintf("if-shell -F '#{!=:#W,%s}' 'select-window -t %s:%s'",
			DashboardWindow, SessionName, DashboardWindow),
	).Run()

	// Dashboard key → back to dashboard
	if err := exec.Command("tmux", "bind-key",
		"-n", keys.Dashboard,
		"if-shell", "-F", sessionCheck,
		fmt.Sprintf("select-window -t %s:%s ; refresh-client", SessionName, DashboardWindow),
		fmt.Sprintf("send-keys %s", keys.Dashboard),
	).Run(); err != nil {
		return err
	}

	// Next/Prev session keys dispatch into `c9s _cycle next|prev`, which
	// orders windows by hook-fed state priority (waiting > processing >
	// done > unknown) and jumps with wrap-around. Falls back to tmux index
	// order when no state files exist, preserving the legacy behavior.
	//
	// When `os.Executable` fails (e.g. PATH issues in exotic setups), fall
	// back to the plain next-window / previous-window bindings.
	bin, binErr := os.Executable()

	nextCmd := fmt.Sprintf(
		"next-window ; if-shell -F '#{==:#W,%s}' next-window ; refresh-client",
		DashboardWindow,
	)
	prevCmd := fmt.Sprintf(
		"previous-window ; if-shell -F '#{==:#W,%s}' previous-window ; refresh-client",
		DashboardWindow,
	)
	if binErr == nil {
		nextCmd = fmt.Sprintf("run-shell -b %q", bin+" _cycle next")
		prevCmd = fmt.Sprintf("run-shell -b %q", bin+" _cycle prev")
	}

	if err := exec.Command("tmux", "bind-key",
		"-n", keys.NextSession,
		"if-shell", "-F", sessionCheck,
		nextCmd,
		fmt.Sprintf("send-keys %s", keys.NextSession),
	).Run(); err != nil {
		return err
	}

	return exec.Command("tmux", "bind-key",
		"-n", keys.PrevSession,
		"if-shell", "-F", sessionCheck,
		prevCmd,
		fmt.Sprintf("send-keys %s", keys.PrevSession),
	).Run()
}

// CleanupNavigationKeys removes the c9s key bindings and session hooks.
func CleanupNavigationKeys(keys NavKeys) error {
	exec.Command("tmux", "unbind-key", "-n", keys.Dashboard).Run()
	exec.Command("tmux", "unbind-key", "-n", keys.NextSession).Run()
	exec.Command("tmux", "unbind-key", "-n", keys.PrevSession).Run()
	exec.Command("tmux", "set-hook", "-u", "-t", SessionName, "pane-exited").Run()
	return nil
}

// Detach detaches the current client from the c9s tmux session.
// The session and all windows keep running in the background.
func Detach() error {
	return exec.Command("tmux", "detach-client", "-s", SessionName).Run()
}

// KillSession kills the entire c9s tmux session.
// This detaches all clients and destroys all windows.
func KillSession() error {
	return exec.Command("tmux", "kill-session", "-t", SessionName).Run()
}
