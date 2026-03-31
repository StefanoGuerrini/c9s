package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stefanoguerrini/c9s/internal/claude"
	"github.com/stefanoguerrini/c9s/internal/git"
	"github.com/stefanoguerrini/c9s/internal/tmux"
)

func TestReconcileWindows_NewSession(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })
	tmpDir := t.TempDir()

	// New session tracked with tmpKey should be reconciled to real sessionID.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"new-123456": {
				windowID:   "@1",
				sessionID:  "",
				project:    "/home/user/project",
				paneStatus: tmux.PaneWaiting,
			},
		},
	}

	sessions := []claude.SessionInfo{
		{
			SessionID:   "abc-def-123",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   time.Now(),
		},
	}

	m.reconcileWindows(sessions)

	if _, ok := m.managedWindows["new-123456"]; ok {
		t.Error("old tmpKey should be deleted")
	}
	mw, ok := m.managedWindows["abc-def-123"]
	if !ok {
		t.Fatal("expected entry under real sessionID")
	}
	if mw.windowID != "@1" {
		t.Errorf("windowID = %q, want @1", mw.windowID)
	}
	if mw.sessionID != "abc-def-123" {
		t.Errorf("sessionID = %q, want abc-def-123", mw.sessionID)
	}
}

func TestReconcileWindows_NewSessionSkipsAlreadyTracked(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })

	// A new-* window should NOT steal a session that's already tracked by another window.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"existing-session": {
				windowID:  "@1",
				sessionID: "existing-session",
				project:   "/home/user/project",
			},
			"new-999": {
				windowID:  "@2",
				sessionID: "",
				project:   "/home/user/project",
			},
		},
	}

	sessions := []claude.SessionInfo{
		{
			SessionID:   "existing-session",
			ProjectPath: "/home/user/project",
			FileMtime:   time.Now(), // recent and active
		},
	}

	m.reconcileWindows(sessions)

	// @1 should still track existing-session.
	if mw, ok := m.managedWindows["existing-session"]; !ok || mw.windowID != "@1" {
		t.Error("existing-session should still own window @1")
	}
	// new-999 should NOT have been re-keyed to existing-session.
	if _, ok := m.managedWindows["new-999"]; !ok {
		t.Error("new-999 should still exist (no valid target to re-key to)")
	}
}

func TestReconcileWindows_Fork(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })
	tmpDir := t.TempDir()

	// Write a JSONL for forked-session-id with forkedFrom pointing to old-session-id.
	// This makes GetSupersededSessions() return old-session-id as superseded.
	forkedJSONL := `{"forkedFrom":{"sessionId":"old-session-id"}}` + "\n"
	os.WriteFile(filepath.Join(tmpDir, "forked-session-id.jsonl"), []byte(forkedJSONL), 0644)
	// Also write a minimal JSONL for old-session-id so it's in cache.
	os.WriteFile(filepath.Join(tmpDir, "old-session-id.jsonl"), []byte(""), 0644)

	// After fork: old session superseded, new forked session is active.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"old-session-id": {
				windowID:   "@2",
				sessionID:  "old-session-id",
				project:    "/home/user/project",
				paneStatus: tmux.PaneProcessing,
			},
		},
	}

	sessions := []claude.SessionInfo{
		{
			SessionID:   "old-session-id",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   time.Now().Add(-5 * time.Minute), // stale
		},
		{
			SessionID:   "forked-session-id",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   time.Now(), // recently active
		},
	}

	// Populate token cache so GetSupersededSessions() works.
	for i := range sessions {
		claude.ReadTokenUsageForTest(&sessions[i])
	}

	m.reconcileWindows(sessions)

	if _, ok := m.managedWindows["old-session-id"]; ok {
		t.Error("old sessionID key should be deleted")
	}
	mw, ok := m.managedWindows["forked-session-id"]
	if !ok {
		t.Fatal("expected entry under forked sessionID")
	}
	if mw.sessionID != "forked-session-id" {
		t.Errorf("sessionID = %q, want forked-session-id", mw.sessionID)
	}
	if !m.replacedSessions["old-session-id"] {
		t.Error("old-session-id should be in replacedSessions")
	}
}

func TestReconcileWindows_ActiveSessionSkipped(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })

	// If current sessionID is valid and recently active, don't reconcile.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"active-id": {
				windowID:  "@4",
				sessionID: "active-id",
				project:   "/home/user/project",
			},
		},
	}

	sessions := []claude.SessionInfo{
		{
			SessionID:   "active-id",
			ProjectPath: "/home/user/project",
			FileMtime:   time.Now(),
		},
		{
			SessionID:   "other-id",
			ProjectPath: "/home/user/project",
			FileMtime:   time.Now().Add(-10 * time.Second),
		},
	}

	m.reconcileWindows(sessions)

	// Should remain under active-id since it's not stale.
	if _, ok := m.managedWindows["active-id"]; !ok {
		t.Error("active session should remain in map")
	}
}

func TestReconcileWindows_ConcurrentSessionsNotStolen(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })
	tmpDir := t.TempDir()

	// Two concurrent sessions in the same project, each tracked by its own window.
	// Neither should steal the other's window — they're independent.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"session-a": {
				windowID:  "@1",
				sessionID: "session-a",
				project:   "/home/user/project",
			},
			"session-b": {
				windowID:  "@2",
				sessionID: "session-b",
				project:   "/home/user/project",
			},
		},
	}

	now := time.Now()
	sessions := []claude.SessionInfo{
		{
			SessionID:   "session-a",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   now.Add(-30 * time.Second),
		},
		{
			SessionID:   "session-b",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   now.Add(-2 * time.Second), // more recent
		},
	}

	m.reconcileWindows(sessions)

	// Both windows must remain under their own session IDs.
	if mw, ok := m.managedWindows["session-a"]; !ok || mw.windowID != "@1" {
		t.Error("session-a should still own window @1")
	}
	if mw, ok := m.managedWindows["session-b"]; !ok || mw.windowID != "@2" {
		t.Error("session-b should still own window @2")
	}
}

func TestReconcileWindows_SupersededRekeys(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })
	tmpDir := t.TempDir()

	// When a session is confirmed superseded (forkedFrom), the window should
	// be re-keyed to the most recent successor.
	forkedJSONL := `{"forkedFrom":{"sessionId":"old-id"}}` + "\n"
	os.WriteFile(filepath.Join(tmpDir, "new-id.jsonl"), []byte(forkedJSONL), 0644)
	os.WriteFile(filepath.Join(tmpDir, "old-id.jsonl"), []byte(""), 0644)

	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"old-id": {
				windowID:  "@5",
				sessionID: "old-id",
				project:   "/home/user/project",
			},
		},
	}

	now := time.Now()
	sessions := []claude.SessionInfo{
		{
			SessionID:   "old-id",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   now.Add(-5 * time.Minute),
		},
		{
			SessionID:   "new-id",
			ProjectPath: "/home/user/project",
			Dir:         tmpDir,
			FileMtime:   now.Add(-2 * time.Second),
		},
	}

	for i := range sessions {
		claude.ReadTokenUsageForTest(&sessions[i])
	}

	m.reconcileWindows(sessions)

	if _, ok := m.managedWindows["old-id"]; ok {
		t.Error("superseded old-id should be removed")
	}
	if _, ok := m.managedWindows["new-id"]; !ok {
		t.Error("window should be re-keyed to new-id")
	}
}

func TestReconcileWindows_NoNewSession(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })

	// If no recent session in the project, entry should remain unchanged.
	m := &model{
		replacedSessions: make(map[string]bool),
		managedWindows: map[string]managedWindow{
			"stale-id": {
				windowID:  "@6",
				sessionID: "stale-id",
				project:   "/home/user/project",
			},
		},
	}

	sessions := []claude.SessionInfo{
		{
			SessionID:   "stale-id",
			ProjectPath: "/home/user/project",
			FileMtime:   time.Now().Add(-5 * time.Minute),
		},
	}

	m.reconcileWindows(sessions)

	if _, ok := m.managedWindows["stale-id"]; !ok {
		t.Error("entry should remain when no new session found")
	}
}

func TestUsageBar(t *testing.T) {
	tests := []struct {
		pct   float64
		width int
		want  string
	}{
		{0, 12, "░░░░░░░░░░░░"},
		{50, 12, "██████░░░░░░"},
		{100, 12, "████████████"},
		{25, 12, "███░░░░░░░░░"},
		{-5, 12, "░░░░░░░░░░░░"},
		{150, 12, "████████████"},
	}
	for _, tt := range tests {
		got := usageBar(tt.pct, tt.width)
		if got != tt.want {
			t.Errorf("usageBar(%.0f, %d) = %q, want %q", tt.pct, tt.width, got, tt.want)
		}
	}
}

func TestAggregateUsageRows_Daily(t *testing.T) {
	now := time.Now()
	m := &model{usageViewMode: 0, usageDayRange: 7}

	points := []claude.UsageDataPoint{
		{Time: now.Add(-2 * 24 * time.Hour), FiveHour: 40, SevenDay: 10, Tokens: 100000},
		{Time: now.Add(-2*24*time.Hour + time.Hour), FiveHour: 60, SevenDay: 12, Tokens: 150000},
		{Time: now.Add(-1 * 24 * time.Hour), FiveHour: 80, SevenDay: 15, Tokens: 200000},
		{Time: now, FiveHour: 30, SevenDay: 20, Tokens: 300000},
	}

	rows := m.aggregateUsageRows(points)
	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	// Most recent first.
	if rows[0].fiveHour != 30 {
		t.Errorf("first row 5h peak = %v, want 30", rows[0].fiveHour)
	}
	// Second day should have peak of 60 (two samples: 40 and 60).
	found := false
	for _, r := range rows {
		if r.fiveHour == 60 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a row with peak 5h = 60")
	}
}

func TestAggregateUsageRows_Weekly(t *testing.T) {
	now := time.Now()
	m := &model{usageViewMode: 1}

	points := []claude.UsageDataPoint{
		{Time: now.Add(-3 * 24 * time.Hour), FiveHour: 50, SevenDay: 10, Tokens: 100000},
		{Time: now, FiveHour: 70, SevenDay: 20, Tokens: 200000},
	}

	rows := m.aggregateUsageRows(points)
	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
}

func TestAggregateUsageRows_ModelDeltas(t *testing.T) {
	now := time.Now()
	m := &model{usageViewMode: 0, usageDayRange: 7}

	// Day 1: opus goes from 1000 → 1500 (+500), sonnet from 200 → 400 (+200)
	// Day 2: opus goes from 1500 → 1600 (+100), sonnet stays at 400 (+0)
	points := []claude.UsageDataPoint{
		{
			Time: now.Add(-1*24*time.Hour - time.Hour), FiveHour: 40, Tokens: 1200,
			Models: map[string]int{"opus": 1000, "sonnet": 200},
		},
		{
			Time: now.Add(-1 * 24 * time.Hour), FiveHour: 50, Tokens: 1900,
			Models: map[string]int{"opus": 1500, "sonnet": 400},
		},
		{
			Time: now, FiveHour: 30, Tokens: 2000,
			Models: map[string]int{"opus": 1600, "sonnet": 400},
		},
	}

	rows := m.aggregateUsageRows(points)
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(rows))
	}

	// rows[0] = today (most recent first): only opus increased by 100, sonnet unchanged
	todayModels := rows[0].models
	if todayModels != nil {
		// sonnet had no delta, should not appear (or be 0%)
		if pct, ok := todayModels["sonnet"]; ok && pct > 0 {
			t.Errorf("today: sonnet should have 0 delta, got %.0f%%", pct)
		}
		if pct, ok := todayModels["opus"]; ok && pct < 99 {
			t.Errorf("today: opus should be ~100%%, got %.0f%%", pct)
		}
	}

	// rows[1] = yesterday: opus +500, sonnet +200 → opus ~71%, sonnet ~29%
	yesterdayModels := rows[1].models
	if yesterdayModels == nil {
		t.Fatal("yesterday: expected model data")
	}
	opusPct := yesterdayModels["opus"]
	sonnetPct := yesterdayModels["sonnet"]
	if opusPct < 65 || opusPct > 77 {
		t.Errorf("yesterday: opus = %.0f%%, want ~71%%", opusPct)
	}
	if sonnetPct < 23 || sonnetPct > 35 {
		t.Errorf("yesterday: sonnet = %.0f%%, want ~29%%", sonnetPct)
	}
}

func TestFmtResetTime(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt string
		want     string
	}{
		{"empty", "", ""},
		{"invalid", "not-a-date", ""},
		{"past", time.Now().Add(-1 * time.Hour).Format(time.RFC3339), ""},
		{"2h13m", time.Now().Add(2*time.Hour + 13*time.Minute + 30*time.Second).Format(time.RFC3339), "resets 2h13m"},
		{"45m", time.Now().Add(45*time.Minute + 20*time.Second).Format(time.RFC3339), "resets 45m"},
		{"5h00m", time.Now().Add(5*time.Hour + 10*time.Second).Format(time.RFC3339), "resets 5h00m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmtResetTime(tt.resetsAt)
			if got != tt.want {
				t.Errorf("fmtResetTime(%q) = %q, want %q", tt.resetsAt, got, tt.want)
			}
		})
	}
}

func TestSelectedSessionPreservedOnReorder(t *testing.T) {
	// Simulate: cursor at session B (index 1), then sessions reorder so B moves to index 2.
	now := time.Now()
	m := &model{
		sessions: []claude.SessionInfo{
			{SessionID: "aaa", Modified: now.Add(-1 * time.Hour)},
			{SessionID: "bbb", Modified: now.Add(-2 * time.Hour)},
			{SessionID: "ccc", Modified: now.Add(-3 * time.Hour)},
		},
		cursor:            1, // pointing at "bbb"
		replacedSessions:  make(map[string]bool),
		managedWindows:    make(map[string]managedWindow),
		expandedWorktrees: make(map[int]bool),
		worktreeCache:     make(map[string][]git.Worktree),
		height:            40,
		width:             120,
	}

	// Verify cursor is on "bbb".
	items := m.items()
	if s := m.selectedSession(items); s == nil || s.SessionID != "bbb" {
		t.Fatalf("before reorder: selected = %v, want bbb", s)
	}

	// Simulate reorder: "ccc" becomes most recent, pushing "bbb" to index 2.
	selectedID := m.selectedSession(m.items()).SessionID
	m.sessions = []claude.SessionInfo{
		{SessionID: "ccc", Modified: now},
		{SessionID: "aaa", Modified: now.Add(-1 * time.Hour)},
		{SessionID: "bbb", Modified: now.Add(-2 * time.Hour)},
	}

	// Restore cursor to "bbb".
	for i, item := range m.items() {
		if !item.isHeader && !item.isWorktreeRow && item.session.SessionID == selectedID {
			m.cursor = i
			break
		}
	}

	items = m.items()
	if s := m.selectedSession(items); s == nil || s.SessionID != "bbb" {
		t.Errorf("after reorder: selected = %v, want bbb", s)
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
}

func TestStartProjectPickerOrdering(t *testing.T) {
	now := time.Now()
	tmpDir := t.TempDir()
	// Create subdirectories.
	os.Mkdir(filepath.Join(tmpDir, "project-a"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "project-b"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "project-c"), 0755)

	// Save and restore config.
	oldWorkDir := cfg.WorkDir
	cfg.WorkDir = tmpDir
	defer func() { cfg.WorkDir = oldWorkDir }()

	m := &model{
		sessions: []claude.SessionInfo{
			{ProjectPath: filepath.Join(tmpDir, "project-a"), Modified: now.Add(-2 * time.Hour)},
			{ProjectPath: filepath.Join(tmpDir, "project-b"), Modified: now.Add(-10 * time.Minute)},
			// project-c has no sessions
		},
		managedWindows:    make(map[string]managedWindow),
		replacedSessions:  make(map[string]bool),
		expandedWorktrees: make(map[int]bool),
		worktreeCache:     make(map[string][]git.Worktree),
	}

	result, _ := m.startProjectPicker(nil, false)
	rm := result.(model)

	if !rm.pickingProject {
		t.Fatal("expected pickingProject to be true")
	}
	// [0] = root, then sorted by last used: project-b (10m), project-a (2h), project-c (none)
	if len(rm.projectDirs) != 4 {
		t.Fatalf("expected 4 dirs, got %d: %v", len(rm.projectDirs), rm.projectDirs)
	}
	if rm.projectDirs[0] != tmpDir {
		t.Errorf("first entry should be root, got %s", rm.projectDirs[0])
	}
	if filepath.Base(rm.projectDirs[1]) != "project-b" {
		t.Errorf("second should be project-b (most recent), got %s", filepath.Base(rm.projectDirs[1]))
	}
	if filepath.Base(rm.projectDirs[2]) != "project-a" {
		t.Errorf("third should be project-a, got %s", filepath.Base(rm.projectDirs[2]))
	}
	if filepath.Base(rm.projectDirs[3]) != "project-c" {
		t.Errorf("fourth should be project-c (no sessions), got %s", filepath.Base(rm.projectDirs[3]))
	}
}

func TestStartProjectPickerNoWorkDir(t *testing.T) {
	tmux.DryRun = true
	t.Cleanup(func() { tmux.DryRun = false })

	oldWorkDir := cfg.WorkDir
	cfg.WorkDir = ""
	defer func() { cfg.WorkDir = oldWorkDir }()

	m := &model{
		insideTmux:        true,
		managedWindows:    make(map[string]managedWindow),
		replacedSessions:  make(map[string]bool),
		expandedWorktrees: make(map[int]bool),
		worktreeCache:     make(map[string][]git.Worktree),
	}

	result, _ := m.startProjectPicker(nil, false)
	rm := result.(model)

	// Should skip picker entirely.
	if rm.pickingProject {
		t.Error("expected pickingProject to be false when work_dir is empty")
	}
}

func TestProjectPickerFilter(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, "webapp"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "api-server"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "mobile-app"), 0755)

	oldWorkDir := cfg.WorkDir
	cfg.WorkDir = tmpDir
	defer func() { cfg.WorkDir = oldWorkDir }()

	m := &model{
		managedWindows:    make(map[string]managedWindow),
		replacedSessions:  make(map[string]bool),
		expandedWorktrees: make(map[int]bool),
		worktreeCache:     make(map[string][]git.Worktree),
	}

	result, _ := m.startProjectPicker(nil, false)
	rm := result.(model)

	// No filter → all dirs (root + 3 subdirs).
	if got := len(rm.filteredProjectDirs()); got != 4 {
		t.Errorf("no filter: got %d dirs, want 4", got)
	}

	// Filter "app" → matches "webapp", "mobile-app".
	rm.projectFilter = "app"
	filtered := rm.filteredProjectDirs()
	if len(filtered) != 2 {
		t.Errorf("filter 'app': got %d dirs, want 2: %v", len(filtered), filtered)
	}
}

func TestShortModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "opus"},
		{"claude-sonnet-4-6", "sonnet"},
		{"claude-haiku-4-5-20251001", "haiku"},
		{"claude-3-5-sonnet-20241022", "sonnet"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortModelName(tt.input)
			if got != tt.want {
				t.Errorf("shortModelName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
