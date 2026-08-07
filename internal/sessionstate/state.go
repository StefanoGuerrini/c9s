// Package sessionstate persists per-session live state emitted by Claude Code
// hooks. Each running session has a JSON file at ~/.c9s/state/<session_id>.json
// that the dashboard reads on every tick to display processing/waiting/done.
package sessionstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// State enumerates the possible lifecycle states for a session.
type State string

const (
	StateProcessing State = "processing"
	StateWaiting    State = "waiting"
	StateDone       State = "done"
	StateEnded      State = "ended"
	StateUnknown    State = "unknown"
)

// Info is the on-disk record. Hook events update State; statusLine ticks
// refresh the metadata fields without touching State.
type Info struct {
	SessionID      string    `json:"session_id"`
	State          State     `json:"state"`
	UpdatedAt      time.Time `json:"updated_at"`
	Model          string    `json:"model,omitempty"`
	Cwd            string    `json:"cwd,omitempty"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	OutputStyle    string    `json:"output_style,omitempty"`
	LastEvent      string    `json:"last_event,omitempty"`

	// Token counts, mined from hook payloads (PostToolUse/Stop). Lets the
	// dashboard avoid polling the session JSONL file every tick.
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheRead    int `json:"cache_read_tokens,omitempty"`
	CacheCreate  int `json:"cache_create_tokens,omitempty"`

	// Conversation metrics, updated from hook events.
	MessageCount        int       `json:"message_count,omitempty"`
	LastTurnStartedAt   time.Time `json:"last_turn_started_at,omitempty"`
	LastTurnFinishedAt  time.Time `json:"last_turn_finished_at,omitempty"`
	LastNotification    string    `json:"last_notification,omitempty"`
	LastNotificationAt  time.Time `json:"last_notification_at,omitempty"`

	// Tool tracking (PreToolUse / PostToolUse). LastTool is the name of the
	// tool currently running (empty between turns or when claude isn't mid-
	// tool). LastToolStartedAt is set on PreToolUse and consumed by
	// PostToolUse to record duration.
	LastTool          string    `json:"last_tool,omitempty"`
	LastToolStartedAt time.Time `json:"last_tool_started_at,omitempty"`
	LastToolDuration  string    `json:"last_tool_duration,omitempty"` // human-readable, e.g. "1.2s"

	// CompactingSoon is set by PreCompact and cleared on the next user
	// turn. Dashboard surfaces it as a ⚠ compact indicator.
	CompactingSoon bool `json:"compacting_soon,omitempty"`

	// TmuxPane is the value of $TMUX_PANE at the last hook fire -- the pane id
	// (e.g. "%23") the session was attached to when the hook ran. This is the
	// authoritative signal for "which window is running this session id right
	// now", because it re-anchors even after /resume, /clear, compact, or
	// --session-id switches inside the pane. Empty when the hook didn't run
	// under tmux, or when hooks aren't installed.
	TmuxPane string `json:"tmux_pane,omitempty"`
}

// PaneStaleAfter bounds how long a state file's TmuxPane claim stays
// authoritative. tmux reissues pane IDs (e.g. "%5") from zero every time its
// server restarts (which happens whenever the c9s tmux session is killed),
// so a long-dead session's leftover file can otherwise resurrect and claim a
// pane that now belongs to a brand-new, unrelated session -- SessionEnd
// doesn't fire on a forced kill, so nothing clears the old claim on its own.
// Any pane still genuinely in use gets its claim refreshed well within this
// window by the next hook event, so the cutoff never affects a live pane.
const PaneStaleAfter = 6 * time.Hour

// DirOverride lets tests redirect state file reads/writes.
var DirOverride string

var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func dir() string {
	if DirOverride != "" {
		return DirOverride
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".c9s", "state")
}

func pathFor(sessionID string) (string, error) {
	if !validSessionID.MatchString(sessionID) || len(sessionID) > 128 {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(dir(), sessionID+".json"), nil
}

// Read returns the persisted info for a session, or an error if the file is
// absent or unreadable. Callers should treat any error as "no info available"
// and render an unknown badge.
func Read(sessionID string) (Info, error) {
	p, err := pathFor(sessionID)
	if err != nil {
		return Info{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

// SetState writes a fresh state for a session, preserving metadata fields when
// the existing file is readable. Used by lifecycle hooks (SessionStart, Stop,
// Notification, etc.).
func SetState(sessionID string, state State, event string) error {
	cur, _ := Read(sessionID)
	cur.SessionID = sessionID
	cur.State = state
	cur.UpdatedAt = time.Now().UTC()
	cur.LastEvent = event
	return write(cur)
}

// UpdateMetadata refreshes metadata fields (model, cost, cwd, transcript path,
// output style) without changing the state. Used by the statusLine emitter,
// which ticks frequently and must not overwrite the lifecycle state.
func UpdateMetadata(sessionID string, m Metadata) error {
	cur, _ := Read(sessionID)
	cur.SessionID = sessionID
	if cur.State == "" {
		cur.State = StateUnknown
	}
	cur.UpdatedAt = time.Now().UTC()
	if m.Model != "" {
		cur.Model = m.Model
	}
	if m.Cwd != "" {
		cur.Cwd = m.Cwd
	}
	if m.TranscriptPath != "" {
		cur.TranscriptPath = m.TranscriptPath
	}
	if m.CostUSD > 0 {
		cur.CostUSD = m.CostUSD
	}
	if m.OutputStyle != "" {
		cur.OutputStyle = m.OutputStyle
	}
	cur.LastEvent = "statusLine"
	return write(cur)
}

// Metadata carries the fields the statusLine command can refresh.
type Metadata struct {
	Model          string
	Cwd            string
	TranscriptPath string
	CostUSD        float64
	OutputStyle    string
}

// Patch applies a read-modify-write update to a session's state file. The
// callback receives the current Info (zero-valued when no file exists yet)
// and mutates it in place. Patch then writes the result atomically and
// updates UpdatedAt. Use Patch when an event needs to update multiple
// fields conditionally; use SetState / UpdateMetadata for the common
// single-purpose cases.
func Patch(sessionID string, fn func(*Info)) error {
	cur, _ := Read(sessionID)
	cur.SessionID = sessionID
	fn(&cur)
	cur.UpdatedAt = time.Now().UTC()
	return write(cur)
}

// PaneSessionMap returns paneID → sessionID drawn from every state file that
// records a TmuxPane. When two sessions claim the same pane (e.g. one ended
// but its file lingers), the freshest UpdatedAt wins -- that's the session
// currently attached. Claims older than PaneStaleAfter are ignored entirely:
// a live pane's claim is refreshed by hooks far more often than that, so a
// surviving old claim is always a leftover from a session that's gone (a
// crash, `kill -9`, or an external `tmux kill-session` bypassing c9s's own
// cleanup) rather than a legitimately quiet one. Empty map if the state dir
// doesn't exist yet.
func PaneSessionMap() map[string]string {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return map[string]string{}
	}
	cutoff := time.Now().Add(-PaneStaleAfter)
	type winner struct {
		sessionID string
		updatedAt time.Time
	}
	best := make(map[string]winner)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		sid := e.Name()[:len(e.Name())-len(".json")]
		info, err := Read(sid)
		if err != nil || info.TmuxPane == "" || info.UpdatedAt.Before(cutoff) {
			continue
		}
		if cur, ok := best[info.TmuxPane]; !ok || info.UpdatedAt.After(cur.updatedAt) {
			best[info.TmuxPane] = winner{sessionID: sid, updatedAt: info.UpdatedAt}
		}
	}
	out := make(map[string]string, len(best))
	for pane, w := range best {
		out[pane] = w.sessionID
	}
	return out
}

// PurgeStale deletes state files whose UpdatedAt is older than maxAge.
// Ungracefully-killed sessions (e.g. `q` tearing down the whole tmux server)
// never run their SessionEnd hook, so their file lingers forever with a
// TmuxPane value that tmux will happily hand to an unrelated pane after a
// server restart -- PaneSessionMap would then misattribute that pane to the
// long-dead session. Called on dashboard startup; safe to run anytime since
// a live session's hooks refresh UpdatedAt continuously. Returns the number
// of files removed.
func PurgeStale(maxAge time.Duration) int {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		sid := e.Name()[:len(e.Name())-len(".json")]
		info, err := Read(sid)
		if err != nil || info.UpdatedAt.After(cutoff) {
			continue
		}
		if Remove(sid) == nil {
			removed++
		}
	}
	return removed
}

// Remove deletes the state file for a session. Used by SessionEnd.
func Remove(sessionID string) error {
	p, err := pathFor(sessionID)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func write(info Info) error {
	p, err := pathFor(info.SessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, p)
}
