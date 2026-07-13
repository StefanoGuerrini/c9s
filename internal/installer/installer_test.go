package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func withTempSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	prev := PathOverride
	PathOverride = p
	t.Cleanup(func() { PathOverride = prev })
	return p
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return out
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallOnMissingFile(t *testing.T) {
	p := withTempSettings(t)
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := readJSON(t, p)
	// c9s does not install a statusLine — the dashboard renders all
	// per-session info via tmux's native status bar.
	if _, has := got["statusLine"]; has {
		t.Errorf("statusLine should not be installed: %v", got["statusLine"])
	}
	hooks, ok := got["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing")
	}
	for _, ev := range HookEvents {
		if _, ok := hooks[ev]; !ok {
			t.Errorf("event %s missing", ev)
		}
	}
}

func TestInstallPreservesUserStatusLine(t *testing.T) {
	p := withTempSettings(t)
	original := map[string]any{"type": "command", "command": "/other/statusline"}
	writeJSON(t, p, map[string]any{"statusLine": original})
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := readJSON(t, p)
	sl, _ := got["statusLine"].(map[string]any)
	if sl["command"] != "/other/statusline" {
		t.Errorf("user statusLine should be preserved, got %v", sl["command"])
	}
}

func TestInstallRemovesStaleC9sStatusLine(t *testing.T) {
	p := withTempSettings(t)
	// An older c9s version that wrote a statusLine: install should clean it up.
	writeJSON(t, p, map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "/old/c9s _statusline"},
	})
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := readJSON(t, p)
	if _, has := got["statusLine"]; has {
		t.Errorf("stale c9s statusLine should be removed: %v", got["statusLine"])
	}
}

func TestInstallPreservesUserHooks(t *testing.T) {
	p := withTempSettings(t)
	writeJSON(t, p, map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/notify-me"},
					},
				},
			},
		},
	})
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := readJSON(t, p)
	hooks := got["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop should have 2 entries (user + c9s), got %d: %v", len(stop), stop)
	}
	first := stop[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if first["command"] != "/usr/local/bin/notify-me" {
		t.Errorf("user entry not preserved: %v", first)
	}
}

func TestInstallIdempotent(t *testing.T) {
	p := withTempSettings(t)
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatal(err)
	}
	first := readJSON(t, p)
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatal(err)
	}
	second := readJSON(t, p)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Install not idempotent:\nfirst:  %v\nsecond: %v", first, second)
	}
}

func TestInstallReplacesOldC9sEntries(t *testing.T) {
	p := withTempSettings(t)
	if err := Install("/old/path/c9s"); err != nil {
		t.Fatal(err)
	}
	if err := Install("/new/path/c9s"); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, p)
	hooks := got["hooks"].(map[string]any)
	for _, ev := range HookEvents {
		arr := hooks[ev].([]any)
		c9sCount := 0
		for _, item := range arr {
			if entryHasC9sCommand(item) {
				c9sCount++
				inner := item.(map[string]any)["hooks"].([]any)[0].(map[string]any)
				if got := inner["command"].(string); got[:14] != "/new/path/c9s " {
					t.Errorf("event %s: old path not replaced: %q", ev, got)
				}
			}
		}
		if c9sCount != 1 {
			t.Errorf("event %s: expected 1 c9s entry, got %d", ev, c9sCount)
		}
	}
}

func TestUninstallRestoresOriginal(t *testing.T) {
	p := withTempSettings(t)
	original := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/notify-me"},
					},
				},
			},
		},
	}
	writeJSON(t, p, original)
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, p)
	if !reflect.DeepEqual(got, original) {
		t.Errorf("Uninstall did not restore original\nwant: %v\ngot:  %v", original, got)
	}
}

func TestUninstallOnVirginFile(t *testing.T) {
	p := withTempSettings(t)
	writeJSON(t, p, map[string]any{"theme": "dark"})
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, p)
	if got["theme"] != "dark" {
		t.Errorf("user content lost: %v", got)
	}
	if _, has := got["hooks"]; has {
		t.Errorf("hooks key should be absent: %v", got["hooks"])
	}
}

func TestUninstallIdempotent(t *testing.T) {
	p := withTempSettings(t)
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	first := readJSON(t, p)
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	second := readJSON(t, p)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Uninstall not idempotent")
	}
}

func TestIsInstalled(t *testing.T) {
	withTempSettings(t)
	if IsInstalled() {
		t.Error("IsInstalled should be false on missing file")
	}
	if err := Install("/opt/c9s/bin/c9s"); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled() {
		t.Error("IsInstalled should be true after Install")
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if IsInstalled() {
		t.Error("IsInstalled should be false after Uninstall")
	}
}

func TestIsC9sCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"/usr/local/bin/c9s _hook stop", true},
		{"/opt/c9s/bin/c9s _statusline", true},
		{"c9s _hook session-start", true},
		{"/path/to/other-tool _hook stop", false},
		{"/path/to/c9s --version", false},
		{"", false},
		{"/path/to/c9s", false},
	}
	for _, c := range cases {
		if got := isC9sCommand(c.cmd); got != c.want {
			t.Errorf("isC9sCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestUninstallStripsC9sFromMixedEntry(t *testing.T) {
	p := withTempSettings(t)
	writeJSON(t, p, map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/path/c9s _hook stop"},
						map[string]any{"type": "command", "command": "/usr/local/bin/notify"},
					},
				},
			},
		},
	})
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, p)
	stop := got["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(stop))
	}
	inner := stop[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 1 {
		t.Fatalf("expected 1 inner hook, got %d", len(inner))
	}
	if inner[0].(map[string]any)["command"] != "/usr/local/bin/notify" {
		t.Errorf("user hook lost: %v", inner[0])
	}
}
