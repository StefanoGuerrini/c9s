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
	mu      sync.Mutex
	usage   *Usage
	fetched time.Time
	token   string
}

// FetchUsage returns the current account usage from Anthropic's OAuth API.
// Results are cached for 5 minutes to avoid rate limiting.
// On error, returns the last cached value if available.
func FetchUsage() (*Usage, error) {
	usageCache.mu.Lock()
	cached := usageCache.usage
	fresh := usageCache.usage != nil && time.Since(usageCache.fetched) < 5*time.Minute
	usageCache.mu.Unlock()

	if fresh {
		return cached, nil
	}

	token, err := getOAuthToken()
	if err != nil {
		if cached != nil {
			return cached, nil // return stale cache on error
		}
		return nil, fmt.Errorf("no OAuth token: %w", err)
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if cached != nil {
			return cached, nil // keep showing stale data on rate limit
		}
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("usage API: %d %s", resp.StatusCode, string(body))
	}

	var usage Usage
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	usageCache.mu.Lock()
	usageCache.usage = &usage
	usageCache.fetched = time.Now()
	usageCache.mu.Unlock()

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
