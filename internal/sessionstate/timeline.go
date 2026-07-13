package sessionstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TimelineEntry is one event recorded in ~/.c9s/timeline/<id>.jsonl.
// Fields are deliberately permissive so callers can attach whatever
// payload makes sense for the event (tool name, message, usage delta).
type TimelineEntry struct {
	SessionID string          `json:"session_id"`
	Timestamp time.Time       `json:"ts"`
	Event     string          `json:"event"`
	Tool      string          `json:"tool,omitempty"`
	Message   string          `json:"message,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

func timelineDir() string {
	if DirOverride != "" {
		return filepath.Join(filepath.Dir(DirOverride), "timeline")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".c9s", "timeline")
}

func timelinePath(sessionID string) (string, error) {
	if !validSessionID.MatchString(sessionID) || len(sessionID) > 128 {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(timelineDir(), sessionID+".jsonl"), nil
}

// Append writes a single TimelineEntry to the per-session JSONL file. Cheap
// (one open-append-close per call) and best-effort: failure returns an error
// but doesn't panic.
func Append(entry TimelineEntry) error {
	if entry.SessionID == "" {
		return errors.New("timeline: missing session_id")
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	p, err := timelinePath(entry.SessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// RemoveTimeline deletes the per-session JSONL file. Called from SessionEnd
// alongside Remove() if the user wants ephemeral timelines.
func RemoveTimeline(sessionID string) error {
	p, err := timelinePath(sessionID)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
