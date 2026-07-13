package notify

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PushOptions describes where and how to send a smartphone push notification.
// Wire these from the caller's config — the notify package stays free of
// any c9s config dependency.
type PushOptions struct {
	Provider string // "off" (default) or "ntfy"
	URL      string // base URL — e.g. "https://ntfy.sh" or a self-hosted host
	Topic    string // ntfy topic; required when Provider == "ntfy"
	Token    string // optional bearer token (private ntfy topics)
	User     string // optional basic-auth username (self-hosted ntfy)
	Password string // optional basic-auth password (self-hosted ntfy)
}

// httpClient is the HTTP client used by Push. Tests replace this with a
// stubbed client so they don't touch the network.
var httpClient = &http.Client{Timeout: 5 * time.Second}

const defaultNtfyURL = "https://ntfy.sh"

// Push delivers (title, body) through the configured smartphone push
// provider. Returns nil when the provider is "off" or unset — callers can
// invoke Push unconditionally without a guard. Failures are returned so the
// caller can log them; the existing desktop notification still fires.
func Push(opts PushOptions, title, body string) error {
	if opts.Provider == "" || opts.Provider == "off" {
		return nil
	}
	if title == "" && body == "" {
		return nil
	}
	switch opts.Provider {
	case "ntfy":
		return pushNtfy(opts, title, body)
	default:
		return fmt.Errorf("notify push: unknown provider %q", opts.Provider)
	}
}

func pushNtfy(opts PushOptions, title, body string) error {
	if opts.Topic == "" {
		return fmt.Errorf("notify push: ntfy topic is empty")
	}
	base := opts.URL
	if base == "" {
		base = defaultNtfyURL
	}
	base = strings.TrimRight(base, "/")
	url := base + "/" + opts.Topic

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify push: %w", err)
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	// Auth precedence: bearer token > basic auth. Both are valid ntfy
	// schemes, but only one Authorization header can be sent at a time.
	switch {
	case opts.Token != "":
		req.Header.Set("Authorization", "Bearer "+opts.Token)
	case opts.User != "":
		req.SetBasicAuth(opts.User, opts.Password)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notify push: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notify push: ntfy returned %d", resp.StatusCode)
	}
	return nil
}
