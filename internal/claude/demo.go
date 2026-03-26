package claude

import (
	"time"

	"github.com/stefanoguerrini/c9s/internal/git"
)

// DemoWorktrees provides fake worktree data keyed by project path for demo mode.
var DemoWorktrees = map[string][]git.Worktree{
	"~/Projects/webapp": {
		{Path: "~/Projects/webapp", Branch: "main", IsMain: true},
		{Path: "~/Projects/webapp-feat-auth", Branch: "feat/jwt-auth", IsMain: false},
		{Path: "~/Projects/webapp-dark-mode", Branch: "feat/dark-mode", IsMain: false},
	},
	"~/Projects/realtime-api": {
		{Path: "~/Projects/realtime-api", Branch: "main", IsMain: true},
		{Path: "~/Projects/realtime-api-ws-fix", Branch: "fix/ws-keepalive", IsMain: false},
	},
}

// DemoSessions returns a set of fake sessions for screenshot/demo purposes.
func DemoSessions() []SessionInfo {
	now := time.Now()
	return []SessionInfo{
		{
			SessionID:    "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			CustomTitle:  "Refactor auth middleware",
			FirstPrompt:  "refactor the auth middleware to use JWT tokens instead of session cookies",
			MessageCount: 47,
			InputTokens:  125000,
			OutputTokens: 89000,
			CacheRead:    340000,
			CacheCreate:  45000,
			Created:      now.Add(-2 * time.Hour),
			Modified:     now.Add(-30 * time.Second),
			FileMtime:    now.Add(-30 * time.Second),
			ProjectPath:  "~/Projects/webapp",
			Dir:          "/demo/.claude/projects/-Projects-webapp",
			GitBranch:      "feat/jwt-auth",
			Status:         StatusActive,
			DemoPaneStatus: 1, // processing
		},
		{
			SessionID:    "b2c3d4e5-f6a7-8901-bcde-f12345678901",
			Summary:      "Add unit tests for payment service",
			FirstPrompt:  "write comprehensive unit tests for the payment processing service",
			MessageCount: 23,
			InputTokens:  67000,
			OutputTokens: 45000,
			CacheRead:    180000,
			CacheCreate:  22000,
			Created:      now.Add(-1 * time.Hour),
			Modified:     now.Add(-5 * time.Second),
			FileMtime:    now.Add(-5 * time.Second),
			ProjectPath:  "~/Projects/webapp",
			Dir:          "/demo/.claude/projects/-Projects-webapp",
			GitBranch:      "main",
			Status:         StatusActive,
			DemoPaneStatus: 2, // waiting
		},
		{
			SessionID:    "c3d4e5f6-a7b8-9012-cdef-123456789012",
			CustomTitle:  "Debug WebSocket reconnection",
			FirstPrompt:  "the websocket connection drops after 30 seconds of inactivity, help me fix it",
			MessageCount: 31,
			InputTokens:  98000,
			OutputTokens: 72000,
			CacheRead:    250000,
			CacheCreate:  35000,
			Created:      now.Add(-4 * time.Hour),
			Modified:     now.Add(-15 * time.Minute),
			FileMtime:    now.Add(-15 * time.Minute),
			ProjectPath:  "~/Projects/realtime-api",
			Dir:          "/demo/.claude/projects/-Projects-realtime-api",
			GitBranch:      "fix/ws-keepalive",
			Status:         StatusIdle,
			DemoPaneStatus: 3, // done
		},
		{
			SessionID:    "d4e5f6a7-b8c9-0123-defa-234567890123",
			Summary:      "Migrate database to PostgreSQL",
			FirstPrompt:  "help me migrate from SQLite to PostgreSQL, including schema changes",
			MessageCount: 56,
			InputTokens:  210000,
			OutputTokens: 156000,
			CacheRead:    520000,
			CacheCreate:  68000,
			Created:      now.Add(-24 * time.Hour),
			Modified:     now.Add(-45 * time.Minute),
			FileMtime:    now.Add(-45 * time.Minute),
			ProjectPath:  "~/Projects/webapp",
			Dir:          "/demo/.claude/projects/-Projects-webapp",
			GitBranch:    "feat/postgres-migration",
			Status:       StatusResumable,
		},
		{
			SessionID:    "e5f6a7b8-c9d0-1234-efab-345678901234",
			CustomTitle:  "Setup CI/CD pipeline",
			FirstPrompt:  "set up GitHub Actions for CI/CD with testing, linting, and deployment",
			MessageCount: 18,
			InputTokens:  42000,
			OutputTokens: 31000,
			CacheRead:    110000,
			CacheCreate:  15000,
			Created:      now.Add(-3 * time.Hour),
			Modified:     now.Add(-2 * time.Hour),
			FileMtime:    now.Add(-2 * time.Hour),
			ProjectPath:  "~/Projects/c9s",
			Dir:          "/demo/.claude/projects/-Projects-c9s",
			GitBranch:      "feat/ci",
			Status:         StatusActive,
			DemoPaneStatus: 1, // processing
		},
		{
			SessionID:    "f6a7b8c9-d0e1-2345-fabc-456789012345",
			Summary:      "Implement dark mode toggle",
			FirstPrompt:  "add a dark mode toggle to the settings page with system preference detection",
			MessageCount: 14,
			InputTokens:  35000,
			OutputTokens: 28000,
			CacheRead:    95000,
			CacheCreate:  12000,
			Created:      now.Add(-6 * time.Hour),
			Modified:     now.Add(-5 * time.Hour),
			FileMtime:    now.Add(-5 * time.Hour),
			ProjectPath:  "~/Projects/webapp",
			Dir:          "/demo/.claude/projects/-Projects-webapp",
			GitBranch:      "feat/dark-mode",
			Status:         StatusActive,
			DemoPaneStatus: 3, // done
		},
		{
			SessionID:    "a7b8c9d0-e1f2-3456-abcd-567890123456",
			CustomTitle:  "Fix memory leak in worker pool",
			FirstPrompt:  "there's a memory leak in the worker pool, goroutines aren't being cleaned up",
			MessageCount: 38,
			InputTokens:  145000,
			OutputTokens: 102000,
			CacheRead:    380000,
			CacheCreate:  48000,
			Created:      now.Add(-48 * time.Hour),
			Modified:     now.Add(-24 * time.Hour),
			FileMtime:    time.Time{},
			ProjectPath:  "~/Projects/realtime-api",
			Dir:          "/demo/.claude/projects/-Projects-realtime-api",
			GitBranch:    "fix/goroutine-leak",
			Status:       StatusArchived,
		},
		{
			SessionID:    "b8c9d0e1-f2a3-4567-bcde-678901234567",
			Summary:      "Design REST API endpoints",
			FirstPrompt:  "design RESTful API endpoints for user management with proper error handling",
			MessageCount: 42,
			InputTokens:  158000,
			OutputTokens: 118000,
			CacheRead:    420000,
			CacheCreate:  55000,
			Created:      now.Add(-72 * time.Hour),
			Modified:     now.Add(-48 * time.Hour),
			FileMtime:    time.Time{},
			ProjectPath:  "~/Projects/api-gateway",
			Dir:          "/demo/.claude/projects/-Projects-api-gateway",
			GitBranch:    "main",
			Status:       StatusArchived,
		},
		{
			SessionID:    "c9d0e1f2-a3b4-5678-cdef-789012345678",
			CustomTitle:  "Optimize Docker build",
			FirstPrompt:  "optimize the Dockerfile for smaller image size and faster builds",
			MessageCount: 11,
			InputTokens:  28000,
			OutputTokens: 19000,
			CacheRead:    72000,
			CacheCreate:  9000,
			Created:      now.Add(-5 * time.Hour),
			Modified:     now.Add(-4 * time.Hour),
			FileMtime:    now.Add(-4 * time.Hour),
			ProjectPath:  "~/Projects/c9s",
			Dir:          "/demo/.claude/projects/-Projects-c9s",
			GitBranch:    "chore/docker",
			Status:       StatusResumable,
		},
		{
			SessionID:    "d0e1f2a3-b4c5-6789-defa-890123456789",
			Summary:      "Write project documentation",
			FirstPrompt:  "help me write comprehensive README and API documentation",
			MessageCount: 26,
			InputTokens:  78000,
			OutputTokens: 62000,
			CacheRead:    200000,
			CacheCreate:  28000,
			Created:      now.Add(-8 * time.Hour),
			Modified:     now.Add(-7 * time.Hour),
			FileMtime:    time.Time{},
			ProjectPath:  "~/Projects/api-gateway",
			Dir:          "/demo/.claude/projects/-Projects-api-gateway",
			GitBranch:    "docs/readme",
			Status:       StatusArchived,
		},
		{
			SessionID:    "e1f2a3b4-c5d6-7890-efab-901234567890",
			CustomTitle:  "Add Redis caching layer",
			FirstPrompt:  "implement Redis caching for the most frequently accessed API endpoints",
			MessageCount: 33,
			InputTokens:  112000,
			OutputTokens: 85000,
			CacheRead:    290000,
			CacheCreate:  38000,
			Created:      now.Add(-36 * time.Hour),
			Modified:     now.Add(-30 * time.Hour),
			FileMtime:    now.Add(-30 * time.Hour),
			ProjectPath:  "~/Projects/webapp",
			Dir:          "/demo/.claude/projects/-Projects-webapp",
			GitBranch:    "feat/redis-cache",
			Status:       StatusResumable,
		},
		{
			SessionID:    "f2a3b4c5-d6e7-8901-fabc-012345678901",
			Summary:      "Setup monitoring with Prometheus",
			FirstPrompt:  "set up Prometheus metrics and Grafana dashboards for the API",
			MessageCount: 19,
			InputTokens:  52000,
			OutputTokens: 38000,
			CacheRead:    140000,
			CacheCreate:  18000,
			Created:      now.Add(-96 * time.Hour),
			Modified:     now.Add(-72 * time.Hour),
			FileMtime:    time.Time{},
			ProjectPath:  "~/Projects/realtime-api",
			Dir:          "/demo/.claude/projects/-Projects-realtime-api",
			GitBranch:    "feat/monitoring",
			Status:       StatusArchived,
		},
	}
}

// DemoUsageHistory returns fake usage history data for demo/screenshot purposes.
func DemoUsageHistory() []UsageDataPoint {
	now := time.Now().UTC()
	var points []UsageDataPoint

	// Generate 14 days of data, 3 points per day (every ~5 hours).
	for d := 13; d >= 0; d-- {
		for h := 0; h < 3; h++ {
			t := now.Add(-time.Duration(d)*24*time.Hour + time.Duration(h*8)*time.Hour)

			// Simulate realistic usage patterns: busier during weekdays.
			dayOfWeek := t.Weekday()
			weekday := dayOfWeek != time.Saturday && dayOfWeek != time.Sunday
			baseFive := 15.0 + float64(d%3)*10
			baseSeven := 8.0 + float64(d%5)*4
			if weekday {
				baseFive += 20
				baseSeven += 10
			}
			// Add variation per point within the day.
			baseFive += float64(h) * 8
			baseSeven += float64(h) * 3
			if baseFive > 95 {
				baseFive = 95
			}
			if baseSeven > 60 {
				baseSeven = 60
			}

			tokens := 150000 + d*25000 + h*50000
			opusTokens := int(float64(tokens) * 0.85)
			sonnetTokens := tokens - opusTokens

			points = append(points, UsageDataPoint{
				Time:     t,
				FiveHour: baseFive,
				SevenDay: baseSeven,
				Tokens:   tokens,
				Models: map[string]int{
					"claude-opus-4-6":   opusTokens,
					"claude-sonnet-4-6": sonnetTokens,
				},
			})
		}
	}
	return points
}
