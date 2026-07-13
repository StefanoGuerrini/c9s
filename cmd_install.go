package main

import (
	"fmt"
	"os"

	"github.com/stefanoguerrini/c9s/internal/installer"
)

// runInstall handles `c9s install`. Resolves the running binary path,
// installs the c9s lifecycle hooks into ~/.claude/settings.json, prints a
// summary. c9s does not install a statusLine — the dashboard renders all
// per-session info via tmux's native status bar.
func runInstall() int {
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "c9s install: cannot resolve binary path: %v\n", err)
		return 1
	}
	if err := installer.Install(bin); err != nil {
		fmt.Fprintf(os.Stderr, "c9s install: %v\n", err)
		return 1
	}
	fmt.Printf("Installed c9s hooks into %s\n", installer.SettingsPath())
	for _, ev := range installer.HookEvents {
		fmt.Printf("  hook %s\n", ev)
	}
	fmt.Println("Run `c9s uninstall` to remove.")
	return 0
}

// runUninstall handles `c9s uninstall`. Removes all c9s-owned entries from
// ~/.claude/settings.json, leaves other user entries intact.
func runUninstall() int {
	if err := installer.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "c9s uninstall: %v\n", err)
		return 1
	}
	fmt.Printf("Removed c9s entries from %s\n", installer.SettingsPath())
	return 0
}
