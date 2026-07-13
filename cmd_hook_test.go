package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stefanoguerrini/c9s/internal/sessionstate"
)

func TestHookPayload_ModelAcceptsStringOrObject(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantID      string
		wantDisplay string
	}{
		{
			name:   "bare string (SessionStart shape)",
			input:  `{"session_id":"abc","model":"claude-opus-4-7"}`,
			wantID: "claude-opus-4-7",
		},
		{
			name:        "object with id and display_name (Stop shape)",
			input:       `{"session_id":"abc","model":{"id":"claude-opus-4-7","display_name":"Opus 4.7"}}`,
			wantID:      "claude-opus-4-7",
			wantDisplay: "Opus 4.7",
		},
		{
			name:  "null model",
			input: `{"session_id":"abc","model":null}`,
		},
		{
			name:  "missing model",
			input: `{"session_id":"abc"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p hookPayload
			if err := json.Unmarshal([]byte(tc.input), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.Model.ID != tc.wantID {
				t.Errorf("Model.ID = %q, want %q", p.Model.ID, tc.wantID)
			}
			if p.Model.DisplayName != tc.wantDisplay {
				t.Errorf("Model.DisplayName = %q, want %q", p.Model.DisplayName, tc.wantDisplay)
			}
		})
	}
}

func TestFriendlyModelName(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-7":           "opus 4.7",
		"claude-sonnet-4-6":         "sonnet 4.6",
		"claude-haiku-4-5-20251001": "haiku 4.5",
		"claude-foo":                "claude-foo", // not enough parts → passthrough
		"some-other-model":          "some-other-model",
		"":                          "",
	}
	for in, want := range cases {
		if got := friendlyModelName(in); got != want {
			t.Errorf("friendlyModelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotificationBody_AllSignals(t *testing.T) {
	now := time.Date(2026, 5, 22, 19, 30, 0, 0, time.UTC)
	p := hookPayload{
		Message:  "Claude needs your permission to use Bash",
		ToolName: "Bash",
	}
	p.Model.ID = "claude-opus-4-7"
	info := sessionstate.Info{
		Model:             "claude-opus-4-7",
		LastTool:          "Read",
		LastTurnStartedAt: now.Add(-90 * time.Second),
	}
	got := notificationBody(p, info, now)
	for _, want := range []string{
		"Claude needs your permission to use Bash",
		"opus 4.7",
		"tool: Bash", // hook payload tool wins over state cache
		"waiting 1m30s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q\ngot: %q", want, got)
		}
	}
}

func TestNotificationBody_MinimalSignals(t *testing.T) {
	got := notificationBody(hookPayload{Message: "Claude is waiting for your input"}, sessionstate.Info{}, time.Now())
	if got != "Claude is waiting for your input" {
		t.Errorf("expected message-only body, got %q", got)
	}
}

func TestNotificationBody_EmptyMessageFallback(t *testing.T) {
	got := notificationBody(hookPayload{}, sessionstate.Info{}, time.Now())
	if !strings.Contains(got, "Claude needs your attention") {
		t.Errorf("expected fallback line, got %q", got)
	}
}

func TestNotificationBody_StatePopulatesWhenPayloadEmpty(t *testing.T) {
	info := sessionstate.Info{Model: "claude-sonnet-4-6", LastTool: "Edit"}
	got := notificationBody(hookPayload{Message: "msg"}, info, time.Now())
	if !strings.Contains(got, "sonnet 4.6") || !strings.Contains(got, "tool: Edit") {
		t.Errorf("expected fallback to state values, got %q", got)
	}
}
