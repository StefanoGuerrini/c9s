package tmux

import (
	"testing"
)

func TestAvailable(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
}

func TestInSession(t *testing.T) {
	_ = InSession()
}

func TestSessionName(t *testing.T) {
	saved := SessionName
	t.Cleanup(func() { SessionName = saved })
	if SessionName != "c9s" {
		t.Errorf("SessionName = %q, want %q", SessionName, "c9s")
	}
	SetCurrentSession("c9s-7")
	if CurrentSession() != "c9s-7" {
		t.Errorf("CurrentSession() = %q, want %q", CurrentSession(), "c9s-7")
	}
	SetCurrentSession("")
	if CurrentSession() != "c9s-7" {
		t.Errorf("SetCurrentSession(\"\") should be a no-op, got %q", CurrentSession())
	}
}

func TestIsC9sSessionName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"c9s", true},
		{"c9s-2", true},
		{"c9s-12", true},
		{"c9s-", false},
		{"c9s-a", false},
		{"c9stuff", false},
		{"", false},
		{"other", false},
	}
	for _, c := range cases {
		if got := isC9sSessionName(c.in); got != c.want {
			t.Errorf("isC9sSessionName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPickSessionName(t *testing.T) {
	savedSessions := listSessionsFn
	savedClients := listClientsFn
	t.Cleanup(func() {
		listSessionsFn = savedSessions
		listClientsFn = savedClients
	})

	tests := []struct {
		name     string
		sessions []string
		clients  map[string]string // session name → list-clients output
		force    bool
		want     string
	}{
		{
			name:     "no sessions yields c9s",
			sessions: nil,
			want:     "c9s",
		},
		{
			name:     "idle c9s is reused",
			sessions: []string{"c9s", "other"},
			clients:  map[string]string{"c9s": ""},
			want:     "c9s",
		},
		{
			name:     "busy c9s forces c9s-2",
			sessions: []string{"c9s"},
			clients:  map[string]string{"c9s": "/dev/ttys001"},
			want:     "c9s-2",
		},
		{
			name:     "prefer idle c9s-3 over creating c9s-4",
			sessions: []string{"c9s", "c9s-2", "c9s-3"},
			clients: map[string]string{
				"c9s":   "/dev/ttys001",
				"c9s-2": "/dev/ttys002",
				"c9s-3": "",
			},
			want: "c9s-3",
		},
		{
			name:     "all busy creates next free name",
			sessions: []string{"c9s", "c9s-2"},
			clients: map[string]string{
				"c9s":   "/dev/ttys001",
				"c9s-2": "/dev/ttys002",
			},
			want: "c9s-3",
		},
		{
			name:     "force skips idle reuse",
			sessions: []string{"c9s"},
			clients:  map[string]string{"c9s": ""},
			force:    true,
			want:     "c9s-2",
		},
		{
			name:     "non-c9s sessions are ignored",
			sessions: []string{"work", "c9s-2"},
			clients:  map[string]string{"c9s-2": "/dev/ttys001"},
			want:     "c9s",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listSessionsFn = func() ([]string, error) { return tc.sessions, nil }
			listClientsFn = func(name string) (string, error) {
				return tc.clients[name], nil
			}
			if got := PickSessionName(tc.force); got != tc.want {
				t.Errorf("PickSessionName(force=%v) = %q, want %q", tc.force, got, tc.want)
			}
		})
	}
}

func TestListC9sSessions_Order(t *testing.T) {
	saved := listSessionsFn
	t.Cleanup(func() { listSessionsFn = saved })
	listSessionsFn = func() ([]string, error) {
		return []string{"work", "c9s-10", "c9s-2", "c9s", "other"}, nil
	}
	got := ListC9sSessions()
	want := []string{"c9s", "c9s-2", "c9s-10"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListC9sSessions()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDashboardWindow(t *testing.T) {
	if DashboardWindow != "dashboard" {
		t.Errorf("DashboardWindow = %q, want %q", DashboardWindow, "dashboard")
	}
}

func TestWindowInfo(t *testing.T) {
	w := WindowInfo{ID: "@1", Name: "test", Command: "claude"}
	if w.ID != "@1" || w.Name != "test" || w.Command != "claude" {
		t.Errorf("WindowInfo fields unexpected: %+v", w)
	}
}

func TestPaneStatusString(t *testing.T) {
	tests := []struct {
		s    PaneStatus
		want string
	}{
		{PaneProcessing, "processing"},
		{PaneWaiting, "waiting"},
		{PaneDone, "done"},
		{PaneUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("PaneStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestWindowExistsNonexistent(t *testing.T) {
	if WindowExists("nosession:nowindow.99") {
		t.Error("WindowExists returned true for nonexistent window")
	}
}

func TestRenameWindowNonexistent(t *testing.T) {
	// Calling RenameWindow on a nonexistent window should return an error.
	err := RenameWindow("nosession:nowindow.99", "newname")
	if err == nil {
		t.Error("expected error for nonexistent window")
	}
}

func TestParseTmuxVersionSupportsSync(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"tmux 3.6a", false},
		{"tmux 3.6", false},
		{"tmux 3.7", true},
		{"tmux 3.7a", true},
		{"tmux 4.0", true},
		{"tmux next-3.7", true},
		{"tmux 3.5", false},
		{"tmux 2.9a", false},
		{"", false},
		{"not-tmux", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := parseTmuxVersionSupportsSync(tt.version); got != tt.want {
				t.Errorf("parseTmuxVersionSupportsSync(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestShellQuoteJoin(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"/usr/bin/c9s"}, "'/usr/bin/c9s'"},
		{[]string{"/path with spaces/c9s", "--inside-tmux"}, "'/path with spaces/c9s' '--inside-tmux'"},
		{[]string{"it's", "a test"}, `'it'"'"'s' 'a test'`},
		{[]string{"/usr/bin/c9s", "--debug", "--demo"}, "'/usr/bin/c9s' '--debug' '--demo'"},
	}
	for _, tt := range tests {
		got := shellQuoteJoin(tt.args)
		if got != tt.want {
			t.Errorf("shellQuoteJoin(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

