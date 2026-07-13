package sessionstate

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendCreatesAndAppends(t *testing.T) {
	withTempDir(t)
	if err := Append(TimelineEntry{SessionID: "t1", Event: "SessionStart"}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := Append(TimelineEntry{SessionID: "t1", Event: "Stop", Tool: "Bash"}); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	p := filepath.Join(filepath.Dir(DirOverride), "timeline", "t1.jsonl")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open timeline: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var entries []TimelineEntry
	for scanner.Scan() {
		var e TimelineEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Event != "SessionStart" || entries[1].Event != "Stop" {
		t.Errorf("events out of order: %+v", entries)
	}
	if entries[1].Tool != "Bash" {
		t.Errorf("tool not preserved: %+v", entries[1])
	}
	// Timestamps were auto-filled.
	for i, e := range entries {
		if e.Timestamp.IsZero() {
			t.Errorf("entry %d missing timestamp", i)
		}
	}
}

func TestAppendRejectsEmptyID(t *testing.T) {
	withTempDir(t)
	if err := Append(TimelineEntry{Event: "Stop"}); err == nil {
		t.Error("expected error for empty session_id")
	}
}

func TestRemoveTimeline(t *testing.T) {
	withTempDir(t)
	Append(TimelineEntry{SessionID: "t2", Event: "SessionStart"})
	if err := RemoveTimeline("t2"); err != nil {
		t.Fatalf("RemoveTimeline: %v", err)
	}
	if err := RemoveTimeline("t2"); err != nil {
		t.Errorf("RemoveTimeline should be idempotent, got %v", err)
	}
}
