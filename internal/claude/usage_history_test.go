package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordUsage(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	u := &Usage{
		FiveHour: UsageWindow{Utilization: 42.5},
		SevenDay: UsageWindow{Utilization: 15.2},
	}
	RecordUsage(u, 125000, nil)

	data, err := os.ReadFile(UsageHistoryPathOverride)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}

	var dp UsageDataPoint
	if err := json.Unmarshal(data, &dp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dp.FiveHour != 42.5 {
		t.Errorf("FiveHour = %v, want 42.5", dp.FiveHour)
	}
	if dp.SevenDay != 15.2 {
		t.Errorf("SevenDay = %v, want 15.2", dp.SevenDay)
	}
	if dp.Extra != nil {
		t.Errorf("Extra = %v, want nil", dp.Extra)
	}
	if dp.Tokens != 125000 {
		t.Errorf("Tokens = %d, want 125000", dp.Tokens)
	}
}

func TestRecordUsageAppend(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	u := &Usage{FiveHour: UsageWindow{Utilization: 10.0}}
	RecordUsage(u, 100, nil)
	RecordUsage(u, 200, nil)

	points := LoadUsageHistory()
	if len(points) != 2 {
		t.Errorf("got %d points, want 2", len(points))
	}
}

func TestRecordUsageCreatesDir(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "sub", "dir", "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	RecordUsage(&Usage{}, 0, nil)

	if _, err := os.Stat(UsageHistoryPathOverride); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestRecordUsageWithExtra(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	pct := 25.5
	u := &Usage{
		FiveHour:   UsageWindow{Utilization: 50.0},
		ExtraUsage: &ExtraUsage{Utilization: &pct},
	}
	RecordUsage(u, 500, nil)

	points := LoadUsageHistory()
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	if points[0].Extra == nil || *points[0].Extra != 25.5 {
		t.Errorf("Extra = %v, want 25.5", points[0].Extra)
	}
}

func TestRecordUsageWithModels(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	models := map[string]int{
		"claude-opus-4-6":    80000,
		"claude-sonnet-4-6":  20000,
	}
	RecordUsage(&Usage{FiveHour: UsageWindow{Utilization: 30.0}}, 100000, models)

	points := LoadUsageHistory()
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	if len(points[0].Models) != 2 {
		t.Errorf("Models len = %d, want 2", len(points[0].Models))
	}
	if points[0].Models["claude-opus-4-6"] != 80000 {
		t.Errorf("opus tokens = %d, want 80000", points[0].Models["claude-opus-4-6"])
	}
	if points[0].Models["claude-sonnet-4-6"] != 20000 {
		t.Errorf("sonnet tokens = %d, want 20000", points[0].Models["claude-sonnet-4-6"])
	}
}

func TestRecordUsageNil(t *testing.T) {
	// Should not panic.
	RecordUsage(nil, 0, nil)
}

func TestLoadUsageHistoryMissing(t *testing.T) {
	UsageHistoryPathOverride = filepath.Join(t.TempDir(), "nonexistent.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	points := LoadUsageHistory()
	if len(points) != 0 {
		t.Errorf("got %d points for missing file, want 0", len(points))
	}
}

func TestLoadUsageHistoryCache(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	// Reset cache.
	historyCache.mu.Lock()
	historyCache.mtime = time.Time{}
	historyCache.points = nil
	historyCache.mu.Unlock()

	RecordUsage(&Usage{FiveHour: UsageWindow{Utilization: 10.0}}, 100, nil)

	p1 := LoadUsageHistory()
	if len(p1) != 1 {
		t.Fatalf("first load: got %d, want 1", len(p1))
	}

	// Second load should return cached.
	p2 := LoadUsageHistory()
	if len(p2) != 1 {
		t.Fatalf("cached load: got %d, want 1", len(p2))
	}

	// Write another point — mtime changes, cache invalidated.
	// Need small delay so mtime differs.
	time.Sleep(10 * time.Millisecond)
	RecordUsage(&Usage{FiveHour: UsageWindow{Utilization: 20.0}}, 200, nil)

	p3 := LoadUsageHistory()
	if len(p3) != 2 {
		t.Errorf("after append: got %d, want 2", len(p3))
	}
}

func TestResetUsageHistory(t *testing.T) {
	dir := t.TempDir()
	UsageHistoryPathOverride = filepath.Join(dir, "history.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	// Reset cache.
	historyCache.mu.Lock()
	historyCache.mtime = time.Time{}
	historyCache.points = nil
	historyCache.mu.Unlock()

	RecordUsage(&Usage{FiveHour: UsageWindow{Utilization: 50.0}}, 500, nil)
	RecordUsage(&Usage{FiveHour: UsageWindow{Utilization: 60.0}}, 600, nil)

	points := LoadUsageHistory()
	if len(points) != 2 {
		t.Fatalf("before reset: got %d, want 2", len(points))
	}

	if err := ResetUsageHistory(); err != nil {
		t.Fatalf("ResetUsageHistory error: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(UsageHistoryPathOverride); !os.IsNotExist(err) {
		t.Error("file should be deleted after reset")
	}

	// Cache should be cleared.
	points = LoadUsageHistory()
	if len(points) != 0 {
		t.Errorf("after reset: got %d, want 0", len(points))
	}
}

func TestResetUsageHistoryMissing(t *testing.T) {
	UsageHistoryPathOverride = filepath.Join(t.TempDir(), "nonexistent.jsonl")
	defer func() { UsageHistoryPathOverride = "" }()

	// Should not error when file doesn't exist.
	if err := ResetUsageHistory(); err != nil {
		t.Errorf("ResetUsageHistory on missing file: %v", err)
	}
}
