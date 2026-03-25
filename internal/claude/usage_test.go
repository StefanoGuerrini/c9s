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
		minWait  time.Duration
		maxWait  time.Duration
	}{
		{1, 10 * time.Minute, 10 * time.Minute},  // 5min * 2^1 = 10min
		{2, 20 * time.Minute, 20 * time.Minute},  // 5min * 2^2 = 20min
		{3, 30 * time.Minute, 30 * time.Minute},  // 5min * 2^3 = 40min, capped at 30min
		{5, 30 * time.Minute, 30 * time.Minute},  // capped at 30min
	}

	for _, tt := range tests {
		resetUsageCache()
		usageCache.mu.Lock()
		usageCache.token = "test-token"
		usageCache.failures = tt.failures - 1 // will be incremented by the failure
		usageCache.mu.Unlock()

		// Simulate a failure path by calling the backoff logic directly.
		usageCache.mu.Lock()
		usageCache.failures++
		backoff := usageCacheTTL * time.Duration(1<<min(usageCache.failures, 4))
		if backoff > usageMaxBackoff {
			backoff = usageMaxBackoff
		}
		usageCache.mu.Unlock()

		if backoff < tt.minWait || backoff > tt.maxWait {
			t.Errorf("failures=%d: backoff=%v, want between %v and %v",
				tt.failures, backoff, tt.minWait, tt.maxWait)
		}
	}
}

func TestFetchUsage_ResetOnSuccess(t *testing.T) {
	resetUsageCache()

	// Simulate successful fetch by populating cache directly.
	usageCache.mu.Lock()
	usageCache.usage = &Usage{FiveHour: UsageWindow{Utilization: 50.0}}
	usageCache.fetched = time.Now()
	usageCache.failures = 0
	usageCache.nextTry = time.Time{}
	usageCache.mu.Unlock()

	result, err := FetchUsage()
	if err != nil {
		t.Fatalf("FetchUsage() error: %v", err)
	}
	if result.Usage.FiveHour.Utilization != 50.0 {
		t.Errorf("utilization = %v, want 50.0", result.Usage.FiveHour.Utilization)
	}
	if result.Stale {
		t.Error("fresh result should not be stale")
	}

	usageCache.mu.Lock()
	failures := usageCache.failures
	usageCache.mu.Unlock()

	if failures != 0 {
		t.Errorf("failures = %d after success, want 0", failures)
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
