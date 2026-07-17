package sessionstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	prev := DirOverride
	DirOverride = d
	t.Cleanup(func() { DirOverride = prev })
}

func TestSetStateAndRead(t *testing.T) {
	withTempDir(t)
	if err := SetState("abc123", StateProcessing, "SessionStart"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, err := Read("abc123")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.State != StateProcessing {
		t.Errorf("state = %q, want %q", got.State, StateProcessing)
	}
	if got.SessionID != "abc123" {
		t.Errorf("session id = %q", got.SessionID)
	}
	if got.LastEvent != "SessionStart" {
		t.Errorf("last event = %q", got.LastEvent)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("updated_at not set")
	}
}

func TestSetStateOverwritesPreservesMetadata(t *testing.T) {
	withTempDir(t)
	if err := UpdateMetadata("s1", Metadata{Model: "claude-opus", Cwd: "/x", CostUSD: 0.5}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if err := SetState("s1", StateWaiting, "Notification"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, _ := Read("s1")
	if got.State != StateWaiting {
		t.Errorf("state = %q, want waiting", got.State)
	}
	if got.Model != "claude-opus" {
		t.Errorf("model dropped: %q", got.Model)
	}
	if got.Cwd != "/x" {
		t.Errorf("cwd dropped: %q", got.Cwd)
	}
	if got.CostUSD != 0.5 {
		t.Errorf("cost dropped: %v", got.CostUSD)
	}
}

func TestUpdateMetadataDoesNotOverwriteState(t *testing.T) {
	withTempDir(t)
	SetState("s2", StateProcessing, "UserPromptSubmit")
	if err := UpdateMetadata("s2", Metadata{Model: "claude-haiku"}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	got, _ := Read("s2")
	if got.State != StateProcessing {
		t.Errorf("state changed to %q (statusLine should not override)", got.State)
	}
	if got.Model != "claude-haiku" {
		t.Errorf("model not updated: %q", got.Model)
	}
}

func TestUpdateMetadataOnEmptyDefaultsToUnknown(t *testing.T) {
	withTempDir(t)
	if err := UpdateMetadata("s3", Metadata{Model: "m"}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	got, _ := Read("s3")
	if got.State != StateUnknown {
		t.Errorf("state = %q, want unknown", got.State)
	}
}

func TestReadMissing(t *testing.T) {
	withTempDir(t)
	_, err := Read("nonexistent")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRemove(t *testing.T) {
	withTempDir(t)
	SetState("s4", StateDone, "Stop")
	if err := Remove("s4"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Read("s4"); err == nil {
		t.Error("expected file gone after Remove")
	}
	// Idempotent.
	if err := Remove("s4"); err != nil {
		t.Errorf("Remove on missing should be nil, got %v", err)
	}
}

func TestInvalidSessionID(t *testing.T) {
	withTempDir(t)
	cases := []string{"", "../escape", "with/slash", "with space"}
	for _, c := range cases {
		if err := SetState(c, StateDone, "Stop"); err == nil {
			t.Errorf("SetState(%q) should fail", c)
		}
	}
}

func TestPatchCreatesAndUpdates(t *testing.T) {
	withTempDir(t)
	if err := Patch("p1", func(info *Info) {
		info.State = StateProcessing
		info.LastEvent = "UserPromptSubmit"
		info.MessageCount = 1
		info.InputTokens = 100
	}); err != nil {
		t.Fatalf("Patch (create): %v", err)
	}
	got, err := Read("p1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.MessageCount != 1 || got.InputTokens != 100 {
		t.Errorf("initial patch lost: %+v", got)
	}

	if err := Patch("p1", func(info *Info) {
		info.MessageCount++
		info.InputTokens += 50
		info.OutputTokens = 30
	}); err != nil {
		t.Fatalf("Patch (update): %v", err)
	}
	got, _ = Read("p1")
	if got.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", got.MessageCount)
	}
	if got.InputTokens != 150 {
		t.Errorf("InputTokens = %d, want 150", got.InputTokens)
	}
	if got.OutputTokens != 30 {
		t.Errorf("OutputTokens = %d, want 30", got.OutputTokens)
	}
	// SessionID is set automatically.
	if got.SessionID != "p1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	// UpdatedAt always refreshed.
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestPatchPreservesUnsetFields(t *testing.T) {
	withTempDir(t)
	// First patch sets several fields.
	Patch("p2", func(info *Info) {
		info.State = StateProcessing
		info.Model = "opus"
		info.MessageCount = 5
		info.LastNotification = "first"
	})
	// Second patch only updates State; the rest must survive.
	Patch("p2", func(info *Info) {
		info.State = StateWaiting
	})
	got, _ := Read("p2")
	if got.State != StateWaiting {
		t.Errorf("state not updated: %q", got.State)
	}
	if got.Model != "opus" {
		t.Errorf("model lost: %q", got.Model)
	}
	if got.MessageCount != 5 {
		t.Errorf("MessageCount lost: %d", got.MessageCount)
	}
	if got.LastNotification != "first" {
		t.Errorf("LastNotification lost: %q", got.LastNotification)
	}
}

func TestPaneSessionMap_FreshestWinsPerPane(t *testing.T) {
	withTempDir(t)
	// Two sessions both claim pane %5. Write them out of order to prove the
	// map sorts by UpdatedAt, not by filesystem order.
	if err := write(Info{SessionID: "stale", TmuxPane: "%5", UpdatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := write(Info{SessionID: "fresh", TmuxPane: "%5", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// A session with no pane recorded must not appear.
	if err := SetState("no-pane", StateProcessing, "SessionStart"); err != nil {
		t.Fatal(err)
	}
	got := PaneSessionMap()
	if got["%5"] != "fresh" {
		t.Errorf("pane %%5 = %q, want %q", got["%5"], "fresh")
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty pane key should not appear: %v", got)
	}
}

func TestPaneSessionMap_EmptyWhenNoStateDir(t *testing.T) {
	prev := DirOverride
	DirOverride = "/tmp/definitely-not-a-real-c9s-state-dir-xyz"
	t.Cleanup(func() { DirOverride = prev })
	if got := PaneSessionMap(); len(got) != 0 {
		t.Errorf("expected empty map for missing dir, got %v", got)
	}
}

func TestAtomicWrite(t *testing.T) {
	withTempDir(t)
	SetState("s5", StateProcessing, "SessionStart")
	// No leftover temp files in the state dir.
	entries, err := os.ReadDir(DirOverride)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name()[0] == '.' {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
	// File exists at expected path.
	if _, err := os.Stat(filepath.Join(DirOverride, "s5.json")); err != nil {
		t.Errorf("expected file: %v", err)
	}
}
