// Package installer manages the c9s entries inside ~/.claude/settings.json.
// It installs a statusLine command and a set of lifecycle hooks (SessionStart,
// UserPromptSubmit, Stop, Notification, SessionEnd) so the dashboard can
// observe live session state without scraping pane content.
//
// The installer is merge-aware: it preserves any other statusLine or hooks
// entries the user already has, only touches the entries it owns, and is
// idempotent. Uninstall removes only c9s-owned entries and leaves the rest
// of the file untouched.
package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathOverride lets tests redirect the settings.json location.
var PathOverride string

// HookEvents is the list of Claude Code hook events c9s subscribes to.
var HookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"Stop",
	"Notification",
	"SessionEnd",
	"PreToolUse",
	"PostToolUse",
	"PreCompact",
}

func settingsPath() string {
	if PathOverride != "" {
		return PathOverride
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// SettingsPath returns the absolute path of ~/.claude/settings.json that the
// installer reads and writes.
func SettingsPath() string {
	return settingsPath()
}

// Install merges the c9s lifecycle hooks into ~/.claude/settings.json using
// binPath as the c9s binary. Existing c9s entries are replaced (handles
// binary moves); other user entries are preserved. Idempotent.
//
// c9s does not install a statusLine: the dashboard renders all per-session
// info via tmux's native status bar (status-format), fed from the hook-
// written state files (see internal/sessionstate). Uninstall still cleans
// up any statusLine left from older c9s versions.
func Install(binPath string) error {
	settings, err := readSettings()
	if err != nil {
		return err
	}

	// Remove any stale c9s statusLine left over from older c9s versions.
	if isC9sStatusLine(settings["statusLine"]) {
		delete(settings, "statusLine")
	}

	hooks := asMap(settings["hooks"])
	for _, event := range HookEvents {
		arr := asSlice(hooks[event])
		arr = removeC9sEntries(arr)
		arr = append(arr, map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": fmt.Sprintf("%s _hook %s", binPath, hookSubcommand(event)),
				},
			},
		})
		hooks[event] = arr
	}
	settings["hooks"] = hooks

	return writeSettings(settings)
}

// Uninstall removes every c9s-owned entry from ~/.claude/settings.json. Other
// user entries are left untouched. Idempotent.
func Uninstall() error {
	settings, err := readSettings()
	if err != nil {
		return err
	}

	if isC9sStatusLine(settings["statusLine"]) {
		delete(settings, "statusLine")
	}

	hooks := asMap(settings["hooks"])
	for event, raw := range hooks {
		arr := removeC9sEntries(asSlice(raw))
		if len(arr) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = arr
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	return writeSettings(settings)
}

// IsInstalled returns true if any c9s-owned entry is present in settings.json.
func IsInstalled() bool {
	settings, err := readSettings()
	if err != nil {
		return false
	}
	if isC9sStatusLine(settings["statusLine"]) {
		return true
	}
	hooks := asMap(settings["hooks"])
	for _, raw := range hooks {
		for _, item := range asSlice(raw) {
			if entryHasC9sCommand(item) {
				return true
			}
		}
	}
	return false
}

// hookSubcommand returns the c9s subcommand for a given Claude hook event.
func hookSubcommand(event string) string {
	switch event {
	case "SessionStart":
		return "session-start"
	case "UserPromptSubmit":
		return "user-prompt-submit"
	case "Stop":
		return "stop"
	case "Notification":
		return "notification"
	case "SessionEnd":
		return "session-end"
	case "PreToolUse":
		return "pre-tool-use"
	case "PostToolUse":
		return "post-tool-use"
	case "PreCompact":
		return "pre-compact"
	}
	return strings.ToLower(event)
}

func readSettings() (map[string]any, error) {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsPath(), err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func writeSettings(settings map[string]any) error {
	p := settingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(p), ".settings-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, p)
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// isC9sCommand identifies a command string that invokes the c9s binary with a
// c9s-owned subcommand. Matches by the trailing `c9s _hook <event>` or
// `c9s _statusline` form so the installer can find its entries even if the
// binary is at a custom path.
func isC9sCommand(cmd string) bool {
	if cmd == "" {
		return false
	}
	// Tokenize on space — sufficient because we control how Install writes
	// these strings (binary path then literal subcommand args).
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return false
	}
	if filepath.Base(fields[0]) != "c9s" {
		return false
	}
	switch fields[1] {
	case "_hook", "_statusline":
		return true
	}
	return false
}

func isC9sStatusLine(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	return isC9sCommand(asString(m["command"]))
}

// entryHasC9sCommand returns true if a hooks.<event> entry contains a c9s
// command anywhere inside its nested `hooks` array.
func entryHasC9sCommand(item any) bool {
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	for _, h := range asSlice(m["hooks"]) {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if isC9sCommand(asString(hm["command"])) {
			return true
		}
	}
	return false
}

// removeC9sEntries returns the input slice without any entries that contain a
// c9s command. Entries that mix c9s and non-c9s commands inside their `hooks`
// array have only the c9s commands stripped (preserving user entries).
func removeC9sEntries(arr []any) []any {
	out := arr[:0:0]
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		nested := asSlice(m["hooks"])
		filtered := nested[:0:0]
		for _, h := range nested {
			hm, ok := h.(map[string]any)
			if !ok {
				filtered = append(filtered, h)
				continue
			}
			if isC9sCommand(asString(hm["command"])) {
				continue
			}
			filtered = append(filtered, h)
		}
		if len(filtered) == 0 {
			continue
		}
		m["hooks"] = filtered
		out = append(out, m)
	}
	return out
}
