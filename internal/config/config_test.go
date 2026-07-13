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
	if cfg.StatusModel != "on" {
		t.Errorf("default status model = %q, want on", cfg.StatusModel)
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
	if len(fields) < 7 {
		t.Fatalf("expected at least 7 fields, got %d", len(fields))
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

	// Third field should be work directory (General section).
	if fields[2].Key != "work_dir" {
		t.Errorf("field[2].Key = %q, want work_dir", fields[2].Key)
	}

	// Fourth should be keep alive (General section).
	if fields[3].Key != "keep_alive" {
		t.Errorf("field[3].Key = %q, want keep_alive", fields[3].Key)
	}

	// Fifth is hide archived (General section).
	if fields[4].Key != "hide_archived" {
		t.Errorf("field[4].Key = %q, want hide_archived", fields[4].Key)
	}
	if len(fields[4].Options) != 2 {
		t.Errorf("hide_archived Options = %v, want 2 options", fields[4].Options)
	}

	// Sixth is status usage (General section).
	if fields[5].Key != "status_usage" {
		t.Errorf("field[5].Key = %q, want status_usage", fields[5].Key)
	}
	if len(fields[5].Options) != 4 {
		t.Errorf("status_usage Options = %v, want 4 options", fields[5].Options)
	}

	// Seventh is status model (General section).
	if fields[6].Key != "status_model" {
		t.Errorf("field[6].Key = %q, want status_model", fields[6].Key)
	}
	if len(fields[6].Options) != 2 {
		t.Errorf("status_model Options = %v, want 2 options", fields[6].Options)
	}

	// Eighth is desktop notifications (General section).
	if fields[7].Key != "notifications" {
		t.Errorf("field[7].Key = %q, want notifications", fields[7].Key)
	}
	if len(fields[7].Options) != 2 {
		t.Errorf("notifications Options = %v, want 2 options", fields[7].Options)
	}

	// 9-14 are phone notification fields (Phone notifications section).
	for i, key := range []string{"notify_push", "notify_push_url", "notify_push_topic", "notify_push_token", "notify_push_user", "notify_push_pass"} {
		if fields[i+8].Key != key {
			t.Errorf("field[%d].Key = %q, want %q", i+8, fields[i+8].Key, key)
		}
		if fields[i+8].Section != "Phone notifications" {
			t.Errorf("field[%d].Section = %q, want Phone notifications", i+8, fields[i+8].Section)
		}
	}

	// 15-16 are usage history fields.
	if fields[14].Key != "usage_history" {
		t.Errorf("field[14].Key = %q, want usage_history", fields[14].Key)
	}
	if fields[14].Section != "Usage history" {
		t.Errorf("field[14].Section = %q, want Usage history", fields[14].Section)
	}
	if fields[15].Key != "reset_history" {
		t.Errorf("field[15].Key = %q, want reset_history", fields[15].Key)
	}
	if !fields[15].Action {
		t.Error("reset_history should be an Action field")
	}

	// 17 is the single worktree field (Worktrees section).
	if fields[16].Key != "worktrees" {
		t.Errorf("field[16].Key = %q, want worktrees", fields[16].Key)
	}
	if fields[16].Section != "Worktrees" {
		t.Errorf("field[16].Section = %q, want Worktrees", fields[16].Section)
	}
	if len(fields[16].Options) != 2 {
		t.Errorf("worktrees Options = %v, want 2 options (off/on)", fields[16].Options)
	}

	// Next 3 should be shortcuts.
	for i, key := range []string{"dashboard", "next_session", "prev_session"} {
		if fields[i+17].Key != key {
			t.Errorf("field[%d].Key = %q, want %q", i+17, fields[i+17].Key, key)
		}
		if fields[i+17].Section != "Shortcuts" {
			t.Errorf("field[%d].Section = %q, want Shortcuts", i+17, fields[i+17].Section)
		}
	}

	// Theme toggle follows shortcuts.
	if fields[20].Key != "theme" {
		t.Errorf("field[20].Key = %q, want theme", fields[20].Key)
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

func TestWorktreeConfigFields(t *testing.T) {
	cfg := Default()
	if cfg.Worktrees != "off" {
		t.Errorf("default Worktrees = %q, want off", cfg.Worktrees)
	}

	// Test worktrees field validation via EditableFields.
	fields := EditableFields()
	var wtField Field
	for _, f := range fields {
		if f.Key == "worktrees" {
			wtField = f
		}
	}

	// Valid values.
	wtField.Set(&cfg, "on")
	if cfg.Worktrees != "on" {
		t.Errorf("Set worktrees on: got %q", cfg.Worktrees)
	}
	wtField.Set(&cfg, "off")
	if cfg.Worktrees != "off" {
		t.Errorf("Set worktrees off: got %q", cfg.Worktrees)
	}

	// Legacy values (auto/always) are no longer accepted via the editor;
	// load-time migration handles them. See TestLoadMigratesLegacyWorktrees.
	wtField.Set(&cfg, "auto")
	if cfg.Worktrees != "off" {
		t.Errorf("auto Set should be ignored after collapse, got %q", cfg.Worktrees)
	}

	// Invalid value should be ignored.
	wtField.Set(&cfg, "invalid")
	if cfg.Worktrees != "off" {
		t.Errorf("invalid Set should not change, got %q", cfg.Worktrees)
	}
}

func TestLoadMigratesLegacyWorktrees(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	PathOverride = path
	t.Cleanup(func() { PathOverride = "" })

	// Legacy "auto" → "on".
	os.WriteFile(path, []byte(`{"worktrees":"auto"}`), 0644)
	if cfg := Load(); cfg.Worktrees != "on" {
		t.Errorf("legacy auto: got %q, want on", cfg.Worktrees)
	}

	// Legacy "always" → "on".
	os.WriteFile(path, []byte(`{"worktrees":"always"}`), 0644)
	if cfg := Load(); cfg.Worktrees != "on" {
		t.Errorf("legacy always: got %q, want on", cfg.Worktrees)
	}

	// Modern "off"/"on" pass through unchanged.
	os.WriteFile(path, []byte(`{"worktrees":"off"}`), 0644)
	if cfg := Load(); cfg.Worktrees != "off" {
		t.Errorf("off: got %q, want off", cfg.Worktrees)
	}
	os.WriteFile(path, []byte(`{"worktrees":"on"}`), 0644)
	if cfg := Load(); cfg.Worktrees != "on" {
		t.Errorf("on: got %q, want on", cfg.Worktrees)
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
