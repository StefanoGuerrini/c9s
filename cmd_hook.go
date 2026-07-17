package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stefanoguerrini/c9s/internal/config"
	"github.com/stefanoguerrini/c9s/internal/notify"
	"github.com/stefanoguerrini/c9s/internal/sessionstate"
)

// notifyThrottle suppresses duplicate notifications for the same session
// fired within this window. Claude can emit several Notification events in
// rapid succession (e.g. tool approval bursts) and we don't want to
// hammer the OS notification system.
const notifyThrottle = 5 * time.Second

// hookPayload captures every field c9s mines from Claude Code hook events.
// Different events carry different subsets; absent fields stay zero-valued.
// See https://code.claude.com/docs/en/hooks for the canonical schema.
type hookPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`

	// Notification carries a human-readable reason string in this field.
	Message string `json:"message"`

	// PreToolUse / PostToolUse describe the tool invocation.
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`

	// Model identity is sent on most events. Claude Code sends it as either
	// an object {id, display_name} (e.g. Stop) or a bare string (e.g.
	// SessionStart); modelField accepts both.
	Model modelField `json:"model"`

	// Cost / usage totals -- present on Stop and statusLine payloads.
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`

	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// modelField holds the model identity from a hook payload. Claude Code sends
// this field as either an object ({id, display_name}) or a bare string
// depending on the event, so we accept both shapes.
type modelField struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

func (m *modelField) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		m.ID = s
		return nil
	}
	type raw modelField
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*m = modelField(r)
	return nil
}

// runHook handles `c9s _hook <event>`. It reads a JSON payload from stdin and
// updates the per-session state file. Subcommand names mirror Claude Code's
// hook event names, lower-kebab-case.
func runHook(event string) int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c9s _hook %s: read stdin: %v\n", event, err)
		return 1
	}
	var p hookPayload
	if len(data) > 0 {
		if err := json.Unmarshal(data, &p); err != nil {
			fmt.Fprintf(os.Stderr, "c9s _hook %s: parse json: %v\n", event, err)
			return 1
		}
	}
	if p.SessionID == "" {
		// Without a session_id we can't address a state file. Silent success
		// so we don't spam the user's terminal with hook errors.
		return 0
	}

	if event == "session-end" {
		_ = sessionstate.Append(sessionstate.TimelineEntry{
			SessionID: p.SessionID,
			Event:     "SessionEnd",
		})
		if err := sessionstate.Remove(p.SessionID); err != nil {
			fmt.Fprintf(os.Stderr, "c9s _hook %s: %v\n", event, err)
			return 1
		}
		return 0
	}

	claudeEvent := claudeEventName(event)
	if claudeEvent == "" {
		// Unrecognized event; silent success.
		return 0
	}
	// Best-effort: append an event row to the per-session timeline. Failures
	// are swallowed so a missing or unwriteable timeline file never blocks
	// the lifecycle path.
	_ = sessionstate.Append(sessionstate.TimelineEntry{
		SessionID: p.SessionID,
		Event:     claudeEvent,
		Tool:      p.ToolName,
		Message:   p.Message,
	})
	// Lifecycle events change State; mid-turn events (PreToolUse,
	// PostToolUse, PreCompact) only enrich metadata.
	state, lifecycle := lifecycleStateFor(event)

	now := time.Now().UTC()
	prev, _ := sessionstate.Read(p.SessionID) // for notification throttle
	err = sessionstate.Patch(p.SessionID, func(info *sessionstate.Info) {
		if lifecycle {
			info.State = state
		}
		info.LastEvent = claudeEvent

		// Identity / context -- always refreshed when present.
		if p.Cwd != "" {
			info.Cwd = p.Cwd
		}
		if p.TranscriptPath != "" {
			info.TranscriptPath = p.TranscriptPath
		}
		if name := modelName(p); name != "" {
			info.Model = name
		}
		// Anchor the session to its tmux pane so the dashboard can retag a
		// window whose managed session id drifts (from /resume, /clear,
		// compact, or --session-id changes inside the pane).
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			info.TmuxPane = pane
		}

		// Per-event enrichment.
		switch claudeEvent {
		case "UserPromptSubmit":
			info.LastTurnStartedAt = now
			info.MessageCount++
			info.CompactingSoon = false // user moved on; warning stale
		case "Stop":
			info.LastTurnFinishedAt = now
			info.MessageCount++ // assistant's reply counts too
			info.LastTool = ""  // turn ended; clear any in-flight tool
			if p.Usage.InputTokens > 0 {
				info.InputTokens += p.Usage.InputTokens
			}
			if p.Usage.OutputTokens > 0 {
				info.OutputTokens += p.Usage.OutputTokens
			}
			if p.Usage.CacheReadInputTokens > 0 {
				info.CacheRead += p.Usage.CacheReadInputTokens
			}
			if p.Usage.CacheCreationInputTokens > 0 {
				info.CacheCreate += p.Usage.CacheCreationInputTokens
			}
			if p.Cost.TotalCostUSD > 0 {
				info.CostUSD = p.Cost.TotalCostUSD
			}
		case "Notification":
			if p.Message != "" {
				info.LastNotification = p.Message
				info.LastNotificationAt = now
			}
		case "PreToolUse":
			if p.ToolName != "" {
				info.LastTool = p.ToolName
				info.LastToolStartedAt = now
			}
		case "PostToolUse":
			if !info.LastToolStartedAt.IsZero() {
				info.LastToolDuration = formatDuration(now.Sub(info.LastToolStartedAt))
			}
			info.LastTool = ""
			info.LastToolStartedAt = time.Time{}
		case "PreCompact":
			info.CompactingSoon = true
		}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "c9s _hook %s: %v\n", event, err)
		return 1
	}

	// Side effect: desktop notification when claude is asking for attention.
	if claudeEvent == "Notification" && p.Message != "" {
		if now.Sub(prev.LastNotificationAt) >= notifyThrottle {
			maybeNotify(p)
		}
	}
	return 0
}

// maybeNotify dispatches a desktop notification iff the user hasn't opted
// out via `notifications: off` in config, and additionally pushes to the
// configured smartphone provider (ntfy) when set. Reads config every call
// (cheap -- it's a single JSON file) so the toggles take effect without
// restarting claude / c9s.
func maybeNotify(p hookPayload) {
	cfg := config.Load()
	title := "claude waiting"
	if label := sessionLabel(p); label != "" {
		title = label
	}
	info, _ := sessionstate.Read(p.SessionID)
	body := notificationBody(p, info, time.Now())
	if cfg.Notifications != "off" {
		notify.Notify(title, body)
	}
	if err := notify.Push(notify.PushOptions{
		Provider: cfg.NotifyPush,
		URL:      cfg.NotifyPushURL,
		Topic:    cfg.NotifyPushTopic,
		Token:    cfg.NotifyPushToken,
		User:     cfg.NotifyPushUser,
		Password: cfg.NotifyPushPass,
	}, title, body); err != nil {
		fmt.Fprintf(os.Stderr, "c9s _hook notification: push: %v\n", err)
	}
}

// notificationBody assembles a richer body for the desktop / push
// notification by pairing Claude's short reason with whatever context we
// have in the per-session state file: model, currently-pending tool, and
// how long the turn has been running. Lines are added only when the data
// is present so the body stays readable on a lock-screen banner.
func notificationBody(p hookPayload, info sessionstate.Info, now time.Time) string {
	var lines []string
	if p.Message != "" {
		lines = append(lines, p.Message)
	} else {
		lines = append(lines, "Claude needs your attention")
	}

	var meta []string
	if model := preferred(modelName(p), info.Model); model != "" {
		meta = append(meta, friendlyModelName(model))
	}
	if tool := preferred(p.ToolName, info.LastTool); tool != "" {
		meta = append(meta, "tool: "+tool)
	}
	if !info.LastTurnStartedAt.IsZero() {
		if d := now.Sub(info.LastTurnStartedAt); d > 0 {
			meta = append(meta, "waiting "+formatDuration(d))
		}
	}
	if len(meta) > 0 {
		lines = append(lines, strings.Join(meta, " · "))
	}
	return strings.Join(lines, "\n")
}

// preferred returns the first non-empty argument. Used to take fresh hook
// payload data over the (possibly stale) value cached in the state file.
func preferred(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// friendlyModelName strips the noisy "claude-" prefix and date suffixes so
// "claude-opus-4-7" / "claude-haiku-4-5-20251001" both render as "opus 4.7"
// / "haiku 4.5". Falls back to the raw value if it doesn't match the
// expected shape.
func friendlyModelName(model string) string {
	m := strings.TrimPrefix(model, "claude-")
	parts := strings.Split(m, "-")
	if len(parts) < 3 {
		return model
	}
	// parts[0] = family, parts[1] = major, parts[2] = minor, … rest is date.
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return model
	}
	if _, err := strconv.Atoi(parts[2]); err != nil {
		return model
	}
	return fmt.Sprintf("%s %s.%s", parts[0], parts[1], parts[2])
}

// sessionLabel returns a short identifier for the session -- preferring the
// cwd basename (project name) so the notification has context, falling
// back to the model name or session id stub.
func sessionLabel(p hookPayload) string {
	if p.Cwd != "" {
		return filepath.Base(p.Cwd)
	}
	if m := modelName(p); m != "" {
		return m
	}
	if len(p.SessionID) > 8 {
		return p.SessionID[:8]
	}
	return p.SessionID
}

// modelName picks the friendlier of display_name / id from the payload.
func modelName(p hookPayload) string {
	if p.Model.DisplayName != "" {
		return p.Model.DisplayName
	}
	return p.Model.ID
}

// claudeEventName converts the c9s subcommand name (lower-kebab-case) to the
// canonical Claude Code event name (PascalCase). Returns "" for unrecognized
// events.
func claudeEventName(event string) string {
	switch event {
	case "session-start":
		return "SessionStart"
	case "user-prompt-submit":
		return "UserPromptSubmit"
	case "stop":
		return "Stop"
	case "notification":
		return "Notification"
	case "pre-tool-use":
		return "PreToolUse"
	case "post-tool-use":
		return "PostToolUse"
	case "pre-compact":
		return "PreCompact"
	}
	return ""
}

// lifecycleStateFor returns the lifecycle State a hook event implies. The
// second return value reports whether the event is a lifecycle event at all;
// mid-turn events like PreToolUse return (StateUnknown, false) and leave the
// existing state untouched.
func lifecycleStateFor(event string) (sessionstate.State, bool) {
	switch event {
	case "session-start", "user-prompt-submit":
		return sessionstate.StateProcessing, true
	case "stop":
		return sessionstate.StateDone, true
	case "notification":
		return sessionstate.StateWaiting, true
	}
	return sessionstate.StateUnknown, false
}

// formatDuration renders a Duration as a short human-readable string suitable
// for the dashboard preview ("0.4s", "3.2s", "1m02s").
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm%02ds", m, s)
}
