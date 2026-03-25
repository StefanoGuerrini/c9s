package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Config holds all user-configurable settings.
type Config struct {
	Theme          string `json:"theme"`            // "default" or "custom"
	RefreshSeconds int    `json:"refresh_seconds"`  // dashboard refresh interval (default: 3)
	ScrollSpeed    int    `json:"scroll_speed"`     // lines per mouse scroll event (default: 3)
	WorkDir        string `json:"work_dir"`         // default working directory for new sessions (empty = cwd)
	KeepAlive      string `json:"keep_alive"`       // "off" (default) or "on" — keep sessions running on quit
	StatusUsage    string  `json:"status_usage"`     // "off", "percent" (default), "cost,percent", "all", etc.
	CostInput      float64 `json:"cost_input"`      // $/M input+cache_write tokens (default: 3.0 = Sonnet)
	CostOutput     float64 `json:"cost_output"`     // $/M output tokens (default: 15.0 = Sonnet)
	CostCache      float64 `json:"cost_cache"`      // $/M cache read tokens (default: 0.30 = Sonnet)
	UsageHistory   string  `json:"usage_history"`   // "on" (default) or "off" — record API usage over time
	Worktrees      string  `json:"worktrees"`       // "off" (default), "auto", "always"
	WorktreeExpand string `json:"worktree_expand"`  // "all" (default), "selected"
	Keys           Keys   `json:"keys"`
	Colors         Colors `json:"colors"`
	Dashboard      Dashboard `json:"dashboard"`     // persisted dashboard state
}

// Dashboard persists the last-used dashboard toggle states.
type Dashboard struct {
	ShowTokens       bool     `json:"show_tokens"`
	ShowPreview      bool     `json:"show_preview"`
	ShowWorktrees    bool     `json:"show_worktrees"`
	GroupBy          int      `json:"group_by"`           // 0=none, 1=project, 2=status
	ReplacedSessions []string `json:"replaced_sessions"`  // sessions hidden after fork/clear
}

// Keys defines tmux-level navigation keybindings.
// Values use tmux key syntax: "C-d" for Ctrl+d, "C-n" for Ctrl+n, etc.
type Keys struct {
	Dashboard   string `json:"dashboard"`    // return to dashboard (default: "C-d")
	NextSession string `json:"next_session"` // next session window (default: "C-n")
	PrevSession string `json:"prev_session"` // previous session window (default: "C-p")
}

// Colors defines the color scheme.
// Values can be ANSI color numbers ("10", "14") or hex codes ("#bb86fc").
type Colors struct {
	Title       string `json:"title"`        // dashboard title (default: "12")
	Header      string `json:"header"`       // column headers (default: "8")
	Selected    string `json:"selected"`     // selected row background (default: "237")
	Dim         string `json:"dim"`          // dimmed text (default: "240")
	GroupHeader string `json:"group_header"` // group header text (default: "11")
	HelpKey     string `json:"help_key"`     // keybinding highlights (default: "12")
	Help        string `json:"help"`         // help text (default: "8")
	Info        string `json:"info"`         // info messages (default: "10")
	Error       string `json:"error"`        // error messages (default: "9")

	// Session statuses
	Active    string `json:"active"`    // active session (default: "14")
	Idle      string `json:"idle"`      // idle session (default: "13")
	Resumable string `json:"resumable"` // resumable session (default: "10")
	Archived  string `json:"archived"`  // archived session (default: "240")

	// Pane statuses
	Processing string `json:"processing"` // processing (default: "14")
	Waiting    string `json:"waiting"`    // waiting for input (default: "11")
	Done       string `json:"done"`       // completed (default: "10")

	// Preview panel
	PreviewBorder string `json:"preview_border"` // border color (default: "237")
	PreviewLabel  string `json:"preview_label"`  // label color (default: "12")
	PreviewDim    string `json:"preview_dim"`    // dim text (default: "240")
	PreviewValue  string `json:"preview_value"`  // value text (default: "252")

	// tmux status bar
	StatusBg     string `json:"status_bg"`     // status bar background (default: "#1b1b2f")
	StatusFg     string `json:"status_fg"`     // status bar foreground (default: "#8888aa")
	StatusAccent string `json:"status_accent"` // c9s label color (default: "#bb86fc")
	StatusDim    string `json:"status_dim"`    // separator/hint color (default: "#555577")
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Theme:          "default",
		RefreshSeconds: 3,
		ScrollSpeed:    3,
		KeepAlive:      "off",
		StatusUsage:    "percent",
		CostInput:      3.0,
		CostOutput:     15.0,
		CostCache:      0.30,
		UsageHistory:   "on",
		Worktrees:      "off",
		WorktreeExpand: "all",
		Keys: Keys{
			Dashboard:   "C-d",
			NextSession: "C-n",
			PrevSession: "C-p",
		},
		Colors: Colors{
			Title:       "12",
			Header:      "8",
			Selected:    "237",
			Dim:         "240",
			GroupHeader: "11",
			HelpKey:     "12",
			Help:        "8",
			Info:        "10",
			Error:       "9",

			Active:    "14",
			Idle:      "13",
			Resumable: "10",
			Archived:  "240",

			Processing: "14",
			Waiting:    "11",
			Done:       "10",

			PreviewBorder: "237",
			PreviewLabel:  "12",
			PreviewDim:    "240",
			PreviewValue:  "252",

			StatusBg:     "#1b1b2f",
			StatusFg:     "#8888aa",
			StatusAccent: "#bb86fc",
			StatusDim:    "#555577",
		},
	}
}

// EffectiveColors returns the colors to use based on the theme setting.
// "default" returns built-in colors; "custom" returns the user's colors.
func (c Config) EffectiveColors() Colors {
	if c.Theme != "custom" {
		return Default().Colors
	}
	return c.Colors
}

// Field describes one editable item in the config screen.
type Field struct {
	Section string // "Shortcuts", "Theme", "Worktrees (beta)"
	Label   string // human-readable label
	Key     string // unique identifier
	Desc    string // short description shown with ? key
	Get     func(Config) string
	Set     func(*Config, string)
	Options []string // if non-nil, cycle through these on Enter (dropdown-style)
	Action  bool     // if true, Enter triggers confirmation instead of edit
}

// EditableFields returns the list of all configurable fields.
func EditableFields() []Field {
	return []Field{
		// General
		{Section: "General", Label: "Refresh interval", Key: "refresh_seconds",
			Desc: "Dashboard refresh rate in seconds (1-10)",
			Get:  func(c Config) string { return fmt.Sprintf("%d", c.RefreshSeconds) },
			Set: func(c *Config, v string) {
				n, err := strconv.Atoi(v)
				if err == nil && n >= 1 && n <= 10 {
					c.RefreshSeconds = n
				}
			}},
		{Section: "General", Label: "Scroll speed", Key: "scroll_speed",
			Desc: "Lines per mouse scroll event in session windows (1-10)",
			Get:  func(c Config) string { return fmt.Sprintf("%d", c.ScrollSpeed) },
			Set: func(c *Config, v string) {
				n, err := strconv.Atoi(v)
				if err == nil && n >= 1 && n <= 10 {
					c.ScrollSpeed = n
				}
			}},
		{Section: "General", Label: "Work directory", Key: "work_dir",
			Desc: "Default directory for new sessions (empty = current directory)",
			Get:  func(c Config) string { return c.WorkDir },
			Set:  func(c *Config, v string) { c.WorkDir = v }},
		{Section: "General", Label: "Keep alive", Key: "keep_alive",
			Desc:    "on: sessions keep running when you quit c9s, off: quit kills all sessions",
			Options: []string{"off", "on"},
			Get:     func(c Config) string { return c.KeepAlive },
			Set: func(c *Config, v string) {
				if v == "off" || v == "on" {
					c.KeepAlive = v
				}
			}},
		{Section: "General", Label: "Status bar usage", Key: "status_usage",
			Desc:    "What to show in tmux status bar. Comma-separated: tokens, cost, percent, or all",
			Options: []string{"off", "percent", "cost", "cost,percent", "tokens", "tokens,cost", "all"},
			Get:     func(c Config) string { return c.StatusUsage },
			Set: func(c *Config, v string) {
				c.StatusUsage = v
			}},
		// Cost estimation
		{Section: "Cost estimation", Label: "$/M input", Key: "cost_input",
			Desc:    "$/M for input + cache write tokens. Sonnet: 3, Opus: 15, Haiku: 0.25",
			Get:     func(c Config) string { return strconv.FormatFloat(c.CostInput, 'f', -1, 64) },
			Set: func(c *Config, v string) {
				if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
					c.CostInput = n
				}
			}},
		{Section: "Cost estimation", Label: "$/M output", Key: "cost_output",
			Desc:    "$/M for output tokens. Sonnet: 15, Opus: 75, Haiku: 1.25",
			Get:     func(c Config) string { return strconv.FormatFloat(c.CostOutput, 'f', -1, 64) },
			Set: func(c *Config, v string) {
				if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
					c.CostOutput = n
				}
			}},
		{Section: "Cost estimation", Label: "$/M cache", Key: "cost_cache",
			Desc:    "$/M for cache read tokens. Sonnet: 0.30, Opus: 1.50, Haiku: 0.025",
			Get:     func(c Config) string { return strconv.FormatFloat(c.CostCache, 'f', -1, 64) },
			Set: func(c *Config, v string) {
				if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
					c.CostCache = n
				}
			}},
		// Usage history
		{Section: "Usage history", Label: "Recording", Key: "usage_history",
			Desc:    "Record API usage to ~/.c9s/usage-history.jsonl every 5 minutes",
			Options: []string{"on", "off"},
			Get:     func(c Config) string { return c.UsageHistory },
			Set: func(c *Config, v string) {
				if v == "on" || v == "off" {
					c.UsageHistory = v
				}
			}},
		{Section: "Usage history", Label: "Reset history", Key: "reset_history",
			Desc:   "Delete all recorded usage data",
			Action: true,
			Get:    func(c Config) string { return "" },
			Set:    func(c *Config, v string) {}},
		// Worktrees (beta)
		{Section: "Worktrees (beta)", Label: "Mode", Key: "worktrees",
			Desc:    "off: disabled, auto: show if worktrees exist, always: always show",
			Options: []string{"off", "auto", "always"},
			Get:     func(c Config) string { return c.Worktrees },
			Set: func(c *Config, v string) {
				if v == "off" || v == "auto" || v == "always" {
					c.Worktrees = v
				}
			}},
		{Section: "Worktrees (beta)", Label: "Expand", Key: "worktree_expand",
			Desc:    "all: toggle all worktrees at once, selected: expand per session",
			Options: []string{"all", "selected"},
			Get:     func(c Config) string { return c.WorktreeExpand },
			Set: func(c *Config, v string) {
				if v == "all" || v == "selected" {
					c.WorktreeExpand = v
				}
			}},
		// Shortcuts
		{Section: "Shortcuts", Label: "Dashboard", Key: "dashboard",
			Desc: "Return to dashboard from session window (tmux key syntax)",
			Get:  func(c Config) string { return c.Keys.Dashboard },
			Set:  func(c *Config, v string) { c.Keys.Dashboard = v }},
		{Section: "Shortcuts", Label: "Next session", Key: "next_session",
			Desc: "Switch to next session window (tmux key syntax)",
			Get:  func(c Config) string { return c.Keys.NextSession },
			Set:  func(c *Config, v string) { c.Keys.NextSession = v }},
		{Section: "Shortcuts", Label: "Prev session", Key: "prev_session",
			Desc: "Switch to previous session window (tmux key syntax)",
			Get:  func(c Config) string { return c.Keys.PrevSession },
			Set:  func(c *Config, v string) { c.Keys.PrevSession = v }},
		// Theme toggle
		{Section: "Theme", Label: "Color scheme", Key: "theme",
			Desc:    "default: built-in colors, custom: edit colors below",
			Options: []string{"default", "custom"},
			Get:     func(c Config) string { return c.Theme },
			Set:     func(c *Config, v string) { c.Theme = v }},
		// Colors (only visible when theme == "custom")
		{Section: "Theme", Label: "Title", Key: "title",
			Get: func(c Config) string { return c.Colors.Title },
			Set: func(c *Config, v string) { c.Colors.Title = v }},
		{Section: "Theme", Label: "Header", Key: "header",
			Get: func(c Config) string { return c.Colors.Header },
			Set: func(c *Config, v string) { c.Colors.Header = v }},
		{Section: "Theme", Label: "Selected bg", Key: "selected",
			Get: func(c Config) string { return c.Colors.Selected },
			Set: func(c *Config, v string) { c.Colors.Selected = v }},
		{Section: "Theme", Label: "Dim text", Key: "dim",
			Get: func(c Config) string { return c.Colors.Dim },
			Set: func(c *Config, v string) { c.Colors.Dim = v }},
		{Section: "Theme", Label: "Group header", Key: "group_header",
			Get: func(c Config) string { return c.Colors.GroupHeader },
			Set: func(c *Config, v string) { c.Colors.GroupHeader = v }},
		{Section: "Theme", Label: "Help key", Key: "help_key",
			Get: func(c Config) string { return c.Colors.HelpKey },
			Set: func(c *Config, v string) { c.Colors.HelpKey = v }},
		{Section: "Theme", Label: "Help text", Key: "help",
			Get: func(c Config) string { return c.Colors.Help },
			Set: func(c *Config, v string) { c.Colors.Help = v }},
		{Section: "Theme", Label: "Info", Key: "info",
			Get: func(c Config) string { return c.Colors.Info },
			Set: func(c *Config, v string) { c.Colors.Info = v }},
		{Section: "Theme", Label: "Error", Key: "error_color",
			Get: func(c Config) string { return c.Colors.Error },
			Set: func(c *Config, v string) { c.Colors.Error = v }},
		{Section: "Theme", Label: "Active", Key: "active",
			Get: func(c Config) string { return c.Colors.Active },
			Set: func(c *Config, v string) { c.Colors.Active = v }},
		{Section: "Theme", Label: "Idle", Key: "idle",
			Get: func(c Config) string { return c.Colors.Idle },
			Set: func(c *Config, v string) { c.Colors.Idle = v }},
		{Section: "Theme", Label: "Resumable", Key: "resumable",
			Get: func(c Config) string { return c.Colors.Resumable },
			Set: func(c *Config, v string) { c.Colors.Resumable = v }},
		{Section: "Theme", Label: "Archived", Key: "archived",
			Get: func(c Config) string { return c.Colors.Archived },
			Set: func(c *Config, v string) { c.Colors.Archived = v }},
		{Section: "Theme", Label: "Processing", Key: "processing",
			Get: func(c Config) string { return c.Colors.Processing },
			Set: func(c *Config, v string) { c.Colors.Processing = v }},
		{Section: "Theme", Label: "Waiting", Key: "waiting_color",
			Get: func(c Config) string { return c.Colors.Waiting },
			Set: func(c *Config, v string) { c.Colors.Waiting = v }},
		{Section: "Theme", Label: "Done", Key: "done",
			Get: func(c Config) string { return c.Colors.Done },
			Set: func(c *Config, v string) { c.Colors.Done = v }},
		{Section: "Theme", Label: "Preview border", Key: "preview_border",
			Get: func(c Config) string { return c.Colors.PreviewBorder },
			Set: func(c *Config, v string) { c.Colors.PreviewBorder = v }},
		{Section: "Theme", Label: "Preview label", Key: "preview_label",
			Get: func(c Config) string { return c.Colors.PreviewLabel },
			Set: func(c *Config, v string) { c.Colors.PreviewLabel = v }},
		{Section: "Theme", Label: "Preview dim", Key: "preview_dim",
			Get: func(c Config) string { return c.Colors.PreviewDim },
			Set: func(c *Config, v string) { c.Colors.PreviewDim = v }},
		{Section: "Theme", Label: "Preview value", Key: "preview_value",
			Get: func(c Config) string { return c.Colors.PreviewValue },
			Set: func(c *Config, v string) { c.Colors.PreviewValue = v }},
		{Section: "Theme", Label: "Status bar bg", Key: "status_bg",
			Get: func(c Config) string { return c.Colors.StatusBg },
			Set: func(c *Config, v string) { c.Colors.StatusBg = v }},
		{Section: "Theme", Label: "Status bar fg", Key: "status_fg",
			Get: func(c Config) string { return c.Colors.StatusFg },
			Set: func(c *Config, v string) { c.Colors.StatusFg = v }},
		{Section: "Theme", Label: "Status accent", Key: "status_accent",
			Get: func(c Config) string { return c.Colors.StatusAccent },
			Set: func(c *Config, v string) { c.Colors.StatusAccent = v }},
		{Section: "Theme", Label: "Status dim", Key: "status_dim",
			Get: func(c Config) string { return c.Colors.StatusDim },
			Set: func(c *Config, v string) { c.Colors.StatusDim = v }},
	}
}

// PathOverride allows tests to redirect config reads/writes.
var PathOverride string

func configPath() string {
	if PathOverride != "" {
		return PathOverride
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".c9s", "config.json")
}

// Path returns the config file path.
func Path() string {
	return configPath()
}

// EnsureExists creates the config file with defaults if it doesn't exist.
func EnsureExists() error {
	path := configPath()
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return Save(Default())
}

// Load reads the config from ~/.c9s/config.json.
// Missing fields keep their default values.
func Load() Config {
	cfg := Default()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

// Save writes the config to ~/.c9s/config.json.
func Save(cfg Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// cachedConfig provides mtime-based config reload.
var cachedConfig struct {
	mu    sync.Mutex
	cfg   Config
	mtime time.Time
	valid bool
}

// LoadIfChanged returns the current config, only re-reading from disk
// when the file mtime has changed. Returns (config, changed).
func LoadIfChanged() (Config, bool) {
	cachedConfig.mu.Lock()
	defer cachedConfig.mu.Unlock()

	path := configPath()
	info, err := os.Stat(path)
	if err != nil {
		if !cachedConfig.valid {
			cachedConfig.cfg = Default()
			cachedConfig.valid = true
			return cachedConfig.cfg, true
		}
		return cachedConfig.cfg, false
	}

	if cachedConfig.valid && info.ModTime().Equal(cachedConfig.mtime) {
		return cachedConfig.cfg, false
	}

	cachedConfig.cfg = Load()
	cachedConfig.mtime = info.ModTime()
	cachedConfig.valid = true
	return cachedConfig.cfg, true
}
