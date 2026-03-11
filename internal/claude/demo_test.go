package claude

import "testing"

func TestDemoSessions(t *testing.T) {
	sessions := DemoSessions()

	if len(sessions) == 0 {
		t.Fatal("DemoSessions returned empty slice")
	}

	// Check we have a mix of statuses.
	statuses := make(map[Status]int)
	for _, s := range sessions {
		statuses[s.Status]++

		if s.SessionID == "" {
			t.Error("session has empty SessionID")
		}
		if s.DisplayName() == "" {
			t.Errorf("session %s has empty display name", s.SessionID)
		}
		if s.ProjectPath == "" {
			t.Errorf("session %s has empty ProjectPath", s.SessionID)
		}
		if s.TotalTokens() == 0 {
			t.Errorf("session %s has zero tokens", s.SessionID)
		}
	}

	for _, status := range []Status{StatusActive, StatusResumable, StatusArchived} {
		if statuses[status] == 0 {
			t.Errorf("no sessions with status %s", status)
		}
	}
}
