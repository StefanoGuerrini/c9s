package notify

import "testing"

func TestEscapeAppleScript(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{`with "quotes"`, `with \"quotes\"`},
		{`back\slash`, `back\\slash`},
		{`mixed "and\both"`, `mixed \"and\\both\"`},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeAppleScript(c.in); got != c.want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeSingleQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"it's", "it''s"},
		{"'both'", "''both''"},
	}
	for _, c := range cases {
		if got := escapeSingleQuote(c.in); got != c.want {
			t.Errorf("escapeSingleQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNotifyEmptyIsSilent(t *testing.T) {
	// Should not panic / not invoke backends with empty input.
	Notify("", "")
}
