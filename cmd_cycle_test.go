package main

import "testing"

func TestParseCycleWindows(t *testing.T) {
	// Three windows: dashboard (skipped), one waiting, one done. The state
	// strings include tmux format directives — we should still extract the
	// keyword.
	in := "@1\tdashboard\t\n" +
		"@2\tmy-session\t#[fg=cyan]● waiting#[default]\n" +
		"@3\tother-session\t#[fg=green]● done#[default]\n"

	got := parseCycleWindows(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 windows (dashboard skipped), got %d: %+v", len(got), got)
	}
	if got[0].Name != "my-session" || got[0].State != "waiting" {
		t.Errorf("window[0] = %+v, want my-session/waiting", got[0])
	}
	if got[1].Name != "other-session" || got[1].State != "done" {
		t.Errorf("window[1] = %+v, want other-session/done", got[1])
	}
}

func TestParseCycleWindowsEmptyState(t *testing.T) {
	// A window whose @c9s-state isn't set yet has an empty third column.
	in := "@1\tmy-session\t\n"
	got := parseCycleWindows(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 window, got %d", len(got))
	}
	if got[0].State != "" {
		t.Errorf("unset state should be empty, got %q", got[0].State)
	}
	// Empty state maps to "unknown" priority bucket.
	if statePriority(got[0].State) != 3 {
		t.Errorf("empty state priority = %d, want 3 (unknown)", statePriority(got[0].State))
	}
}

func TestStatePriorityOrder(t *testing.T) {
	cases := []struct {
		state string
		want  int
	}{
		{"waiting", 0},
		{"processing", 1},
		{"done", 2},
		{"unknown", 3},
		{"", 3},
		{"anything-else", 3},
	}
	for _, c := range cases {
		if got := statePriority(c.state); got != c.want {
			t.Errorf("statePriority(%q) = %d, want %d", c.state, got, c.want)
		}
	}
}

func TestExtractStateKeyword(t *testing.T) {
	cases := []struct {
		badge string
		want  string
	}{
		{"#[fg=cyan]● waiting#[default]", "waiting"},
		{"#[fg=green]● done#[default]", "done"},
		{"● processing", "processing"},
		{"#[fg=#555]● unknown#[default]", "unknown"},
		{"", ""},
		{"garbage", ""},
	}
	for _, c := range cases {
		if got := extractStateKeyword(c.badge); got != c.want {
			t.Errorf("extractStateKeyword(%q) = %q, want %q", c.badge, got, c.want)
		}
	}
}
