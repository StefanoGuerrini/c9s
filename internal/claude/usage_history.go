package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageDataPoint is a single usage snapshot recorded from the API.
type UsageDataPoint struct {
	Time     time.Time      `json:"t"`
	FiveHour float64        `json:"5h"`
	SevenDay float64        `json:"7d"`
	Extra    *float64       `json:"extra"`
	Tokens   int            `json:"tokens"`
	Models   map[string]int `json:"models,omitempty"` // model name → cumulative tokens
}

// UsageHistoryPathOverride overrides the history file path for testing.
var UsageHistoryPathOverride string

func usageHistoryPath() string {
	if UsageHistoryPathOverride != "" {
		return UsageHistoryPathOverride
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".c9s", "usage-history.jsonl")
}

// RecordUsage appends a usage data point to the history file.
// Errors are silently ignored — this should never block the main flow.
func RecordUsage(u *Usage, totalTokens int, models map[string]int) {
	if u == nil {
		return
	}

	var extra *float64
	if u.ExtraUsage != nil && u.ExtraUsage.Utilization != nil {
		extra = u.ExtraUsage.Utilization
	}

	dp := UsageDataPoint{
		Time:     time.Now().UTC(),
		FiveHour: u.FiveHour.Utilization,
		SevenDay: u.SevenDay.Utilization,
		Extra:    extra,
		Tokens:   totalTokens,
		Models:   models,
	}

	path := usageHistoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(dp)
	if err != nil {
		return
	}
	f.Write(data)
	f.Write([]byte{'\n'})
}

// usageHistoryCache caches the parsed history file.
var historyCache struct {
	mu     sync.Mutex
	mtime  time.Time
	points []UsageDataPoint
}

// LoadUsageHistory reads all data points from the history file.
// Uses mtime-based caching to avoid re-reading unchanged file.
func LoadUsageHistory() []UsageDataPoint {
	path := usageHistoryPath()

	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}

	historyCache.mu.Lock()
	defer historyCache.mu.Unlock()

	if fi.ModTime().Equal(historyCache.mtime) {
		return historyCache.points
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var points []UsageDataPoint
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var dp UsageDataPoint
		if json.Unmarshal(scanner.Bytes(), &dp) == nil {
			points = append(points, dp)
		}
	}

	historyCache.mtime = fi.ModTime()
	historyCache.points = points
	return points
}

// ResetUsageHistory deletes the history file and clears the cache.
func ResetUsageHistory() error {
	path := usageHistoryPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	historyCache.mu.Lock()
	historyCache.mtime = time.Time{}
	historyCache.points = nil
	historyCache.mu.Unlock()
	return nil
}
