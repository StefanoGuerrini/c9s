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
	if SessionName != "c9s" {
		t.Errorf("SessionName = %q, want %q", SessionName, "c9s")
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

func TestClassifyPrompt(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantAtMain bool
	}{
		{
			"at main prompt (real capture)",
			"⏺ Could you be more specific?\n\n" +
				"───────────────────────────────────\n" +
				"❯ \n" +
				"───────────────────────────────────\n" +
				"  -- INSERT --\n\n\n",
			true,
		},
		{
			"at main prompt with trailing blanks",
			"Done!\n" +
				"─────\n" +
				"❯\n" +
				"─────\n" +
				"  -- INSERT --\n\n\n\n\n\n",
			true,
		},
		{
			"at main prompt NORMAL mode",
			"output\n" +
				"───────\n" +
				"❯ some text\n" +
				"───────\n" +
				"  -- NORMAL --\n",
			true,
		},
		{
			"tool approval prompt (not main)",
			"⏺ I need to run this command:\n" +
				"  git status\n\n" +
				"  Allow  Deny\n\n",
			false,
		},
		{
			"processing output (not main)",
			"Here is the code:\n```go\nfunc main() {\n",
			false,
		},
		{
			"user message echo ❯ not at prompt",
			"❯ do something\n\n⏺ Working on it...\nEditing files...\n",
			false,
		},
		{
			"empty content",
			"\n\n\n",
			false,
		},
		{
			"question from claude (not main)",
			"⏺ Which approach would you prefer?\n" +
				"  1. Option A\n" +
				"  2. Option B\n\n",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPrompt(tt.content); got != tt.wantAtMain {
				t.Errorf("classifyPrompt() = %v, want %v", got, tt.wantAtMain)
			}
		})
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

func TestIsBoxLine(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"───────────", true},
		{"━━━━━━━━━", true},
		{"── hello ──", false},
		{"", false},
		{"regular text", false},
	}
	for _, tt := range tests {
		if got := isBoxLine(tt.input); got != tt.want {
			t.Errorf("isBoxLine(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
