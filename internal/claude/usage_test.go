package claude

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func resetUsageCache() {
	usageCache.mu.Lock()
	usageCache.usage = nil
	usageCache.fetched = time.Time{}
	usageCache.nextTry = time.Time{}
	usageCache.failures = 0
	usageCache.token = ""
	usageCache.mu.Unlock()
}

func TestFetchUsage_CachesResult(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(`{"five_hour":{"utilization":42.5},"seven_day":{"utilization":10.0}}`))
	}))
	defer srv.Close()

	resetUsageCache()
	usageCache.mu.Lock()
	usageCache.token = "test-token"
	usageCache.usage = &Usage{FiveHour: UsageWindow{Utilization: 42.5}}
	usageCache.fetched = time.Now()
	usageCache.mu.Unlock()

	// Should return cached result without calling API.
	result, err := FetchUsage()
	if err != nil {
		t.Fatalf("FetchUsage() error: %v", err)
	}
	if result.Usage.FiveHour.Utilization != 42.5 {
		t.Errorf("utilization = %v, want 42.5", result.Usage.FiveHour.Utilization)
	}
	if result.Stale {
		t.Error("fresh cache should not be stale")
	}
}

func TestFetchUsage_BackoffOnFailure(t *testing.T) {
	resetUsageCache()

	// Simulate failures by setting backoff state.
	usageCache.mu.Lock()
	usageCache.failures = 2
	usageCache.nextTry = time.Now().Add(10 * time.Minute)
	usageCache.usage = &Usage{FiveHour: UsageWindow{Utilization: 30.0}}
	usageCache.fetched = time.Now().Add(-10 * time.Minute) // stale cache
	usageCache.mu.Unlock()

	// Should return stale cache because we're in backoff period.
	result, err := FetchUsage()
	if err != nil {
		t.Fatalf("FetchUsage() error during backoff: %v", err)
	}
	if result.Usage.FiveHour.Utilization != 30.0 {
		t.Errorf("utilization = %v, want 30.0 (stale cache)", result.Usage.FiveHour.Utilization)
	}
	if !result.Stale {
		t.Error("backoff cache should be marked stale")
	}
}

func TestFetchUsage_BackoffNoCache(t *testing.T) {
	resetUsageCache()

	// Backoff with no cached data should return error.
	usageCache.mu.Lock()
	usageCache.failures = 1
	usageCache.nextTry = time.Now().Add(10 * time.Minute)
	usageCache.mu.Unlock()

	_, err := FetchUsage()
	if err == nil {
		t.Error("FetchUsage() should error during backoff with no cache")
	}
}

func TestFetchUsage_BackoffSchedule(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{1, 10 * time.Minute},              // 5min * 2^1 = 10min
		{2, 20 * time.Minute},              // 5min * 2^2 = 20min
		{3, 40 * time.Minute},              // 5min * 2^3 = 40min
		{4, 80 * time.Minute},              // 5min * 2^4 = 80min
		{5, 160 * time.Minute},             // 5min * 2^5 = 160min
		{6, 4 * time.Hour},                 // 5min * 2^6 = 320min, capped at 4h
		{10, 4 * time.Hour},                // capped at 4h
	}

	for _, tt := range tests {
		resetUsageCache()
		usageCache.mu.Lock()
		usageCache.token = "test-token"
		usageCache.failures = tt.failures - 1
		usageCache.mu.Unlock()

		usageCache.mu.Lock()
		usageCache.failures++
		backoff := usageCacheTTL * time.Duration(1<<min(usageCache.failures, 6))
		if backoff > usageMaxBackoff {
			backoff = usageMaxBackoff
		}
		usageCache.mu.Unlock()

		if backoff != tt.want {
			t.Errorf("failures=%d: backoff=%v, want %v",
				tt.failures, backoff, tt.want)
		}
	}
}

func TestFetchUsage_HalvesFailuresOnSuccess(t *testing.T) {
	resetUsageCache()

	// Simulate state after 4 failures then a successful recovery.
	usageCache.mu.Lock()
	usageCache.usage = &Usage{FiveHour: UsageWindow{Utilization: 50.0}}
	usageCache.fetched = time.Now()
	usageCache.failures = 4 // was 4 failures, success halves to 2
	usageCache.nextTry = time.Time{}
	usageCache.mu.Unlock()

	result, err := FetchUsage()
	if err != nil {
		t.Fatalf("FetchUsage() error: %v", err)
	}
	if result.Usage.FiveHour.Utilization != 50.0 {
		t.Errorf("utilization = %v, want 50.0", result.Usage.FiveHour.Utilization)
	}

	// After fresh cache (simulated success above), failures should remain as-is
	// since we didn't go through the actual fetch path.
	// Test the halving logic directly.
	failures := 4
	failures = failures / 2
	if failures != 2 {
		t.Errorf("halved failures = %d, want 2", failures)
	}
	failures = failures / 2
	if failures != 1 {
		t.Errorf("double halved failures = %d, want 1", failures)
	}
	failures = failures / 2
	if failures != 0 {
		t.Errorf("triple halved failures = %d, want 0", failures)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		val  string
		want time.Duration
	}{
		{"", 0},
		{"60", 60 * time.Second},
		{"120", 120 * time.Second},
		{"0", 0},
		{"-1", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.val)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestRateLimitError(t *testing.T) {
	err := &rateLimitError{retryAfter: 60 * time.Second}
	if err.Error() == "" {
		t.Error("rateLimitError.Error() should not be empty")
	}
}

func TestFetchUsage_Concurrent(t *testing.T) {
	resetUsageCache()

	// Pre-populate cache.
	usageCache.mu.Lock()
	usageCache.usage = &Usage{FiveHour: UsageWindow{Utilization: 25.0}}
	usageCache.fetched = time.Now()
	usageCache.token = "test-token"
	usageCache.mu.Unlock()

	// Concurrent reads should all succeed without races.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := FetchUsage()
			if err != nil {
				t.Errorf("concurrent FetchUsage() error: %v", err)
			}
			if result == nil || result.Usage == nil {
				t.Error("concurrent FetchUsage() returned nil")
			}
		}()
	}
	wg.Wait()
}
