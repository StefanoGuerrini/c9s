package main

import (
	"testing"
	"time"

	"github.com/stefanoguerrini/c9s/internal/claude"
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
