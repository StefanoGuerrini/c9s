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
	usageCacheTTL  = 5 * time.Minute
	usageMaxBackoff = 30 * time.Minute
)

// FetchUsage returns the current account usage from Anthropic's OAuth API.
// Results are cached for 5 minutes. On repeated failures, backs off
// exponentially up to 30 minutes before retrying.
func FetchUsage() (*Usage, error) {
	usageCache.mu.Lock()
	cached := usageCache.usage
	fresh := cached != nil && time.Since(usageCache.fetched) < usageCacheTTL
	tooSoon := time.Now().Before(usageCache.nextTry)
	usageCache.mu.Unlock()

	if fresh {
		return cached, nil
	}
	if tooSoon {
		if cached != nil {
			return cached, nil
		}
		return nil, fmt.Errorf("usage API: backing off after repeated failures")
	}

	usage, err := fetchUsageOnce()
	if err != nil {
		usageCache.mu.Lock()
		usageCache.failures++
		backoff := usageCacheTTL * time.Duration(1<<min(usageCache.failures, 4))
		if backoff > usageMaxBackoff {
			backoff = usageMaxBackoff
		}
		usageCache.nextTry = time.Now().Add(backoff)
		usageCache.mu.Unlock()

		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	usageCache.mu.Lock()
	usageCache.usage = usage
	usageCache.fetched = time.Now()
	usageCache.failures = 0
	usageCache.nextTry = time.Time{}
	usageCache.mu.Unlock()

	return usage, nil
}

// fetchUsageOnce makes a single API call to get usage data.
func fetchUsageOnce() (*Usage, error) {
	token, err := getOAuthToken()
	if err != nil {
		return nil, fmt.Errorf("no OAuth token: %w", err)
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("usage API: %d %s", resp.StatusCode, string(body))
	}

	var usage Usage
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

// oauthCreds is the JSON structure stored in the credentials file and keychain.
type oauthCreds struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// getOAuthToken reads Claude Code's OAuth access token.
// On macOS: tries Keychain first, falls back to credentials file.
// On Linux/Windows: reads from ~/.claude/.credentials.json (or $CLAUDE_CONFIG_DIR).
func getOAuthToken() (string, error) {
	usageCache.mu.Lock()
	if usageCache.token != "" {
		t := usageCache.token
		usageCache.mu.Unlock()
		return t, nil
	}
	usageCache.mu.Unlock()

	var token string
	var err error

	if runtime.GOOS == "darwin" {
		// macOS: try Keychain first, fall back to credentials file.
		token, err = readKeychainToken()
		if err != nil {
			token, err = readCredentialsFile()
		}
	} else {
		// Linux/Windows: credentials file only.
		token, err = readCredentialsFile()
	}

	if err != nil {
		return "", err
	}

	usageCache.mu.Lock()
	usageCache.token = token
	usageCache.mu.Unlock()

	return token, nil
}

// readKeychainToken reads the OAuth token from macOS Keychain.
func readKeychainToken() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain read failed: %w", err)
	}
	return parseOAuthToken(out)
}

// readCredentialsFile reads the OAuth token from ~/.claude/.credentials.json.
func readCredentialsFile() (string, error) {
	path := filepath.Join(claudeDir(), ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("credentials file: %w", err)
	}
	return parseOAuthToken(data)
}

// parseOAuthToken extracts the access token from credentials JSON.
func parseOAuthToken(data []byte) (string, error) {
	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}
	token := strings.TrimSpace(creds.ClaudeAiOauth.AccessToken)
	if token == "" {
		return "", fmt.Errorf("empty OAuth token")
	}
	return token, nil
}
