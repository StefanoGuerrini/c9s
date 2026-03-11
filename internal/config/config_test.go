package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Theme != "default" {
		t.Errorf("default theme = %q, want default", cfg.Theme)
	}
	if cfg.Keys.Dashboard != "C-d" {
		t.Errorf("default dashboard key = %q, want C-d", cfg.Keys.Dashboard)
	}
	if cfg.Keys.NextSession != "C-n" {
		t.Errorf("default next key = %q, want C-n", cfg.Keys.NextSession)
	}
	if cfg.Colors.Active != "14" {
		t.Errorf("default active color = %q, want 14", cfg.Colors.Active)
	}
	if cfg.Colors.StatusBg != "#1b1b2f" {
		t.Errorf("default status bg = %q, want #1b1b2f", cfg.Colors.StatusBg)
	}
}

func TestEffectiveColors(t *testing.T) {
	cfg := Default()
	// Default theme → returns built-in colors.
	ec := cfg.EffectiveColors()
	if ec.Active != "14" {
		t.Errorf("default effective active = %q, want 14", ec.Active)
	}

	// Custom theme → returns user's colors.
	cfg.Theme = "custom"
	cfg.Colors.Active = "#ff0000"
	ec = cfg.EffectiveColors()
	if ec.Active != "#ff0000" {
		t.Errorf("custom effective active = %q, want #ff0000", ec.Active)
	}
}

func TestEditableFields(t *testing.T) {
	fields := EditableFields()
	if len(fields) < 5 {
		t.Fatalf("expected at least 5 fields, got %d", len(fields))
	}

	// First field should be refresh interval (General section).
	if fields[0].Key != "refresh_seconds" {
		t.Errorf("field[0].Key = %q, want refresh_seconds", fields[0].Key)
	}
	if fields[0].Section != "General" {
		t.Errorf("field[0].Section = %q, want General", fields[0].Section)
	}

	// Second field should be scroll speed (General section).
	if fields[1].Key != "scroll_speed" {
		t.Errorf("field[1].Key = %q, want scroll_speed", fields[1].Key)
	}

	// Next 3 should be shortcuts.
	for i, key := range []string{"dashboard", "next_session", "prev_session"} {
		if fields[i+2].Key != key {
			t.Errorf("field[%d].Key = %q, want %q", i+2, fields[i+2].Key, key)
		}
		if fields[i+2].Section != "Shortcuts" {
			t.Errorf("field[%d].Section = %q, want Shortcuts", i+2, fields[i+2].Section)
		}
	}

	// 6th should be theme toggle.
	if fields[5].Key != "theme" {
		t.Errorf("field[5].Key = %q, want theme", fields[5].Key)
	}

	// Test Get/Set roundtrip on refresh field.
	cfg := Default()
	val := fields[0].Get(cfg)
	if val != "3" {
		t.Errorf("Get refresh = %q, want 3", val)
	}
	fields[0].Set(&cfg, "5")
	if cfg.RefreshSeconds != 5 {
		t.Errorf("Set refresh: got %d, want 5", cfg.RefreshSeconds)
	}

	// Invalid values should be ignored.
	fields[0].Set(&cfg, "abc")
	if cfg.RefreshSeconds != 5 {
		t.Errorf("invalid Set should not change value, got %d", cfg.RefreshSeconds)
	}
	fields[0].Set(&cfg, "0")
	if cfg.RefreshSeconds != 5 {
		t.Errorf("out of range Set should not change value, got %d", cfg.RefreshSeconds)
	}
}

func TestLoadMissingFile(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "nonexistent.json")
	t.Cleanup(func() { PathOverride = "" })

	cfg := Load()
	def := Default()
	if cfg.Keys.Dashboard != def.Keys.Dashboard {
		t.Error("missing file should return defaults")
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	// Only override one key — rest should keep defaults.
	os.WriteFile(path, []byte(`{"keys":{"dashboard":"C-b"}}`), 0644)

	cfg := Load()
	if cfg.Keys.Dashboard != "C-b" {
		t.Errorf("dashboard = %q, want C-b", cfg.Keys.Dashboard)
	}
	if cfg.Keys.NextSession != "C-n" {
		t.Errorf("next_session should keep default C-n, got %q", cfg.Keys.NextSession)
	}
	if cfg.Colors.Active != "14" {
		t.Errorf("colors should keep defaults, active = %q", cfg.Colors.Active)
	}
}

func TestLoadColorOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	os.WriteFile(path, []byte(`{"colors":{"active":"#ff0000","status_bg":"#000000"}}`), 0644)

	cfg := Load()
	if cfg.Colors.Active != "#ff0000" {
		t.Errorf("active = %q, want #ff0000", cfg.Colors.Active)
	}
	if cfg.Colors.StatusBg != "#000000" {
		t.Errorf("status_bg = %q, want #000000", cfg.Colors.StatusBg)
	}
	// Non-overridden should keep defaults.
	if cfg.Colors.Idle != "13" {
		t.Errorf("idle should keep default, got %q", cfg.Colors.Idle)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	cfg := Default()
	cfg.Keys.Dashboard = "C-x"
	cfg.Colors.StatusAccent = "#ffffff"

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := Load()
	if loaded.Keys.Dashboard != "C-x" {
		t.Errorf("dashboard = %q, want C-x", loaded.Keys.Dashboard)
	}
	if loaded.Colors.StatusAccent != "#ffffff" {
		t.Errorf("accent = %q, want #ffffff", loaded.Colors.StatusAccent)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	os.WriteFile(path, []byte(`{invalid json`), 0644)

	cfg := Load()
	def := Default()
	if cfg.Keys.Dashboard != def.Keys.Dashboard {
		t.Error("invalid JSON should return defaults")
	}
}

func TestEnsureExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	if err := EnsureExists(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("config file should exist after EnsureExists")
	}

	// Calling again should not error (file already exists).
	if err := EnsureExists(); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestLoadIfChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	PathOverride = path
	t.Cleanup(func() {
		PathOverride = ""
		// Reset cache.
		cachedConfig.mu.Lock()
		cachedConfig.valid = false
		cachedConfig.mu.Unlock()
	})

	// No file — returns defaults, changed=true.
	cfg1, changed1 := LoadIfChanged()
	if !changed1 {
		t.Error("first call should report changed")
	}
	if cfg1.Keys.Dashboard != "C-d" {
		t.Error("should return defaults")
	}

	// Same call again, no file — changed=false.
	_, changed2 := LoadIfChanged()
	if changed2 {
		t.Error("second call without file change should not report changed")
	}

	// Write config — should detect change.
	os.WriteFile(path, []byte(`{"keys":{"dashboard":"C-x"}}`), 0644)
	cfg3, changed3 := LoadIfChanged()
	if !changed3 {
		t.Error("should detect file creation")
	}
	if cfg3.Keys.Dashboard != "C-x" {
		t.Errorf("dashboard = %q, want C-x", cfg3.Keys.Dashboard)
	}
}
