package main

import (
	"testing"
	"time"

	"github.com/stefanoguerrini/c9s/internal/claude"
	"github.com/stefanoguerrini/c9s/internal/git"
	"github.com/stefanoguerrini/c9s/internal/tmux"
)

func TestReconcileWindows_NewSession(t *testing.T) {
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

func TestReconcileWindows_Fork(t *testing.T) {
	// After fork: old session JSONL is stale, new forked session is active.
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
			FileMtime:   time.Now().Add(-5 * time.Minute), // stale
		},
		{
			SessionID:   "forked-session-id",
			ProjectPath: "/home/user/project",
			FileMtime:   time.Now(), // recently active
		},
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

func TestReconcileWindows_PicksMostRecent(t *testing.T) {
	// Multiple active sessions in same project — should pick the most recent.
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
			FileMtime:   now.Add(-5 * time.Minute), // stale
		},
		{
			SessionID:   "session-a",
			ProjectPath: "/home/user/project",
			FileMtime:   now.Add(-10 * time.Second),
		},
		{
			SessionID:   "session-b",
			ProjectPath: "/home/user/project",
			FileMtime:   now.Add(-2 * time.Second), // most recent
		},
	}

	m.reconcileWindows(sessions)

	if _, ok := m.managedWindows["old-id"]; ok {
		t.Error("old entry should be deleted")
	}
	if _, ok := m.managedWindows["session-b"]; !ok {
		t.Error("expected entry under most recent session-b")
	}
}

func TestReconcileWindows_NoNewSession(t *testing.T) {
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
