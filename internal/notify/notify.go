// Package notify sends native desktop notifications across macOS, Linux, and
// Windows. Failures are silent — a missing notification daemon should never
// break a hook handler.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Notify dispatches a desktop notification with the given title and body. On
// macOS it shells out to osascript; on Linux it uses notify-send if
// available; on Windows it tries BurntToast via PowerShell. Returns silently
// when no backend is available.
func Notify(title, body string) {
	if title == "" && body == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		notifyMacOS(title, body)
	case "linux":
		notifyLinux(title, body)
	case "windows":
		notifyWindows(title, body)
	}
}

func notifyMacOS(title, body string) {
	// Prefer terminal-notifier when installed: it registers as its own
	// notification source, so users don't have to enable "Script Editor"
	// in System Settings, and it works under modern macOS sandboxing.
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		_ = exec.Command(path,
			"-title", title,
			"-message", body,
			"-sender", "com.apple.Terminal", // group under the terminal app
		).Run()
		return
	}
	// Fallback: osascript. Works only when "Script Editor" has
	// notifications enabled in System Settings → Notifications.
	if _, err := exec.LookPath("osascript"); err != nil {
		return
	}
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	_ = exec.Command("osascript", "-e", script).Run()
}

func notifyLinux(title, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	_ = exec.Command("notify-send", title, body).Run()
}

func notifyWindows(title, body string) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return
	}
	// BurntToast is the most common toast-notification module on Windows.
	// If it isn't installed the command fails silently — that's the contract.
	ps := `if (Get-Module -ListAvailable -Name BurntToast) { Import-Module BurntToast; New-BurntToastNotification -Text '` +
		escapeSingleQuote(title) + `','` + escapeSingleQuote(body) + `' }`
	_ = exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Run()
}

// escapeAppleScript quotes a string for safe interpolation into an
// AppleScript string literal. Backslashes and double-quotes are escaped.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}
