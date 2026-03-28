package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Usage holds the account-wide usage data from Anthropic's API.
type Usage struct {
	FiveHour   UsageWindow `json:"five_hour"`
	SevenDay   UsageWindow `json:"seven_day"`
	ExtraUsage *ExtraUsage `json:"extra_usage"`
}

// UsageWindow holds utilization for a time window.
type UsageWindow struct {
	Utilization float64 `json:"utilization"` // percentage 0-100
	ResetsAt    string  `json:"resets_at"`
}

// ExtraUsage holds overage capacity info.
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *int     `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

// usageCache caches the API response and OAuth token.
var usageCache struct {
	mu       sync.Mutex
	usage    *Usage
	fetched  time.Time
	nextTry  time.Time // earliest time to retry after failure
	failures int       // consecutive failure count for backoff
	token    string
}

const (
	usageCacheTTL    = 5 * time.Minute
	usageMaxBackoff  = 4 * time.Hour
	usageAuthBackoff = 10 * time.Minute // shorter backoff for auth errors — token may be refreshed by Claude Code
)

// UsageResult wraps usage data with metadata about freshness.
type UsageResult struct {
	Usage   *Usage
	Stale   bool      // true when returning cached data after a failed refresh
	Fetched time.Time // when the data was last fetched from the API
}

// FetchUsage returns the current account usage from Anthropic's OAuth API.
// Results are cached for 5 minutes. On repeated failures, backs off
// exponentially up to 30 minutes before retrying.
// The Stale flag indicates when the result comes from cache after a failed refresh.
func FetchUsage() (*UsageResult, error) {
	usageCache.mu.Lock()
	cached := usageCache.usage
	fetched := usageCache.fetched
	fresh := cached != nil && time.Since(fetched) < usageCacheTTL
	tooSoon := time.Now().Before(usageCache.nextTry)
	usageCache.mu.Unlock()

	if fresh {
		return &UsageResult{Usage: cached, Stale: false, Fetched: fetched}, nil
	}
	if tooSoon {
		DebugLog("usage → backing off (nextTry in %s, failures=%d)", time.Until(usageCache.nextTry).Round(time.Second), usageCache.failures)
		if cached != nil {
			return &UsageResult{Usage: cached, Stale: true, Fetched: fetched}, nil
		}
		return nil, fmt.Errorf("usage API: backing off after repeated failures")
	}

	DebugLog("usage → fetching from API")
	usage, err := fetchUsageOnce()
	if err != nil {
		usageCache.mu.Lock()
		usageCache.failures++

		// Use Retry-After from server if available, otherwise exponential backoff.
		// Auth errors use a shorter max backoff since the token may be refreshed
		// at any time by Claude Code.
		var backoff time.Duration
		if rle, ok := err.(*rateLimitError); ok && rle.retryAfter > 0 {
			backoff = rle.retryAfter
			DebugLog("usage → 429 rate limited, retry after %s (failures=%d)", backoff, usageCache.failures)
		} else if isAuthError(err) {
			backoff = usageCacheTTL * time.Duration(1<<min(usageCache.failures, 3))
			if backoff > usageAuthBackoff {
				backoff = usageAuthBackoff
			}
			DebugLog("usage → auth error: %v, backoff=%s (failures=%d)", err, backoff, usageCache.failures)
		} else {
			backoff = usageCacheTTL * time.Duration(1<<min(usageCache.failures, 6))
			DebugLog("usage → error: %v, backoff=%s (failures=%d)", err, backoff, usageCache.failures)
		}
		if backoff > usageMaxBackoff {
			backoff = usageMaxBackoff
		}
		usageCache.nextTry = time.Now().Add(backoff)
		usageCache.mu.Unlock()

		if cached != nil {
			return &UsageResult{Usage: cached, Stale: true, Fetched: fetched}, nil
		}
		return nil, err
	}

	now := time.Now()
	usageCache.mu.Lock()
	usageCache.usage = usage
	usageCache.fetched = now
	// Halve failures on success instead of resetting to 0.
	// This prevents hammering the API after recovering from rate limiting:
	// after 3 failures (30min backoff) and a success, next cache TTL is still 5min
	// but if it fails again, backoff starts at 10min (failures=1) not 10min (failures=1 from 0).
	usageCache.failures = usageCache.failures / 2
	if usageCache.failures == 0 {
		usageCache.nextTry = time.Time{}
	}
	usageCache.mu.Unlock()

	DebugLog("usage → fetched OK (failures=%d)", usageCache.failures)
	return &UsageResult{Usage: usage, Stale: false, Fetched: now}, nil
}

// fetchUsageOnce makes a single API call to get usage data.
// On 429, returns a rateLimitError with the Retry-After duration.
// On 401/403, clears the cached token and re-reads from Keychain/file
// (Claude Code may have already refreshed it) and retries once.
func fetchUsageOnce() (*Usage, error) {
	token, err := getOAuthToken()
	if err != nil {
		return nil, fmt.Errorf("no OAuth token: %w", err)
	}

	usage, statusCode, err := doUsageRequest(token)
	if err == nil {
		return usage, nil
	}

	// On auth failure, clear cached token and re-read from source.
	// Claude Code refreshes its own token when used, so re-reading
	// picks up the new token without us performing the OAuth refresh.
	if statusCode == 401 || statusCode == 403 {
		DebugLog("usage → %d, clearing cached token and re-reading", statusCode)
		clearCachedToken()
		newToken, readErr := readOAuthToken()
		if readErr != nil {
			DebugLog("usage → re-read token failed: %v", readErr)
			return nil, err // return original error
		}
		if newToken == token {
			DebugLog("usage → re-read token unchanged (still expired)")
			return nil, err
		}
		// Got a different (hopefully refreshed) token — cache and retry.
		DebugLog("usage → got new token, retrying")
		usageCache.mu.Lock()
		usageCache.token = newToken
		usageCache.mu.Unlock()
		usage, _, retryErr := doUsageRequest(newToken)
		if retryErr != nil {
			return nil, retryErr
		}
		return usage, nil
	}

	return nil, err
}

// doUsageRequest makes a single HTTP request to the usage API.
// Returns the usage data, HTTP status code, and any error.
func doUsageRequest(token string) (*Usage, int, error) {
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, 429, &rateLimitError{retryAfter: retryAfter}
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("usage API: %d %s", resp.StatusCode, string(body))
	}

	var usage Usage
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, 200, err
	}
	return &usage, 200, nil
}

// rateLimitError is returned on 429 responses, carrying the server's requested wait time.
type rateLimitError struct {
	retryAfter time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("usage API: 429 rate limited (retry after %s)", e.retryAfter)
}

// isAuthError checks if an error is an authentication/authorization failure.
func isAuthError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "usage API: 401") || strings.Contains(s, "usage API: 403")
}

// parseRetryAfter parses the Retry-After header value (seconds).
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// oauthCreds is the JSON structure stored in the credentials file and keychain.
type oauthCreds struct {
	ClaudeAiOauth oauthFields `json:"claudeAiOauth"`
}

type oauthFields struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken,omitempty"`
	ExpiresAt        int64    `json:"expiresAt,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// getOAuthToken reads Claude Code's OAuth access token.
// Returns cached token if available and not expired, otherwise reads fresh from source.
func getOAuthToken() (string, error) {
	usageCache.mu.Lock()
	if usageCache.token != "" {
		t := usageCache.token
		usageCache.mu.Unlock()
		return t, nil
	}
	usageCache.mu.Unlock()

	token, err := readOAuthToken()
	if err != nil {
		return "", err
	}

	usageCache.mu.Lock()
	usageCache.token = token
	usageCache.mu.Unlock()

	return token, nil
}

// clearCachedToken resets the cached token so the next call re-reads from source.
func clearCachedToken() {
	usageCache.mu.Lock()
	usageCache.token = ""
	usageCache.mu.Unlock()
}

// readOAuthToken reads the OAuth credentials from the platform-appropriate source.
// On macOS: tries Keychain first, falls back to credentials file.
// On Linux/Windows: reads from ~/.claude/.credentials.json (or $CLAUDE_CONFIG_DIR).
// If the token is expired, clears the cached token so the next API call triggers re-read.
func readOAuthToken() (string, error) {
	var creds *oauthCreds
	var err error

	if runtime.GOOS == "darwin" {
		creds, err = readKeychainCreds()
		if err != nil {
			creds, err = readCredentialsCreds()
		}
	} else {
		creds, err = readCredentialsCreds()
	}

	if err != nil {
		return "", err
	}

	token := strings.TrimSpace(creds.ClaudeAiOauth.AccessToken)
	if token == "" {
		return "", fmt.Errorf("empty OAuth token")
	}

	// Check expiry proactively (expiresAt is milliseconds since epoch).
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		expiresAt := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
		if time.Now().After(expiresAt) {
			DebugLog("usage → token expired at %s", expiresAt.Format("15:04:05"))
		}
	}

	return token, nil
}

// readKeychainCreds reads the OAuth credentials from macOS Keychain.
func readKeychainCreds() (*oauthCreds, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("keychain read failed: %w", err)
	}
	return parseOAuthCreds(out)
}

// readCredentialsCreds reads the OAuth credentials from ~/.claude/.credentials.json.
func readCredentialsCreds() (*oauthCreds, error) {
	path := filepath.Join(claudeDir(), ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file: %w", err)
	}
	return parseOAuthCreds(data)
}

// parseOAuthCreds parses the full OAuth credentials from JSON data.
func parseOAuthCreds(data []byte) (*oauthCreds, error) {
	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if strings.TrimSpace(creds.ClaudeAiOauth.AccessToken) == "" {
		return nil, fmt.Errorf("empty OAuth token")
	}
	return &creds, nil
}
