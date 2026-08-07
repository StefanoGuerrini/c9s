package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/stefanoguerrini/c9s/internal/claude"
	"github.com/stefanoguerrini/c9s/internal/config"
	"github.com/stefanoguerrini/c9s/internal/git"
	"github.com/stefanoguerrini/c9s/internal/installer"
	"github.com/stefanoguerrini/c9s/internal/sessionstate"
	"github.com/stefanoguerrini/c9s/internal/tmux"
)

// version is set at build time via ldflags.
var version = "dev"

// cfg is the global config loaded at startup.
var cfg config.Config

// debugLog writes to /tmp/c9s-debug.log when --debug is enabled.
var debugLog = func(string, ...any) {}

// staleStateAge is how long a session state file can go without a hook
// update before startup treats it as an orphan from an ungracefully-killed
// session and purges it (see sessionstate.PurgeStale). A live session's
// hooks (Stop, PreToolUse, ...) refresh UpdatedAt far more often than this,
// even when the user is idle mid-conversation.
const staleStateAge = 24 * time.Hour

func initDebugLog(enabled bool) {
	if !enabled {
		return
	}
	logDir := filepath.Join(os.Getenv("HOME"), ".c9s")
	os.MkdirAll(logDir, 0700)
	logPath := filepath.Join(logDir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	debugLog = log
	claude.DebugLog = log
}

var (
	titleStyle    lipgloss.Style
	headerStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	dimStyle      lipgloss.Style
	groupStyle    lipgloss.Style
	helpKeyStyle  lipgloss.Style
	helpStyle     lipgloss.Style
	stActive      lipgloss.Style
	stIdle        lipgloss.Style
	stResumable   lipgloss.Style
	stArchived    lipgloss.Style
	stBackground  lipgloss.Style
	infoStyle     lipgloss.Style
	errStyle      lipgloss.Style
	stWaiting     lipgloss.Style
	stProcessing  lipgloss.Style
	stDone        lipgloss.Style
	stUnknown     lipgloss.Style
	previewBorder lipgloss.Style
	previewLabel  lipgloss.Style
	previewDim    lipgloss.Style
	previewVal    lipgloss.Style
)

func applyColors(c config.Colors) {
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Title))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Header))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color(c.Selected))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Dim))
	groupStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.GroupHeader)).Bold(true)
	helpKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.HelpKey))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Help))
	stActive = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Active)).Bold(true)
	stIdle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Idle))
	stResumable = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Resumable))
	stArchived = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Archived))
	stBackground = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Bold(true)
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Info))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Error))
	stWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Waiting)).Bold(true)
	stProcessing = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Processing)).Bold(true)
	stDone = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Done))
	stUnknown = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Dim))
	previewBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.PreviewBorder))
	previewLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.PreviewLabel))
	previewDim = lipgloss.NewStyle().Foreground(lipgloss.Color(c.PreviewDim))
	previewVal = lipgloss.NewStyle().Foreground(lipgloss.Color(c.PreviewValue))
}

// groupMode controls how sessions are grouped.
type groupMode int

const (
	groupNone    groupMode = iota
	groupProject
	groupStatus
)


type tickMsg time.Time

// statusMsg is a temporary message shown in the footer.
type statusMsg struct {
	text    string
	isError bool
}

type clearStatusMsg struct{}

// displayItem is one row of the dashboard table. Rows come in four flavors,
// selected by exactly one of the bool flags:
//
//   - isHeader        -- group header (── auto-parser (2 sessions) ──)
//   - isWorktreeRow   -- a worktree row inside a project group
//   - isEmptyWorktree -- a worktree with no sessions (dim placeholder)
//   - (none set)      -- a session row
//
// indent carries the visual nesting level so the renderer can prepend the
// right amount of whitespace: 0 for headers, 1 for worktree rows and for
// sessions directly under a project header, 2 for sessions nested under a
// worktree. parentProject and parentWorktree link session/worktree rows back
// to the project/worktree they belong to, so key handlers can act on the
// enclosing scope without re-scanning the table.
type displayItem struct {
	isHeader        bool
	header          string
	session         claude.SessionInfo
	isWorktreeRow   bool
	isEmptyWorktree bool
	worktree        git.Worktree
	indent          int // 0, 1, or 2
	parentProject   string
	parentWorktree  string // absolute path of the enclosing worktree, if any
}

// managedWindow tracks a tmux window we opened for a session.
type managedWindow struct {
	windowID   string
	sessionID  string
	project    string
	paneStatus tmux.PaneStatus
}

type model struct {
	sessions []claude.SessionInfo
	cursor   int
	scroll   int
	width    int
	height   int
	err      error

	// Search
	searching   bool
	searchInput textinput.Model
	filter      string

	// Toggles
	groupBy      groupMode // none / project / status
	showTokens   bool      // show token column
	showPreview  bool      // show session preview panel

	// tmux
	insideTmux         bool // running inside tmux as dashboard
	managedWindows     map[string]managedWindow // sessionID → window
	replacedSessions   map[string]bool          // sessions replaced by fork/clear, hidden from dashboard
	lastRecordedFetch  time.Time                // dedup usage history writes

	// Demo mode
	demoSessionScreen bool   // showing fake session screen
	demoSessionName   string // session name being "opened"

	// Usage history screen
	usageScreen   bool
	usageViewMode int // 0=daily, 1=weekly, 2=monthly
	usageDayRange int // 7, 14, or 30 (daily mode)

	// Rename
	renaming      bool
	renameInput   textinput.Model
	renameSession *claude.SessionInfo // session being renamed

	// Project picker
	pickingProject       bool
	projectDirs          []string             // full paths, [0] = work_dir root, [1:] = subdirs sorted by last used
	projectLastUsed      map[string]time.Time // dir → most recent session Modified
	projectCursor        int
	projectFilter        string // typed search filter
	projectWithEffort    bool   // true when triggered via N (chain into effort picker)
	projectEffortStep    bool   // true when showing effort options inside the picker
	projectModelStep     bool   // true when showing model options inside the picker
	projectEffort        string // selected effort (stored between steps)

	// Effort picker
	pickingEffort bool
	effortWorkDir string // project dir for the new session

	// Worktree display. Hierarchical view is only active when groupBy=project;
	// there worktrees are always rendered under the project header, and this
	// map tracks which individual worktrees have their session sub-list
	// collapsed. Default is expanded (map miss == show sessions).
	collapsedWorktrees map[string]bool               // worktree path → collapsed
	worktreeCache      map[string]worktreeCacheEntry // project dir → cached worktrees + mtime fingerprint

	// Worktree create prompt (a key) -- reuses renameInput.
	addingWorktree    bool
	addWorktreeRepo   string // repo dir for the worktree being created

	// Worktree delete confirmation (d key).
	deletingWorktree     bool
	deleteWorktree       git.Worktree // worktree being deleted
	deleteWorktreeRepo   string       // repo dir that owns it
	deleteWorktreeForce  bool         // second-stage prompt: dirty worktree needs --force

	// Config screen
	configScreen  bool
	configDraft   config.Config
	configFields  []config.Field
	configCursor  int
	configScroll  int
	configEditing bool
	configEditIdx int
	configInput   textinput.Model
	configShowDesc    bool   // show field descriptions
	configConfirming  bool   // showing confirmation prompt
	configConfirmKey  string // key of field being confirmed

	// Demo mode (--demo flag, fake data for screenshots)
	demoMode bool

	// Status bar message
	statusText    string
	statusIsError bool

	// True when c9s hooks are present in ~/.claude/settings.json. Refreshed
	// on each tick; drives the dashboard install banner.
	hooksInstalled bool
}

func initialModel(sessions []claude.SessionInfo, err error, insideTmux bool) model {
	si := textinput.New()
	si.Prompt = "/ "
	si.Placeholder = "search..."

	ri := textinput.New()
	ri.Prompt = "rename: "
	ri.CharLimit = 80

	ci := textinput.New()
	ci.Prompt = "  "
	ci.CharLimit = 40



	return model{
		sessions:          sessions,
		err:               err,
		searchInput:       si,
		renameInput:       ri,
		configInput:       ci,
		insideTmux:        insideTmux,
		managedWindows:    make(map[string]managedWindow),
		replacedSessions:  loadReplacedSessions(),
		usageDayRange:     14,
		collapsedWorktrees: make(map[string]bool),
		worktreeCache:      make(map[string]worktreeCacheEntry),
		showTokens:        cfg.Dashboard.ShowTokens,
		showPreview:       cfg.Dashboard.ShowPreview,
		groupBy:           groupMode(cfg.Dashboard.GroupBy),
		hooksInstalled:    installer.IsInstalled(),
	}
}

func (m model) Init() tea.Cmd {
	cmd := tea.Tick(refreshInterval(), func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
	if m.insideTmux {
		tmux.SetupNavigationKeys(navKeys())
	}
	return cmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tickMsg:
		if !m.demoMode {
			// Capture selected session before any changes so we can restore
			// cursor position after all updates (session reorder + hidden sessions).
			var selectedID string
			if s := m.selectedSession(m.items()); s != nil {
				selectedID = s.SessionID
			}

			sessions, err := claude.ListAllSessions()
			if err == nil && sessionsChanged(m.sessions, sessions) {
				m.sessions = sessions
			}
			// Reconcile managed windows FIRST so a forked session's window is
			// re-keyed to the new session ID before superseded detection runs.
			// If superseded detection ran first it would kill the active window
			// before reconcile could re-key it to the new session ID.
			if m.insideTmux && len(m.managedWindows) > 0 {
				// Pane-anchor pass first: hooks record $TMUX_PANE, so if
				// Claude switched session ids inside a pane the state file
				// is the authoritative signal. This corrects the tag before
				// mtime-based heuristics run.
				events := m.retagFromPaneAnchors()
				prevReplaced := len(m.replacedSessions)
				// Consolidate /clear-style transitions (pane rebound to a
				// new id) into a single dashboard row so the user keeps
				// working with the same visible session across the switch.
				m.applyClearOrForkEffects(events, m.sessions)
				m.reconcileWindows(m.sessions)
				if len(m.replacedSessions) > prevReplaced {
					m.saveDashboardState()
				}
			}
			// Hide sessions that have been superseded by a fork/compaction.
			// Also close their tmux windows so Ctrl+n/p won't navigate to stale sessions.
			// By this point, reconcileWindows has already re-keyed any active windows,
			// so only truly orphaned windows get closed here.
			for id := range claude.GetSupersededSessions() {
				if !m.replacedSessions[id] {
					m.replacedSessions[id] = true
					if mw, ok := m.managedWindows[id]; ok {
						debugLog("tick → killing superseded window %s session=%q", mw.windowID, id)
						tmux.KillWindow(mw.windowID)
						delete(m.managedWindows, id)
					} else {
						debugLog("tick → hiding superseded session %q (no window)", id)
					}
				}
			}
			// Restore cursor to the selected session after all list changes.
			if selectedID != "" {
				for i, item := range m.items() {
					if !item.isHeader && !item.isWorktreeRow && item.session.SessionID == selectedID {
						m.cursor = i
						break
					}
				}
				m.adjustScroll()
			}
			// Reload config if changed on disk (e.g. after editing via 'c').
			if newCfg, changed := config.LoadIfChanged(); changed {
				cfg = newCfg
				applyColors(cfg.EffectiveColors())
			}
			// Keep backups up to date with source files.
			claude.RefreshBackups()
			// Refresh worktree cache (cheap git calls, only when feature
			// enabled). lookupWorktrees() now invalidates per-entry based on
			// .git/ mtime, so re-read for projects whose fingerprint shifted.
			if cfg.Worktrees == "on" && git.Available() {
				for dir := range m.worktreeCache {
					m.lookupWorktrees(dir)
				}
			}
		}
		// Update pane statuses for managed windows. Status is sourced from
		// per-session state files written by Claude Code hooks (see
		// internal/sessionstate, internal/installer). Sessions without a
		// state file render as PaneUnknown.
		m.hooksInstalled = installer.IsInstalled()
		for key, mw := range m.managedWindows {
			if !tmux.WindowExists(mw.windowID) {
				debugLog("tick → window %s gone, removing session=%q", mw.windowID, key)
				delete(m.managedWindows, key)
				// The window is gone -- release its pane claim now rather
				// than waiting on SessionEnd (which never fires if the
				// window was killed rather than exited) or the startup
				// purge, so a reused pane ID can't collide with this stale
				// entry in the meantime.
				sessionstate.Remove(mw.sessionID)
				continue
			}

			// Refresh tmux per-window usage badge. Prefer hook-pushed state
			// (input/output tokens, model) when available -- that avoids
			// reopening the session JSONL every tick. Fall back to the
			// JSONL-derived SessionInfo for sessions started before hooks
			// were installed.
			info, infoErr := sessionstate.Read(mw.sessionID)
			for _, s := range m.sessions {
				if s.SessionID == mw.sessionID {
					var usage string
					if infoErr == nil {
						usage = formatUsageFromState(s, info)
					} else {
						usage = formatUsage(s)
					}
					if usage != "" {
						tmux.SetWindowEnv(mw.windowID, "c9s-usage", usage)
					}
					break
				}
			}

			mw.paneStatus = paneStatusFromInfo(info, infoErr)
			m.managedWindows[key] = mw
		}
		// Update dashboard status bar with global usage.
		if m.insideTmux {
			if usage := formatDashboardUsage(m.sessions); usage != "" {
				tmux.SetWindowEnv(tmux.SessionName+":"+tmux.DashboardWindow, "c9s-usage", usage)
			}
		}
		// Record usage history when we get fresh API data (dedup by fetch time).
		if cfg.UsageHistory != "off" {
			if result, err := claude.FetchUsage(); err == nil && !result.Stale && result.Fetched.After(m.lastRecordedFetch) {
				var totalTokens int
				for _, s := range m.sessions {
					totalTokens += s.TotalTokens()
				}
				claude.RecordUsage(result.Usage, totalTokens, claude.GetModelBreakdown(m.sessions))
				m.lastRecordedFetch = result.Fetched
			}
		}

		return m, tea.Tick(refreshInterval(), func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	case statusMsg:
		m.statusText = msg.text
		m.statusIsError = msg.isError
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
			return clearStatusMsg{}
		})
	case clearStatusMsg:
		m.statusText = ""
		return m, nil
	case tea.KeyPressMsg:
		debugLog("key=%q", msg.String())
		if m.demoSessionScreen {
			// Any key returns to dashboard.
			m.demoSessionScreen = false
			return m, nil
		}
		if m.usageScreen {
			return m.updateUsage(msg)
		}
		if m.configScreen {
			return m.updateConfig(msg)
		}
		if m.pickingProject {
			return m.updateProjectPicker(msg)
		}
		if m.pickingEffort {
			return m.updateEffortPicker(msg)
		}
		if m.renaming {
			return m.updateRename(msg)
		}
		if m.addingWorktree {
			return m.updateAddWorktree(msg)
		}
		if m.deletingWorktree {
			return m.updateDeleteWorktree(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.filter = ""
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.cursor = 0
		m.scroll = 0
		return m, nil
	case "enter":
		m.searching = false
		m.filter = m.searchInput.Value()
		m.searchInput.Blur()
		m.cursor = 0
		m.scroll = 0
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.filter = m.searchInput.Value()
	m.cursor = 0
	m.scroll = 0
	return m, cmd
}

func (m model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.items()
	switch msg.String() {
	case "q", "ctrl+c":
		if m.insideTmux {
			if cfg.KeepAlive == "on" {
				// Just detach -- dashboard and all sessions keep running.
				// Re-running c9s will re-attach to the existing session.
				tmux.Detach()
				return m, nil // don't quit, keep dashboard alive
			}
			tmux.CleanupNavigationKeys(navKeys())
			// Every managed session is about to die without a chance to run
			// its SessionEnd hook -- release their pane claims now. Without
			// this, a same-day restart reuses tmux's low pane IDs almost
			// immediately and collides with these still-fresh (well under
			// the startup purge's age cutoff) leftover claims.
			for _, mw := range m.managedWindows {
				sessionstate.Remove(mw.sessionID)
			}
			// Kill the entire tmux session so we don't fall through
			// to a claude window after the dashboard exits.
			tmux.KillSession()
		}
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.skipHeaders(items, -1)
		}
	case "down", "j":
		if m.cursor < len(items)-1 {
			m.cursor++
			m.skipHeaders(items, 1)
		}
	case "pgup", "ctrl+u":
		m.cursor -= m.tableHeight()
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.skipHeaders(items, 1)
	case "pgdown", "ctrl+d":
		m.cursor += m.tableHeight()
		if m.cursor >= len(items) {
			m.cursor = len(items) - 1
		}
		m.skipHeaders(items, -1)
	case "home", "g":
		m.cursor = 0
		m.skipHeaders(items, 1)
	case "end", "G":
		m.cursor = len(items) - 1
		m.skipHeaders(items, -1)
	case "/":
		m.searching = true
		m.searchInput.SetValue(m.filter)
		return m, m.searchInput.Focus()
	case "tab":
		m.groupBy = (m.groupBy + 1) % 3
		m.cursor = 0
		m.scroll = 0
		newItems := m.items()
		m.skipHeaders(newItems, 1)
		m.saveDashboardState()
	case "t":
		m.showTokens = !m.showTokens
		m.saveDashboardState()
	case "p":
		m.showPreview = !m.showPreview
		m.saveDashboardState()
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.searchInput.SetValue("")
			m.cursor = 0
			m.scroll = 0
		}
	case "w":
		return m.toggleWorktree(items)
	case "a":
		if !m.worktreesActive() {
			return m, statusCmd("worktrees show up when grouped by project -- press Tab", false)
		}
		return m.startAddWorktree(items)
	case "d":
		if !m.worktreesActive() {
			return m, statusCmd("worktrees show up when grouped by project -- press Tab", false)
		}
		return m.startDeleteWorktree(items)
	case "enter":
		return m.openSession(items)
	case "n":
		return m.startProjectPicker(items, false)
	case "N":
		return m.startProjectPicker(items, true)
	case "x":
		return m.closeWindow(items)
	case "R":
		return m.startRename(items)
	case "b":
		return m.backupSession(items)
	case "c":
		return m.enterConfigScreen()
	case "u":
		return m.enterUsageScreen()
	}
	m.adjustScroll()
	return m, nil
}

// openSession opens or switches to the selected session.
func (m model) openSession(items []displayItem) (tea.Model, tea.Cmd) {
	debugLog("openSession cursor=%d", m.cursor)
	// Demo mode: show a fake session screen instead of opening a real tmux window.
	if m.demoMode {
		s := m.selectedSession(items)
		if s == nil {
			return m, nil
		}
		m.demoSessionScreen = true
		m.demoSessionName = s.DisplayName()
		return m, nil
	}
	if !m.insideTmux {
		return m, statusCmd("tmux required -- run c9s outside tmux to auto-bootstrap", true)
	}

	// If a worktree row (populated or empty placeholder) is selected, start a
	// new session in that worktree dir.
	if m.cursor >= 0 && m.cursor < len(items) {
		it := items[m.cursor]
		if it.isWorktreeRow || it.isEmptyWorktree {
			m.effortWorkDir = it.worktree.Path
			return m.newSession(nil, "", "")
		}
	}

	s := m.selectedSession(items)
	if s == nil {
		return m, nil
	}

	debugLog("openSession session=%q status=%s project=%q", s.SessionID, s.Status, s.ProjectPath)

	// If we already have a window for this session, try to switch to it.
	if mw, ok := m.managedWindows[s.SessionID]; ok {
		if err := tmux.SelectWindow(mw.windowID); err == nil {
			debugLog("openSession → switched to existing window %s", mw.windowID)
			return m, nil
		}
		// Window was closed externally -- clean up and fall through to re-open.
		debugLog("openSession → window %s gone, cleaning up", mw.windowID)
		delete(m.managedWindows, s.SessionID)
		sessionstate.Remove(s.SessionID)
	}

	// Check for superseded/forked sessions to prevent duplicate windows.
	superseded := claude.GetSupersededSessions()

	// Case 1: This session was superseded -- switch to the successor's window.
	if superseded[s.SessionID] {
		debugLog("openSession → session is superseded, looking for successor window")
		for _, mw := range m.managedWindows {
			if mw.project == s.ProjectPath {
				if err := tmux.SelectWindow(mw.windowID); err == nil {
					debugLog("openSession → redirected to successor window %s", mw.windowID)
					return m, nil
				}
			}
		}
	}

	// Case 2: This is the successor session, but reconcileWindows hasn't
	// re-keyed yet. If a managed window still tracks a superseded session
	// in the same project, switch to it and re-key now.
	for oldID, mw := range m.managedWindows {
		if mw.project == s.ProjectPath && superseded[oldID] {
			if err := tmux.SelectWindow(mw.windowID); err == nil {
				debugLog("openSession → re-keyed window %s from %q to %q", mw.windowID, oldID, s.SessionID)
				// Re-key the managed window to the new session ID.
				delete(m.managedWindows, oldID)
				m.managedWindows[s.SessionID] = managedWindow{
					windowID:  mw.windowID,
					sessionID: s.SessionID,
					project:   mw.project,
				}
				tmux.SetWindowEnv(mw.windowID, "session-id", s.SessionID)
				return m, nil
			}
		}
	}

	// Build the claude command.
	var claudeCmd string
	workDir := s.ProjectPath

	// Validate session ID before using in shell commands.
	if !claude.IsValidSessionID(s.SessionID) {
		return m, statusCmd("invalid session ID", true)
	}

	switch s.Status {
	case claude.StatusBackground:
		// A session claimed by a Claude Code background agent can't be
		// resumed with a plain `claude --resume` -- the CLI refuses since
		// the agent process already owns it, and the window would flash and
		// close before the user could read the error. `claude agents` is
		// the only way to attach; open its interactive picker directly
		// instead of dead-ending on a status message. We don't know which
		// session id the user will land on, so track the window with a
		// tmpKey (same as the `n` new-session flow) -- the pane-anchor pass
		// adopts it automatically once hooks fire for the real id.
		return m.openAgentsPicker(s.ProjectPath)
	case claude.StatusActive, claude.StatusIdle:
		// Resume into the running session (claude handles concurrent access).
		claudeCmd = fmt.Sprintf("claude --resume %s", s.SessionID)
	case claude.StatusResumable:
		claudeCmd = fmt.Sprintf("claude --resume %s", s.SessionID)
	case claude.StatusArchived:
		// Check if we have a backup that can be restored.
		if claude.HasBackup(s.SessionID) {
			restored, err := claude.RestoreSession(s.SessionID)
			if err != nil {
				return m, statusCmd(fmt.Sprintf("restore failed: %v", err), true)
			}
			if restored {
				claudeCmd = fmt.Sprintf("claude --resume %s", s.SessionID)
				break
			}
		}
		// No backup -- start a new session in the same project directory.
		claudeCmd = "claude"
	}

	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	name := truncWindowName(s.DisplayName())
	debugLog("openSession → creating window name=%q cmd=%q dir=%q", name, claudeCmd, workDir)
	windowID, err := tmux.NewWindow(name, claudeCmd, workDir)
	if err != nil {
		return m, statusCmd(fmt.Sprintf("failed to open window: %v", err), true)
	}
	debugLog("openSession → created window %s", windowID)

	// Tag the window with the session ID so we can recover it after restart.
	tmux.SetWindowEnv(windowID, "session-id", s.SessionID)
	m.managedWindows[s.SessionID] = managedWindow{
		windowID:  windowID,
		sessionID: s.SessionID,
		project:   s.ProjectPath,
	}
	seedSessionState(s.SessionID, workDir)

	return m, nil
}

// openAgentsPicker opens `claude agents` (the only CLI-level way to attach to
// a background agent) in a new tmux window. There's no flag to preselect a
// specific agent, so this opens the picker unscoped -- background agents are
// rare enough in practice that finding the right one in the list is trivial.
// The window is tracked with a tmpKey since we don't know which session id
// the user will land on; the pane-anchor pass (applyPaneAnchorRetag) adopts
// it once hooks fire for the real id, same as the `n` new-session flow.
func (m model) openAgentsPicker(workDir string) (tea.Model, tea.Cmd) {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	windowID, err := tmux.NewWindow("claude agents", "claude agents", workDir)
	if err != nil {
		return m, statusCmd(fmt.Sprintf("failed to open agents picker: %v", err), true)
	}
	tmpKey := fmt.Sprintf("new-%d", time.Now().UnixNano())
	m.managedWindows[tmpKey] = managedWindow{windowID: windowID, project: workDir}
	return m, nil
}

// seedSessionState writes a minimal state file for a session c9s just opened,
// but only when one doesn't already exist. Hooks (SessionStart / UserPromptSubmit
// / Stop, etc.) overwrite this the moment they fire; the seed is just a
// placeholder so the dashboard has *something* to read for the window while we
// wait for the first hook to land. Without it, sessions resumed before any
// hook event ever fires (or whose state file was deleted) sit at "unknown"
// forever. Errors are swallowed on purpose -- this is best-effort book-keeping,
// not a correctness path.
func seedSessionState(sessionID, workDir string) {
	if sessionID == "" {
		return
	}
	if _, err := sessionstate.Read(sessionID); err == nil {
		return // file exists -- hooks own it now
	}
	_ = sessionstate.Patch(sessionID, func(i *sessionstate.Info) {
		if i.State == "" {
			i.State = sessionstate.StateDone
		}
		if i.Cwd == "" {
			i.Cwd = workDir
		}
		i.LastEvent = "c9s-seed"
	})
}

// newSession creates a brand new claude session in the selected project.
func (m model) newSession(items []displayItem, effort, modelID string) (tea.Model, tea.Cmd) {
	debugLog("newSession effort=%q model=%q effortWorkDir=%q pickingProject=%v pickingEffort=%v",
		effort, modelID, m.effortWorkDir, m.pickingProject, m.pickingEffort)
	if !m.insideTmux {
		return m, statusCmd("tmux required -- run c9s outside tmux to auto-bootstrap", true)
	}

	// Use the selected session's project dir, configured work_dir, or cwd.
	// Priority: effortWorkDir (worktree) > work_dir config > selected session's project.
	// Prefer the repo's main worktree over a linked worktree, so `n` from a
	// row inside a feature worktree still creates the new session in main.
	// Enter on a worktree row bypasses this via effortWorkDir.
	workDir := m.effortWorkDir
	if workDir == "" && cfg.WorkDir != "" {
		workDir = cfg.WorkDir
	}
	if workDir == "" {
		if s := m.selectedSession(items); s != nil && s.ProjectPath != "" {
			if main := m.mainWorktreeDir(s.ProjectPath); main != "" {
				workDir = main
			} else {
				workDir = s.ProjectPath
			}
		}
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	cmd := "claude"
	if effort != "" {
		cmd = fmt.Sprintf("claude --effort %s", effort)
	}
	if modelID != "" {
		cmd = fmt.Sprintf("ANTHROPIC_MODEL=%s %s", modelID, cmd)
	}

	// Name the window with project + short timestamp to distinguish multiple sessions.
	name := fmt.Sprintf("%s·%s", filepath.Base(workDir), time.Now().Format("15:04"))
	debugLog("newSession → creating window name=%q cmd=%q dir=%q", name, cmd, workDir)
	windowID, err := tmux.NewWindow(name, cmd, workDir)
	if err != nil {
		return m, statusCmd(fmt.Sprintf("failed to create session: %v", err), true)
	}

	// Track with a temporary key (will be discovered on next refresh).
	tmpKey := fmt.Sprintf("new-%d", time.Now().UnixNano())
	m.managedWindows[tmpKey] = managedWindow{
		windowID: windowID,
		project:  workDir,
	}

	return m, nil
}

// startEffortPicker enters effort selection mode before creating a new session.
func (m model) startEffortPicker(items []displayItem) (tea.Model, tea.Cmd) {
	if !m.insideTmux {
		return m, statusCmd("tmux required -- run c9s outside tmux to auto-bootstrap", true)
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		if s := m.selectedSession(items); s != nil && s.ProjectPath != "" {
			if main := m.mainWorktreeDir(s.ProjectPath); main != "" {
				workDir = main
			} else {
				workDir = s.ProjectPath
			}
		}
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	m.pickingEffort = true
	m.effortWorkDir = workDir
	return m, nil
}

// updateEffortPicker handles key input during effort selection.
func (m model) updateEffortPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	efforts := map[string]string{
		"1": "low",
		"2": "medium",
		"3": "high",
		"4": "max",
	}
	key := msg.String()
	if effort, ok := efforts[key]; ok {
		m.pickingEffort = false
		return m.newSession(nil, effort, "")
	}
	if key == "esc" || key == "q" {
		m.pickingEffort = false
		m.effortWorkDir = ""
		return m, nil
	}
	return m, nil
}

// startProjectPicker shows the project directory picker when work_dir is configured.
// If work_dir is empty, falls through to newSession or startEffortPicker directly.
func (m model) startProjectPicker(items []displayItem, withEffort bool) (tea.Model, tea.Cmd) {
	if cfg.WorkDir == "" {
		if withEffort {
			return m.startEffortPicker(items)
		}
		return m.newSession(items, "", "")
	}

	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		if withEffort {
			return m.startEffortPicker(items)
		}
		return m.newSession(items, "", "")
	}

	// Collect subdirectories (skip hidden).
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, filepath.Join(cfg.WorkDir, e.Name()))
		}
	}

	// Build last-used map from sessions.
	lastUsed := make(map[string]time.Time)
	for _, s := range m.sessions {
		if s.ProjectPath != "" {
			if t, ok := lastUsed[s.ProjectPath]; !ok || s.Modified.After(t) {
				lastUsed[s.ProjectPath] = s.Modified
			}
		}
	}

	// Sort: dirs with recent sessions first (newest first), then dirs without sessions (alphabetical).
	sort.SliceStable(dirs, func(i, j int) bool {
		ti, oki := lastUsed[dirs[i]]
		tj, okj := lastUsed[dirs[j]]
		if oki && okj {
			return ti.After(tj)
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return dirs[i] < dirs[j]
	})

	// Prepend root (work_dir itself) as first entry.
	m.projectDirs = append([]string{cfg.WorkDir}, dirs...)
	m.projectLastUsed = lastUsed
	m.projectCursor = 0
	m.pickingProject = true
	m.projectWithEffort = withEffort
	return m, nil
}

// filteredProjectDirs returns projectDirs filtered by the current search string.
func (m model) filteredProjectDirs() []string {
	if m.projectFilter == "" {
		return m.projectDirs
	}
	f := strings.ToLower(m.projectFilter)
	var out []string
	for _, dir := range m.projectDirs {
		name := filepath.Base(dir)
		if dir == cfg.WorkDir {
			name = ". (root)"
		}
		if strings.Contains(strings.ToLower(name), f) {
			out = append(out, dir)
		}
	}
	return out
}

// updateProjectPicker handles key input during project directory selection.
func (m model) updateProjectPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Model step: pick model after effort.
	if m.projectModelStep {
		models := map[string]string{
			"1": "claude-opus-4-6",
			"2": "claude-sonnet-4-6",
			"3": "claude-haiku-4-5-20251001",
			"4": "", // default (no override)
		}
		if modelID, ok := models[key]; ok {
			m.pickingProject = false
			m.projectModelStep = false
			return m.newSession(nil, m.projectEffort, modelID)
		}
		if key == "esc" {
			m.projectModelStep = false
			m.projectEffortStep = true
		}
		return m, nil
	}

	// Effort step: pick effort level inside the same overlay.
	if m.projectEffortStep {
		efforts := map[string]string{"1": "low", "2": "medium", "3": "high", "4": "max"}
		if effort, ok := efforts[key]; ok {
			m.projectEffortStep = false
			m.projectEffort = effort
			m.projectModelStep = true
			return m, nil
		}
		if key == "esc" {
			// Go back to project list.
			m.projectEffortStep = false
			m.projectCursor = 0
		}
		return m, nil
	}

	filtered := m.filteredProjectDirs()

	switch key {
	case "down":
		if m.projectCursor < len(filtered)-1 {
			m.projectCursor++
		}
	case "up":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case "enter":
		if len(filtered) > 0 && m.projectCursor < len(filtered) {
			m.effortWorkDir = filtered[m.projectCursor]
			m.projectFilter = ""
			if m.projectWithEffort {
				// Transition to effort step inside the same overlay.
				m.projectEffortStep = true
				return m, nil
			}
			m.pickingProject = false
			return m.newSession(nil, "", "")
		}
	case "esc":
		m.pickingProject = false
		m.projectDirs = nil
		m.projectLastUsed = nil
		m.projectFilter = ""
	case "backspace":
		if len(m.projectFilter) > 0 {
			m.projectFilter = m.projectFilter[:len(m.projectFilter)-1]
			m.projectCursor = 0
		}
	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.projectFilter += key
			m.projectCursor = 0
		}
	}
	return m, nil
}

// overlayProjectPicker composites the project picker as a floating box
// on top of the existing dashboard output.
func (m model) overlayProjectPicker(base string) string {
	boxW := 52
	if m.width > 90 {
		boxW = 62
	}
	innerW := boxW - 2 // inside the │ borders

	bc := lipgloss.Color(statusColors().Accent)
	bdr := lipgloss.NewStyle().Foreground(bc)

	// Helper: build a bordered row with exact inner width.
	row := func(content string) string {
		vis := lipgloss.Width(content)
		pad := innerW - vis
		if pad < 0 {
			pad = 0
		}
		return bdr.Render("│") + content + strings.Repeat(" ", pad) + bdr.Render("│")
	}
	emptyRow := row("")

	var lines []string

	if m.projectModelStep {
		// Model selection step.
		projName := filepath.Base(m.effortWorkDir)
		title := fmt.Sprintf(" %s · %s -- model ", projName, m.projectEffort)
		barW := boxW - 2 - len([]rune(title))
		if barW < 0 {
			barW = 0
		}
		lines = append(lines, bdr.Render("╭"+title+strings.Repeat("─", barW)+"╮"))
		lines = append(lines, emptyRow)

		modelOpts := []struct{ key, label string }{
			{"1", "opus"},
			{"2", "sonnet"},
			{"3", "haiku"},
			{"4", "default (no override)"},
		}
		for _, o := range modelOpts {
			label := fmt.Sprintf(" %s  %s", helpKeyStyle.Render(o.key), o.label)
			lines = append(lines, row(label))
		}

		lines = append(lines, emptyRow)
		lines = append(lines, row(dimStyle.Render(" 1-4 select  esc back")))
		lines = append(lines, bdr.Render("╰"+strings.Repeat("─", boxW-2)+"╯"))
	} else if m.projectEffortStep {
		// Effort selection step.
		projName := filepath.Base(m.effortWorkDir)
		title := " " + projName + " -- effort "
		barW := boxW - 2 - len([]rune(title))
		if barW < 0 {
			barW = 0
		}
		lines = append(lines, bdr.Render("╭"+title+strings.Repeat("─", barW)+"╮"))
		lines = append(lines, emptyRow)

		effortOpts := []struct{ key, label string }{
			{"1", "low"},
			{"2", "medium"},
			{"3", "high"},
			{"4", "max"},
		}
		for _, e := range effortOpts {
			label := fmt.Sprintf(" %s  %s", helpKeyStyle.Render(e.key), e.label)
			lines = append(lines, row(label))
		}

		lines = append(lines, emptyRow)
		lines = append(lines, row(dimStyle.Render(" 1-4 select  esc back")))
		lines = append(lines, bdr.Render("╰"+strings.Repeat("─", boxW-2)+"╯"))
	} else {
		// Project directory selection.
		title := " select project "
		if m.projectFilter != "" {
			title = fmt.Sprintf(" %s▏", m.projectFilter)
		}
		barW := boxW - 2 - len([]rune(title))
		if barW < 0 {
			barW = 0
		}
		lines = append(lines, bdr.Render("╭"+title+strings.Repeat("─", barW)+"╮"))
		lines = append(lines, emptyRow)

		filtered := m.filteredProjectDirs()
		if len(filtered) == 0 {
			lines = append(lines, row(dimStyle.Render(" no matching directories")))
		} else {
			maxVis := m.height/2 - 6
			if maxVis < 5 {
				maxVis = 5
			}
			if maxVis > len(filtered) {
				maxVis = len(filtered)
			}
			scroll := 0
			if m.projectCursor >= maxVis {
				scroll = m.projectCursor - maxVis + 1
			}
			end := scroll + maxVis
			if end > len(filtered) {
				end = len(filtered)
			}

			for i := scroll; i < end; i++ {
				dir := filtered[i]
				name := filepath.Base(dir)
				if dir == cfg.WorkDir {
					name = ". (root)"
				}

				hint := ""
				if t, ok := m.projectLastUsed[dir]; ok {
					hint = fmtTimeAgo(t)
				}

				hintW := len(hint)
				nameMax := innerW - hintW - 3 // 1 leading space + 1 gap + 1 trailing
				if nameMax < 8 {
					nameMax = 8
				}
				nr := []rune(name)
				if len(nr) > nameMax {
					name = string(nr[:nameMax-1]) + "…"
				}

				gap := innerW - 2 - len([]rune(name)) - hintW
				if gap < 1 {
					gap = 1
				}

				var content string
				if hint != "" {
					content = " " + name + strings.Repeat(" ", gap) + dimStyle.Render(hint) + " "
				} else {
					content = " " + name + strings.Repeat(" ", gap+hintW) + " "
				}

				if i == m.projectCursor {
					// Pad content to innerW then apply selected style.
					vis := lipgloss.Width(content)
					if vis < innerW {
						content += strings.Repeat(" ", innerW-vis)
					}
					content = selectedStyle.Render(content)
				}
				lines = append(lines, row(content))
			}
		}

		lines = append(lines, emptyRow)
		foot := " ↑/↓ navigate  enter select  esc cancel"
		if m.projectFilter != "" {
			foot = " ↑/↓ enter  backspace  esc"
		}
		lines = append(lines, row(dimStyle.Render(foot)))
		lines = append(lines, bdr.Render("╰"+strings.Repeat("─", boxW-2)+"╯"))
	}

	// Overlay: replace full base lines starting at row 2.
	baseLines := strings.Split(base, "\n")
	startRow := 2
	for i, pline := range lines {
		r := startRow + i
		if r < len(baseLines) {
			baseLines[r] = "  " + pline
		}
	}
	return strings.Join(baseLines, "\n")
}

// fmtTimeAgo formats a time as a human-readable "X ago" string.
func fmtTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// clearOrForkOutcome decides what a session-id transition (oldSession →
// newSession, same tmux window) means and what to do about it:
//
//   - /clear or /compact: the new session is blank. hide is true (collapse
//     the dashboard to one row) and newTitle carries the old name forward
//     as-is.
//   - /fork or resume-with-new-id: the new session has its own content. hide
//     is false (both rows stay visible) and newTitle gets a " fork" suffix
//     so the lineage is obvious.
//
// oldSession must be non-nil and non-archived -- callers are responsible for
// the compaction check (an archived/missing old session means the JSONL is
// gone; there's nothing to hide and no title worth inheriting, so skip
// calling this at all rather than pass a zero value). newSession may be nil
// (new session's SessionInfo hasn't shown up in a ListAllSessions pass yet);
// newTitle is empty whenever there's nothing to write, including when
// newSession already has a custom title of its own.
//
// Shared by the two independent paths that detect these transitions:
// pane-anchor retagging (immediate, hook-driven) and mtime/forkedFrom
// reconciliation (fallback, for windows with no pane-anchor signal yet).
func clearOrForkOutcome(oldSession, newSession *claude.SessionInfo) (hide bool, newTitle string) {
	newHasContent := newSession != nil && (newSession.Summary != "" || newSession.FirstPrompt != "")
	oldName := oldSession.DisplayName()

	if newSession == nil || newSession.CustomTitle != "" || oldName == "" {
		return !newHasContent, ""
	}
	if newHasContent {
		return false, oldName + " fork"
	}
	return true, oldName
}

// reconcileWindows matches managed windows to the claude sessions actually
// running inside them. This handles two cases:
// 1. New sessions (n key): tracked with tmpKey, sessionID empty
// 2. Forked sessions (/fork): old sessionID in map, new session running in window
//
// When a managed window's tracked session goes stale (JSONL not written
// recently), find the most recently active session in the same project and
// re-key -- the fallback for /clear and /fork transitions that happen
// before the pane-anchor hook fires (see clearOrForkOutcome).
func (m *model) reconcileWindows(sessions []claude.SessionInfo) {
	// Build sessionID → session lookup.
	sessionByID := make(map[string]*claude.SessionInfo)
	for i := range sessions {
		sessionByID[sessions[i].SessionID] = &sessions[i]
	}

	toDelete := []string{}
	toAdd := map[string]managedWindow{}

	// Only confirmed-superseded sessions can cause a real-ID window to be re-keyed.
	// This prevents concurrent sessions in the same project from stealing each other's windows.
	superseded := claude.GetSupersededSessions()

	for key, mw := range m.managedWindows {
		project := mw.project
		if project == "" {
			continue
		}

		isNewKey := strings.HasPrefix(key, "new-")

		// For real session IDs: only re-key if this session is confirmed superseded
		// (i.e. another session has a forkedFrom pointing here). This prevents concurrent
		// sessions in the same project from stealing each other's windows.
		if !isNewKey && !superseded[mw.sessionID] {
			continue
		}

		// Get the tracked session's mtime (if it exists).
		var trackedMtime time.Time
		if mw.sessionID != "" {
			if s, ok := sessionByID[mw.sessionID]; ok {
				trackedMtime = s.FileMtime
			}
		}

		// Find the most recently active session in the same project
		// that is NEWER than the tracked session.
		var bestID string
		var bestMtime time.Time
		for _, s := range sessions {
			if s.ProjectPath != project {
				continue
			}
			if s.SessionID == mw.sessionID {
				continue
			}
			if s.FileMtime.IsZero() {
				continue
			}
			// Skip sessions already tracked by another managed window.
			// This prevents a new-* window from stealing a session that
			// already has its own window.
			if _, alreadyTracked := m.managedWindows[s.SessionID]; alreadyTracked {
				continue
			}
			// Must be recent (< 60s) AND newer than the tracked session.
			if time.Since(s.FileMtime) < 60*time.Second &&
				s.FileMtime.After(trackedMtime) &&
				s.FileMtime.After(bestMtime) {
				bestID = s.SessionID
				bestMtime = s.FileMtime
			}
		}

		if bestID == "" || bestID == key {
			continue
		}

		// Re-key: remove old entry, add under new sessionID.
		debugLog("reconcile → re-key window %s: %q → %q (project=%q)", mw.windowID, key, bestID, mw.project)
		toDelete = append(toDelete, key)
		toAdd[bestID] = managedWindow{
			windowID:   mw.windowID,
			sessionID:  bestID,
			project:    mw.project,
			paneStatus: mw.paneStatus,
		}

		// Compaction: the old session's JSONL is archived/gone. Nothing to
		// hide and no title worth inheriting -- skip the clear/fork decision
		// entirely and let the new row stand on its own.
		oldSession, oldOnDisk := sessionByID[mw.sessionID]
		if oldOnDisk && oldSession.Status == claude.StatusArchived {
			oldOnDisk = false
		}
		if mw.sessionID != "" && !strings.HasPrefix(key, "new-") && oldOnDisk {
			newSession := sessionByID[bestID] // may be nil (map lookup on pointer type)
			hide, newTitle := clearOrForkOutcome(oldSession, newSession)
			debugLog("reconcile → type: hide=%v newTitle=%q", hide, newTitle)
			if hide {
				m.replacedSessions[key] = true
			}
			if newTitle != "" {
				claude.RenameSession(newSession.Dir, bestID, newTitle)
			}
		}
	}

	for _, k := range toDelete {
		delete(m.managedWindows, k)
	}
	for k, v := range toAdd {
		m.managedWindows[k] = v
		// Update the window tag so recovery after restart uses the new session ID.
		tmux.SetWindowEnv(v.windowID, "session-id", k)
	}
}

// reconcileStartupWindows scans existing tmux windows and re-populates
// managedWindows for windows that were opened by a previous c9s run.
// This prevents duplicate windows when the user re-opens a session after restart.
// Windows are NOT killed here -- the normal tick handler (reconcileWindows +
// GetSupersededSessions) handles cleanup once sessions are confirmed stale.
//
// Two adoption signals, in priority order:
//  1. Pane anchor -- a state file recorded $TMUX_PANE at its last hook fire.
//     This is authoritative even when the session id has drifted from the
//     static @session-id tag (e.g. after /resume or --session-id inside the
//     pane).
//  2. @session-id tag -- the id c9s wrote when it created the window. Used
//     when hooks haven't fired yet for that pane.
func (m *model) reconcileStartupWindows(sessions []claude.SessionInfo) {
	if !m.insideTmux {
		return
	}
	windows, err := tmux.ListWindows()
	if err != nil {
		return
	}
	sessionByID := make(map[string]*claude.SessionInfo)
	for i := range sessions {
		sessionByID[sessions[i].SessionID] = &sessions[i]
	}
	paneSession := sessionstate.PaneSessionMap()
	for _, w := range windows {
		if w.Name == tmux.DashboardWindow {
			continue
		}
		sid := paneSession[w.PaneID]
		fromPane := sid != ""
		if sid == "" {
			sid = w.SessionID
		}
		if sid == "" {
			continue
		}
		s, ok := sessionByID[sid]
		if !ok {
			debugLog("startup → orphan window %s name=%q session=%q (not in session list)", w.ID, w.Name, sid)
			continue
		}
		if _, alreadyTracked := m.managedWindows[sid]; alreadyTracked {
			continue
		}
		debugLog("startup → recovered window %s name=%q session=%q project=%q (fromPane=%v)",
			w.ID, w.Name, sid, s.ProjectPath, fromPane)
		m.managedWindows[sid] = managedWindow{
			windowID:  w.ID,
			sessionID: sid,
			project:   s.ProjectPath,
		}
		// Re-align the tmux tag if the pane anchor disagreed with it. Cheap
		// idempotent write when they already match.
		if w.SessionID != sid {
			tmux.SetWindowEnv(w.ID, "session-id", sid)
		}
		seedSessionState(sid, s.ProjectPath)
	}
}

// retagEvent records a single (oldKey → newID) window rebind done by the
// pane-anchor pass. The follow-up handler consumes these to hide the old
// dashboard row and carry the title over so /clear feels like a continuation
// of the same session rather than a jump to a new one.
type retagEvent struct {
	oldKey   string // previous managedWindows key (may be a `new-*` tmpKey)
	newID    string // pane-anchored session id
	windowID string
	project  string
}

// retagFromPaneAnchors uses the tmux pane id recorded by Claude Code hooks to
// realign managed windows with the session id they're actually running right
// now. This is the only signal that survives a session-id change from inside
// the pane (/resume, /clear, compact, --session-id) -- the static @session-id
// tag we set at window creation goes stale in those cases.
//
// Returns the list of retag events performed; empty when nothing moved.
func (m *model) retagFromPaneAnchors() []retagEvent {
	if !m.insideTmux || len(m.managedWindows) == 0 {
		return nil
	}
	windows, err := tmux.ListWindows()
	if err != nil {
		return nil
	}
	return m.applyPaneAnchorRetag(windows, sessionstate.PaneSessionMap())
}

// applyPaneAnchorRetag is the pure logic behind retagFromPaneAnchors, split
// out for testability. Two things happen here:
//
//  1. Retag drifted windows -- a managed window whose active pane anchors to a
//     session id different from its map key is rebound to the pane's id.
//  2. Adopt orphan windows -- a tmux window whose pane anchors to a known
//     session id but that isn't in managedWindows yet gets added (this covers
//     the `n`-born, tag-less window whose Claude switched sessions via
//     /resume before c9s could see the change).
//
// Returns the retagEvents performed; nil when nothing changed.
func (m *model) applyPaneAnchorRetag(windows []tmux.WindowInfo, paneSession map[string]string) []retagEvent {
	if len(paneSession) == 0 {
		return nil
	}
	windowReal := make(map[string]string, len(windows))
	windowProject := make(map[string]string, len(windows))
	for _, w := range windows {
		if w.PaneID == "" || w.Name == tmux.DashboardWindow {
			continue
		}
		if realID := paneSession[w.PaneID]; realID != "" {
			windowReal[w.ID] = realID
			windowProject[w.ID] = "" // project resolved from managed entry when available
		}
	}
	var events []retagEvent
	// Pass 1: retag drifted managed windows.
	for key, mw := range m.managedWindows {
		realID, ok := windowReal[mw.windowID]
		if !ok {
			continue
		}
		windowProject[mw.windowID] = mw.project // remember for pass-2 adoption
		if realID == key {
			continue
		}
		debugLog("retag → window %s: %q → %q (pane anchor)", mw.windowID, key, realID)
		// If another entry already claims realID with a different window,
		// the pane anchor is authoritative -- drop the impostor so we don't
		// leave two entries for the same id. Its window will be retagged
		// in this pass or the next when its own pane emits a hook.
		if prev, exists := m.managedWindows[realID]; exists && prev.windowID != mw.windowID {
			debugLog("retag → dropping stale entry %q → window %s", realID, prev.windowID)
			delete(m.managedWindows, realID)
		}
		delete(m.managedWindows, key)
		m.managedWindows[realID] = managedWindow{
			windowID:   mw.windowID,
			sessionID:  realID,
			project:    mw.project,
			paneStatus: mw.paneStatus,
		}
		tmux.SetWindowEnv(mw.windowID, "session-id", realID)
		events = append(events, retagEvent{
			oldKey:   key,
			newID:    realID,
			windowID: mw.windowID,
			project:  mw.project,
		})
	}
	// Pass 2: adopt orphan windows whose pane anchors to a session not
	// already tracked. This picks up untagged `n`-born windows where Claude
	// switched sessions from inside the pane before c9s could observe it.
	tracked := make(map[string]string, len(m.managedWindows)) // windowID → key
	for key, mw := range m.managedWindows {
		tracked[mw.windowID] = key
	}
	for wid, realID := range windowReal {
		if _, alreadyManaged := m.managedWindows[realID]; alreadyManaged {
			continue
		}
		if _, thisWindowAlreadyTracked := tracked[wid]; thisWindowAlreadyTracked {
			continue
		}
		debugLog("adopt → window %s → session %q (pane anchor)", wid, realID)
		m.managedWindows[realID] = managedWindow{
			windowID:  wid,
			sessionID: realID,
			project:   windowProject[wid],
		}
		tmux.SetWindowEnv(wid, "session-id", realID)
		events = append(events, retagEvent{
			oldKey:   "",
			newID:    realID,
			windowID: wid,
			project:  windowProject[wid],
		})
	}
	return events
}

// applyClearOrForkEffects reacts to a pane-anchor rebind (Claude Code switched
// session-ids inside an existing pane). Two cases:
//
//   - /clear or /compact: the new session is blank. The old row is hidden so
//     the dashboard collapses to a single entry that keeps routing Enter to
//     the same tmux window, and the old title is carried forward.
//   - /fork or resume-with-new-id: the new session has its own content. Both
//     rows stay visible so the user can still open the old one; the new row
//     gets a "<old> fork" title so the lineage is obvious.
//
// Ignored for adoption events (empty oldKey) and for tmpKey `new-*` bindings,
// which don't correspond to an existing dashboard row.
func (m *model) applyClearOrForkEffects(events []retagEvent, sessions []claude.SessionInfo) bool {
	if len(events) == 0 {
		return false
	}
	byID := make(map[string]*claude.SessionInfo, len(sessions))
	for i := range sessions {
		byID[sessions[i].SessionID] = &sessions[i]
	}
	changed := false
	for _, ev := range events {
		if ev.oldKey == "" || strings.HasPrefix(ev.oldKey, "new-") {
			continue
		}
		if m.replacedSessions[ev.oldKey] {
			continue
		}
		oldSession, oldOK := byID[ev.oldKey]
		if !oldOK || oldSession.Status == claude.StatusArchived {
			// Compaction path: the old JSONL is gone. Nothing to hide and
			// no title to inherit -- the new row stands on its own.
			continue
		}
		newSession := byID[ev.newID] // may be nil (map lookup on pointer type)
		hide, newTitle := clearOrForkOutcome(oldSession, newSession)
		if hide {
			m.replacedSessions[ev.oldKey] = true
			changed = true
		}
		if newTitle != "" {
			claude.RenameSession(newSession.Dir, ev.newID, newTitle)
		}
	}
	return changed
}

// saveDashboardState persists toggle states to config so they survive restarts.
func (m model) saveDashboardState() {
	cfg.Dashboard = config.Dashboard{
		ShowTokens:       m.showTokens,
		ShowPreview:      m.showPreview,
		GroupBy:          int(m.groupBy),
		ReplacedSessions: m.replacedSessionsList(),
	}
	config.Save(cfg)
}

// loadReplacedSessions loads the replaced sessions set from persisted config.
func loadReplacedSessions() map[string]bool {
	m := make(map[string]bool)
	for _, id := range cfg.Dashboard.ReplacedSessions {
		m[id] = true
	}
	return m
}

// replacedSessionsList converts the map to a slice for persistence.
func (m model) replacedSessionsList() []string {
	if len(m.replacedSessions) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.replacedSessions))
	for id := range m.replacedSessions {
		out = append(out, id)
	}
	return out
}

// enterUsageScreen switches to the usage history view.
func (m model) enterUsageScreen() (tea.Model, tea.Cmd) {
	m.usageScreen = true
	return m, nil
}

// updateUsage handles key input on the usage history screen.
func (m model) updateUsage(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.usageScreen = false
	case "d":
		m.usageViewMode = 0 // daily
	case "w":
		m.usageViewMode = 1 // weekly
	case "m":
		m.usageViewMode = 2 // monthly
	case "7":
		m.usageViewMode = 0
		m.usageDayRange = 7
	case "1":
		m.usageViewMode = 0
		m.usageDayRange = 14
	case "3":
		m.usageViewMode = 0
		m.usageDayRange = 30
	}
	return m, nil
}

// demoSessionView renders a screen that looks like a real Claude Code session.
func (m model) demoSessionView() string {
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color("#b388ff"))
	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0e0e0"))
	fg := lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666688"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#66bb6a"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("#4dd0e1"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd54f"))
	toolBorder := lipgloss.NewStyle().Foreground(lipgloss.Color("#555577"))

	var b strings.Builder

	// User message
	b.WriteString("\n")
	b.WriteString(purple.Render(" ❯ ") + bold.Render("refactor the auth middleware to use JWT tokens"))
	b.WriteString("\n\n")

	// Assistant response
	b.WriteString(fg.Render("  I'll refactor the auth middleware from session cookies to JWT tokens."))
	b.WriteString("\n")
	b.WriteString(fg.Render("  Let me start by reading the current implementation."))
	b.WriteString("\n\n")

	// Tool: Read file
	b.WriteString(toolBorder.Render("  ╭─ ") + cyan.Render("Read") + dim.Render(" src/auth/middleware.go"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("   1  package auth"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("   2  "))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("   3  func SessionMiddleware(next http.Handler) http.Handler {"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("   4      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("   5          cookie, err := r.Cookie(\"session_id\")"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("  ...  (42 lines)"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  ╰─"))
	b.WriteString("\n\n")

	// More response
	b.WriteString(fg.Render("  Now I'll update it to use JWT verification:"))
	b.WriteString("\n\n")

	// Tool: Edit file
	b.WriteString(toolBorder.Render("  ╭─ ") + yellow.Render("Edit") + dim.Render(" src/auth/middleware.go"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + green.Render(" + ") + fg.Render("func JWTMiddleware(secret []byte) func(http.Handler) http.Handler {"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + green.Render(" + ") + fg.Render("    return func(next http.Handler) http.Handler {"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + green.Render(" + ") + fg.Render("        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + green.Render(" + ") + fg.Render("            token := r.Header.Get(\"Authorization\")"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + green.Render(" + ") + fg.Render("            claims, err := validateJWT(token, secret)"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  │ ") + dim.Render("  ... (+18 lines)"))
	b.WriteString("\n")
	b.WriteString(toolBorder.Render("  ╰─"))
	b.WriteString("\n\n")

	// Prompt
	b.WriteString(purple.Render(" ❯ "))
	b.WriteString(dim.Render("press any key to return to c9s dashboard"))

	return b.String()
}

// usageView renders the usage history screen.
func (m model) usageView() string {
	var points []claude.UsageDataPoint
	if m.demoMode {
		points = claude.DemoUsageHistory()
	} else {
		points = claude.LoadUsageHistory()
	}

	var b strings.Builder

	// Title
	modeLabel := "daily"
	switch m.usageViewMode {
	case 1:
		modeLabel = "weekly"
	case 2:
		modeLabel = "monthly"
	}
	rangeLabel := ""
	if m.usageViewMode == 0 {
		rangeLabel = fmt.Sprintf(" (%dd)", m.usageDayRange)
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(statusColors().Accent))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColors().Dim))

	b.WriteString(titleStyle.Render(" c9s -- usage history"))
	padding := m.width - 21 - len(modeLabel) - len(rangeLabel) - 1
	if padding > 0 {
		b.WriteString(strings.Repeat(" ", padding))
	}
	b.WriteString(dimStyle.Render(modeLabel + rangeLabel))
	b.WriteString("\n\n")

	if len(points) == 0 {
		b.WriteString(dimStyle.Render("  No usage data yet. Data will appear after the API is polled."))
		b.WriteString("\n")
	} else {
		// Aggregate data points into rows.
		rows := m.aggregateUsageRows(points)

		// Header -- built the same way as data rows so bar-char widths match.
		hdr := "  " + fmt.Sprintf("%-14s", "Date") +
			fmt.Sprintf("%-12s", "5h peak") + fmt.Sprintf("%-8s", "") +
			fmt.Sprintf("%-12s", "7d last") + fmt.Sprintf("%-8s", "") +
			fmt.Sprintf("%8s", "Tokens") + "    Models"
		b.WriteString(dimStyle.Render(hdr))
		b.WriteString("\n")

		maxRows := m.height - 6 // title + header + footer + padding
		if maxRows < 1 {
			maxRows = 1
		}
		if len(rows) > maxRows {
			rows = rows[:maxRows]
		}

		for _, r := range rows {
			b.WriteString("  ")
			b.WriteString(fmt.Sprintf("%-14s", r.label))
			b.WriteString(usageBar(r.fiveHour, 12))
			b.WriteString(fmt.Sprintf(" %3.0f%%   ", r.fiveHour))
			b.WriteString(usageBar(r.sevenDay, 12))
			b.WriteString(fmt.Sprintf(" %3.0f%%   ", r.sevenDay))
			if r.tokens > 0 {
				b.WriteString(fmt.Sprintf("%8s", fmtTokens(r.tokens)))
			} else {
				b.WriteString(dimStyle.Render("       --"))
			}
			if len(r.models) > 0 {
				b.WriteString("    ")
				b.WriteString(dimStyle.Render(formatModelPcts(r.models)))
			}
			b.WriteString("\n")
		}
	}

	// Fill remaining space
	lines := strings.Count(b.String(), "\n")
	for lines < m.height-2 {
		b.WriteString("\n")
		lines++
	}

	// Footer
	b.WriteString(dimStyle.Render("  d daily  w weekly  m monthly  7/14/30 range  q back"))
	return b.String()
}

type usageRow struct {
	label    string
	fiveHour float64
	sevenDay float64
	tokens   int
	models   map[string]float64 // model name → percentage of token delta
}

// aggregateUsageRows groups data points into display rows based on view mode.
func (m model) aggregateUsageRows(points []claude.UsageDataPoint) []usageRow {
	type bucket struct {
		key         string
		label       string
		peak5h      float64
		last7d      float64
		maxToken    int
		minToken    int
		firstModels map[string]int // earliest non-nil model snapshot in bucket
		lastModels  map[string]int // most recent non-nil model snapshot in bucket
		hasData     bool
	}

	bucketKey := func(t time.Time) (string, string) {
		local := t.Local()
		switch m.usageViewMode {
		case 1: // weekly
			y, w := local.ISOWeek()
			// Label as the Monday of that week.
			// Find Monday: go back (weekday - 1) days, Sunday = 0 → back 6.
			wd := int(local.Weekday())
			if wd == 0 {
				wd = 7
			}
			monday := local.AddDate(0, 0, -(wd - 1))
			return fmt.Sprintf("%d-W%02d", y, w), monday.Format("Jan 02")
		case 2: // monthly
			return local.Format("2006-01"), local.Format("Jan 2006")
		default: // daily
			return local.Format("2006-01-02"), local.Format("Jan 02 Mon")
		}
	}

	// Determine cutoff.
	now := time.Now()
	var cutoff time.Time
	switch m.usageViewMode {
	case 1:
		cutoff = now.AddDate(0, 0, -12*7)
	case 2:
		cutoff = now.AddDate(0, -6, 0)
	default:
		cutoff = now.AddDate(0, 0, -m.usageDayRange)
	}

	buckets := map[string]*bucket{}
	var order []string

	for _, p := range points {
		if p.Time.Before(cutoff) {
			continue
		}
		key, label := bucketKey(p.Time)
		bk, ok := buckets[key]
		if !ok {
			bk = &bucket{key: key, label: label, minToken: p.Tokens}
			buckets[key] = bk
			order = append(order, key)
		}
		if p.FiveHour > bk.peak5h {
			bk.peak5h = p.FiveHour
		}
		bk.last7d = p.SevenDay // last sample wins
		if p.Tokens > bk.maxToken {
			bk.maxToken = p.Tokens
		}
		if p.Tokens < bk.minToken {
			bk.minToken = p.Tokens
		}
		if p.Models != nil {
			if bk.firstModels == nil {
				bk.firstModels = p.Models
			}
			bk.lastModels = p.Models
		}
		bk.hasData = true
	}

	// Build rows in reverse chronological order.
	var rows []usageRow
	for i := len(order) - 1; i >= 0; i-- {
		bk := buckets[order[i]]
		tokens := bk.maxToken - bk.minToken
		if tokens < 0 {
			tokens = 0
		}
		// Compute model deltas: tokens added during this bucket period.
		var models map[string]float64
		if len(bk.lastModels) > 0 {
			deltas := make(map[string]int)
			for m, v := range bk.lastModels {
				delta := v
				if bk.firstModels != nil {
					delta -= bk.firstModels[m]
				}
				if delta > 0 {
					deltas[m] = delta
				}
			}
			var total int
			for _, v := range deltas {
				total += v
			}
			if total > 0 {
				models = make(map[string]float64)
				for m, v := range deltas {
					pct := float64(v) / float64(total) * 100
					if pct >= 1 {
						models[m] = pct
					}
				}
			}
		}
		rows = append(rows, usageRow{
			label:    bk.label,
			fiveHour: bk.peak5h,
			sevenDay: bk.last7d,
			tokens:   tokens,
			models:   models,
		})
	}
	return rows
}

// usageBar renders a bar chart: filled █ and empty ░.
func usageBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// shortModelName converts a full model ID to a short display name.
// Examples: "claude-opus-4-6" → "opus", "claude-3-5-sonnet-20241022" → "sonnet"
func shortModelName(model string) string {
	name := strings.TrimPrefix(model, "claude-")
	parts := strings.Split(name, "-")
	var family []string
	for _, p := range parts {
		if len(p) > 0 && (p[0] < '0' || p[0] > '9') {
			family = append(family, p)
		}
	}
	if len(family) == 0 {
		return name
	}
	return strings.Join(family, "-")
}

// formatModelPcts formats a model percentage map as a sorted string.
// Models are sorted by descending percentage. E.g. "opus 73%  sonnet 27%"
func formatModelPcts(models map[string]float64) string {
	type mp struct {
		name string
		pct  float64
	}
	var sorted []mp
	for m, p := range models {
		sorted = append(sorted, mp{shortModelName(m), p})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].pct > sorted[j].pct
	})
	var parts []string
	for _, m := range sorted {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", m.name, m.pct))
	}
	return strings.Join(parts, "  ")
}

// enterConfigScreen switches to the in-app config editor.
func (m model) enterConfigScreen() (tea.Model, tea.Cmd) {
	m.configScreen = true
	m.configDraft = cfg
	m.configFields = config.EditableFields()
	m.configCursor = 0
	m.configScroll = 0
	m.configEditing = false
	return m, nil
}

// configDisplayItem is either a section header or an editable field row.
type configDisplayItem struct {
	isHeader bool
	header   string
	fieldIdx int // index into m.configFields
}

// configVisibleItems returns the list of visible items for the config screen,
// hiding individual color fields when theme is "default".
func (m model) configVisibleItems() []configDisplayItem {
	var items []configDisplayItem
	lastSection := ""
	for i, f := range m.configFields {
		// Hide individual color fields when theme is "default".
		if f.Section == "Theme" && f.Key != "theme" && m.configDraft.Theme != "custom" {
			continue
		}
		if f.Section != lastSection {
			items = append(items, configDisplayItem{isHeader: true, header: f.Section})
			lastSection = f.Section
		}
		items = append(items, configDisplayItem{fieldIdx: i})
	}
	return items
}

// updateConfig handles all key input on the config screen.
func (m model) updateConfig(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.configConfirming {
		return m.updateConfigConfirm(msg)
	}
	if m.configEditing {
		return m.updateConfigEdit(msg)
	}
	return m.updateConfigNav(msg)
}

func (m model) updateConfigConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.configConfirming = false
		switch m.configConfirmKey {
		case "reset_history":
			if err := claude.ResetUsageHistory(); err != nil {
				return m, statusCmd("Failed to reset: "+err.Error(), true)
			}
			return m, statusCmd("Usage history cleared", false)
		}
	case "n", "esc":
		m.configConfirming = false
	}
	return m, nil
}

func (m model) updateConfigNav(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.configVisibleItems()
	switch msg.String() {
	case "q", "esc":
		m.configScreen = false
		return m, nil
	case "s":
		return m.saveConfig()
	case "d":
		m.configDraft = config.Default()
		return m, nil
	case "?":
		m.configShowDesc = !m.configShowDesc
	case "up", "k":
		if m.configCursor > 0 {
			m.configCursor--
			m.skipConfigHeaders(items, -1)
		}
	case "down", "j":
		if m.configCursor < len(items)-1 {
			m.configCursor++
			m.skipConfigHeaders(items, 1)
		}
	case "enter", "space":
		if m.configCursor >= 0 && m.configCursor < len(items) && !items[m.configCursor].isHeader {
			f := m.configFields[items[m.configCursor].fieldIdx]
			if f.Action {
				m.configConfirming = true
				m.configConfirmKey = f.Key
				return m, nil
			}
			if len(f.Options) > 0 {
				// Cycle through options.
				current := f.Get(m.configDraft)
				next := f.Options[0]
				for i, opt := range f.Options {
					if opt == current && i+1 < len(f.Options) {
						next = f.Options[i+1]
						break
					}
				}
				f.Set(&m.configDraft, next)
				return m, nil
			}
			// Start editing (free text input).
			m.configEditing = true
			m.configEditIdx = items[m.configCursor].fieldIdx
			m.configInput.SetValue(f.Get(m.configDraft))
			m.configInput.CursorEnd()
			return m, m.configInput.Focus()
		}
	}
	m.adjustConfigScroll(items)
	return m, nil
}

func (m model) updateConfigEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		f := m.configFields[m.configEditIdx]
		f.Set(&m.configDraft, m.configInput.Value())
		m.configEditing = false
		m.configInput.Blur()
		return m, nil
	case "esc":
		m.configEditing = false
		m.configInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.configInput, cmd = m.configInput.Update(msg)
	return m, cmd
}

func (m model) saveConfig() (tea.Model, tea.Cmd) {
	oldKeys := navKeys()

	// Save to disk.
	if err := config.Save(m.configDraft); err != nil {
		m.configScreen = false
		return m, statusCmd(fmt.Sprintf("save config: %v", err), true)
	}

	// Apply in-process.
	cfg = m.configDraft
	applyColors(cfg.EffectiveColors())

	// Rebind tmux keys and refresh status bar if inside tmux.
	newKeys := navKeys()
	if m.insideTmux {
		if oldKeys != newKeys {
			tmux.CleanupNavigationKeys(oldKeys)
			tmux.SetupNavigationKeys(newKeys)
		}
		tmux.ConfigureStatusBar(newKeys, statusColors(), version, cfg.ScrollSpeed, cfg.RefreshSeconds)
	}

	m.configScreen = false
	return m, statusCmd("config saved", false)
}

func (m *model) skipConfigHeaders(items []configDisplayItem, dir int) {
	for m.configCursor >= 0 && m.configCursor < len(items) && items[m.configCursor].isHeader {
		m.configCursor += dir
	}
	if m.configCursor < 0 {
		m.configCursor = 0
	}
	if m.configCursor >= len(items) {
		m.configCursor = len(items) - 1
	}
}

func (m *model) adjustConfigScroll(items []configDisplayItem) {
	visible := m.height - 4 // title + footer + padding
	if visible < 1 {
		visible = 1
	}
	if m.configCursor < m.configScroll {
		m.configScroll = m.configCursor
	}
	if m.configCursor >= m.configScroll+visible {
		m.configScroll = m.configCursor - visible + 1
	}
}

// configView renders the config screen.
func (m model) configView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(" c9s -- config"))
	b.WriteString("\n\n")

	items := m.configVisibleItems()
	visible := m.height - 4
	if visible < 1 {
		visible = 1
	}

	end := m.configScroll + visible
	if end > len(items) {
		end = len(items)
	}

	labelWidth := 20

	for i := m.configScroll; i < end; i++ {
		item := items[i]
		if item.isHeader {
			line := fmt.Sprintf("── %s ", item.header)
			pad := m.width - len(line)
			if pad > 0 {
				line += strings.Repeat("─", pad)
			}
			b.WriteString(groupStyle.Render(line))
			b.WriteString("\n")
			continue
		}

		f := m.configFields[item.fieldIdx]
		val := f.Get(m.configDraft)
		label := fmt.Sprintf("  %-*s", labelWidth, f.Label)

		// Show edit input for the field being edited.
		var row string
		if f.Action {
			row = "  " + dimStyle.Render(f.Label)
		} else if m.configEditing && item.fieldIdx == m.configEditIdx {
			row = dimStyle.Render(label) + m.configInput.View()
		} else if len(f.Options) > 0 {
			// Dropdown-style: show all options, highlight current.
			var optParts []string
			for _, opt := range f.Options {
				if opt == val {
					optParts = append(optParts, helpKeyStyle.Render(opt))
				} else {
					optParts = append(optParts, dimStyle.Render(opt))
				}
			}
			row = dimStyle.Render(label) + "◀ " + strings.Join(optParts, " | ") + " ▸"
		} else {
			// Color fields: show a color swatch.
			valDisplay := previewVal.Render(val)
			if f.Section == "Theme" && f.Key != "theme" {
				swatch := lipgloss.NewStyle().Background(lipgloss.Color(val)).Render("  ")
				valDisplay = swatch + " " + previewVal.Render(val)
			}
			row = dimStyle.Render(label) + valDisplay
		}

		if i == m.configCursor {
			// Pad to full width for selection highlight.
			plain := label + val
			pad := m.width - len(plain)
			if pad < 0 {
				pad = 0
			}
			row = selectedStyle.Render(row + strings.Repeat(" ", pad))
		}

		b.WriteString(row)
		b.WriteString("\n")

		// Show description below the selected field when ? is toggled.
		if m.configShowDesc && i == m.configCursor && f.Desc != "" {
			b.WriteString(dimStyle.Render("    " + f.Desc))
			b.WriteString("\n")
		}
	}

	// Fill remaining lines.
	used := 2 // title + blank
	for i := m.configScroll; i < end; i++ {
		used++
	}
	for used < m.height-1 {
		b.WriteString("\n")
		used++
	}

	// Footer.
	b.WriteString(m.configFooter())

	return b.String()
}

func (m model) configFooter() string {
	if m.configConfirming {
		label := "Confirm action?"
		if m.configConfirmKey == "reset_history" {
			label = "Reset all usage history?"
		}
		return helpStyle.Render(" " + label + "  " +
			helpKeyStyle.Render("y") + " confirm  " +
			helpKeyStyle.Render("esc") + " cancel")
	}
	if m.configEditing {
		return helpStyle.Render(" " +
			helpKeyStyle.Render("enter") + " accept  " +
			helpKeyStyle.Render("esc") + " cancel")
	}
	descLabel := "show help"
	if m.configShowDesc {
		descLabel = "hide help"
	}
	return helpStyle.Render(" " +
		helpKeyStyle.Render("j/k") + " nav  " +
		helpKeyStyle.Render("enter") + " edit  " +
		helpKeyStyle.Render("space") + " toggle  " +
		helpKeyStyle.Render("?") + " " + descLabel + "  " +
		helpKeyStyle.Render("d") + " reset  " +
		helpKeyStyle.Render("s") + " save  " +
		helpKeyStyle.Render("q") + " cancel")
}

// backupSession backs up the selected session's JSONL file.
func (m model) backupSession(items []displayItem) (tea.Model, tea.Cmd) {
	s := m.selectedSession(items)
	if s == nil {
		return m, nil
	}
	if s.Dir == "" {
		return m, statusCmd("no project directory for this session", true)
	}
	if s.Status == claude.StatusArchived {
		return m, statusCmd("no JSONL file to back up (archived)", true)
	}

	if err := claude.BackupSession(s); err != nil {
		return m, statusCmd(fmt.Sprintf("backup failed: %v", err), true)
	}
	return m, statusCmd("backed up "+s.DisplayName(), false)
}

// closeWindow closes the managed tmux window for the selected session.
func (m model) closeWindow(items []displayItem) (tea.Model, tea.Cmd) {
	if !m.insideTmux {
		return m, nil
	}
	s := m.selectedSession(items)
	if s == nil {
		return m, nil
	}

	mw, ok := m.managedWindows[s.SessionID]
	if !ok {
		return m, statusCmd("no managed window for this session", true)
	}

	debugLog("closeWindow session=%q window=%s", s.SessionID, mw.windowID)
	tmux.KillWindow(mw.windowID)
	delete(m.managedWindows, s.SessionID)
	// kill-window doesn't give the pane's claude process a chance to run its
	// SessionEnd hook, so release its pane claim ourselves.
	sessionstate.Remove(s.SessionID)
	return m, statusCmd("window closed", false)
}

// startRename enters rename mode for the selected session.
func (m model) startRename(items []displayItem) (tea.Model, tea.Cmd) {
	s := m.selectedSession(items)
	if s == nil {
		return m, nil
	}
	if s.Dir == "" {
		return m, statusCmd("no project directory for this session", true)
	}
	m.renaming = true
	m.renameSession = s
	m.renameInput.Prompt = fmt.Sprintf("rename (was: %s): ", s.DisplayName())
	m.renameInput.SetValue("")
	return m, m.renameInput.Focus()
}

// updateRename handles keypresses while in rename mode.
func (m model) updateRename(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.renaming = false
		m.renameSession = nil
		m.renameInput.Blur()
		return m, nil
	case "enter":
		newTitle := strings.TrimSpace(m.renameInput.Value())
		m.renaming = false
		s := m.renameSession
		m.renameSession = nil
		m.renameInput.Blur()
		if newTitle == "" || s == nil {
			return m, nil
		}
		if err := claude.RenameSession(s.Dir, s.SessionID, newTitle); err != nil {
			return m, statusCmd(fmt.Sprintf("rename failed: %v", err), true)
		}
		// Update tmux window name if there's a managed window for this session.
		if mw, ok := m.managedWindows[s.SessionID]; ok {
			tmux.RenameWindow(mw.windowID, truncWindowName(newTitle))
		}
		// Refresh sessions immediately to show the new name.
		sessions, err := claude.ListAllSessions()
		if err == nil {
			m.sessions = sessions
			// Restore cursor to the renamed session.
			for i, item := range m.items() {
				if !item.isHeader && !item.isWorktreeRow && item.session.SessionID == s.SessionID {
					m.cursor = i
					break
				}
			}
			m.adjustScroll()
		}
		return m, statusCmd("renamed to \""+newTitle+"\"", false)
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// worktreesActive reports whether the worktree feature has any effect right
// now -- used to gate the w/a/d handlers and to decide what footer hint to
// show. Being "active" requires: feature toggled on AND grouping by project
// (worktrees are project-scoped and only render inside a project group).
func (m model) worktreesActive() bool {
	return cfg.Worktrees == "on" && m.groupBy == groupProject
}

// projectAtCursor returns the repo path associated with the row under the
// cursor, resolving upward through parent links so any row inside a project
// group answers with the same project.
func (m model) projectAtCursor(items []displayItem) string {
	if m.cursor < 0 || m.cursor >= len(items) {
		return ""
	}
	item := items[m.cursor]
	if item.parentProject != "" {
		return item.parentProject
	}
	if item.session.ProjectPath != "" {
		return item.session.ProjectPath
	}
	return ""
}

// toggleWorktree implements the `w` key. Semantics vary by cursor context:
//
//   - on a worktree row / empty-worktree placeholder: collapse or expand that
//     specific worktree's session list.
//   - on a session row nested under a worktree: collapse the enclosing
//     worktree.
//   - on a project header: bulk toggle every worktree under that project
//     (collapse-all if any is currently expanded, else expand-all).
//
// Outside groupBy=project, it prints a footer hint instead of silently doing
// nothing so the user learns where the feature lives.
func (m model) toggleWorktree(items []displayItem) (tea.Model, tea.Cmd) {
	if !m.worktreesActive() {
		return m, statusCmd("worktrees show up when grouped by project -- press Tab", false)
	}
	if m.cursor < 0 || m.cursor >= len(items) {
		return m, nil
	}
	item := items[m.cursor]
	switch {
	case item.isWorktreeRow || item.isEmptyWorktree:
		p := item.worktree.Path
		m.collapsedWorktrees[p] = !m.collapsedWorktrees[p]
	case item.isHeader && item.parentProject != "":
		wts := m.lookupWorktrees(item.parentProject)
		anyExpanded := false
		for _, wt := range wts {
			if !m.collapsedWorktrees[wt.Path] {
				anyExpanded = true
				break
			}
		}
		for _, wt := range wts {
			m.collapsedWorktrees[wt.Path] = anyExpanded
		}
	case !item.isHeader && item.parentWorktree != "":
		p := item.parentWorktree
		m.collapsedWorktrees[p] = !m.collapsedWorktrees[p]
	}
	return m, nil
}

// startAddWorktree opens a single-line prompt for a branch name. Works from
// any row inside a project group; the new worktree is added to that project.
func (m model) startAddWorktree(items []displayItem) (tea.Model, tea.Cmd) {
	repo := m.projectAtCursor(items)
	if repo == "" {
		return m, statusCmd("place the cursor on a project row first", true)
	}
	if !git.Available() {
		return m, statusCmd("git not installed", true)
	}
	m.addingWorktree = true
	m.addWorktreeRepo = repo
	m.renameInput.Prompt = fmt.Sprintf("new worktree branch (in %s): ", filepath.Base(repo))
	m.renameInput.SetValue("")
	return m, m.renameInput.Focus()
}

func (m model) updateAddWorktree(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.addingWorktree = false
		m.addWorktreeRepo = ""
		m.renameInput.Blur()
		return m, nil
	case "enter":
		branch := strings.TrimSpace(m.renameInput.Value())
		repo := m.addWorktreeRepo
		m.addingWorktree = false
		m.addWorktreeRepo = ""
		m.renameInput.Blur()
		if branch == "" || repo == "" {
			return m, nil
		}
		wtPath, err := git.CreateWorktree(repo, branch)
		if err != nil {
			return m, statusCmd(fmt.Sprintf("worktree add: %v", err), true)
		}
		// Force a cache refresh so the new worktree renders immediately,
		// before the next tick. Newly-added worktrees start expanded (the
		// collapsedWorktrees map treats a miss as expanded).
		delete(m.worktreeCache, repo)
		m.lookupWorktrees(repo)
		return m, statusCmd(fmt.Sprintf("worktree '%s' at %s", branch, wtPath), false)
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// startDeleteWorktree triggers the delete confirmation overlay. It works on
// worktree rows (both populated and empty-placeholder). The main worktree is
// refused outright -- removing it would orphan the repo.
func (m model) startDeleteWorktree(items []displayItem) (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(items) {
		return m, nil
	}
	item := items[m.cursor]
	if !item.isWorktreeRow && !item.isEmptyWorktree {
		return m, statusCmd("place the cursor on a worktree row to delete it", true)
	}
	if item.worktree.IsMain {
		return m, statusCmd("refusing to delete the main worktree", true)
	}
	repo := item.parentProject
	if repo == "" {
		return m, statusCmd("could not locate owning repo for this worktree", true)
	}
	m.deletingWorktree = true
	m.deleteWorktree = item.worktree
	m.deleteWorktreeRepo = repo
	m.deleteWorktreeForce = false
	return m, nil
}

func (m model) updateDeleteWorktree(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.deletingWorktree = false
		m.deleteWorktree = git.Worktree{}
		m.deleteWorktreeRepo = ""
		m.deleteWorktreeForce = false
		return m, statusCmd("delete cancelled", false)
	case "y", "enter":
		wt := m.deleteWorktree
		repo := m.deleteWorktreeRepo
		force := m.deleteWorktreeForce
		err := git.RemoveWorktree(repo, wt.Path, force)
		if err != nil && !force && wt.Dirty {
			// First attempt failed; offer a force-confirm instead of bailing.
			m.deleteWorktreeForce = true
			return m, nil
		}
		m.deletingWorktree = false
		m.deleteWorktree = git.Worktree{}
		m.deleteWorktreeRepo = ""
		m.deleteWorktreeForce = false
		if err != nil {
			return m, statusCmd(fmt.Sprintf("worktree remove: %v", err), true)
		}
		// Refresh cache so the deleted worktree vanishes immediately.
		delete(m.worktreeCache, repo)
		m.lookupWorktrees(repo)
		return m, statusCmd(fmt.Sprintf("worktree '%s' removed", wt.Branch), false)
	}
	return m, nil
}

// selectedSession returns the session at the cursor, or nil.
func (m model) selectedSession(items []displayItem) *claude.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(items) {
		return nil
	}
	item := items[m.cursor]
	if item.isHeader || item.isWorktreeRow || item.isEmptyWorktree {
		return nil
	}
	return &item.session
}

func statusCmd(text string, isError bool) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{text: text, isError: isError}
	}
}

func truncWindowName(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60])
	}
	return s
}

func (m *model) skipHeaders(items []displayItem, dir int) {
	for m.cursor >= 0 && m.cursor < len(items) && items[m.cursor].isHeader {
		m.cursor += dir
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
}

// previewWidth returns the width of the preview panel (0 if hidden or too narrow).
func (m model) previewWidth() int {
	if !m.showPreview || m.width < 100 {
		return 0
	}
	pw := m.width / 3
	if pw > 50 {
		pw = 50
	}
	if pw < 30 {
		pw = 30
	}
	return pw
}

// tableWidth returns the width available for the table.
func (m model) tableWidth() int {
	pw := m.previewWidth()
	if pw == 0 {
		return m.width
	}
	return m.width - pw - 1 // 1 for gap
}

func altView(s string) tea.View {
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (m model) View() tea.View {
	if m.width == 0 {
		return altView("Loading...")
	}
	if m.demoSessionScreen {
		return altView(m.demoSessionView())
	}
	if m.usageScreen {
		return altView(m.usageView())
	}
	if m.configScreen {
		return altView(m.configView())
	}
	if m.err != nil {
		return altView(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
	}

	var b strings.Builder

	// Title
	count := len(m.filtered())
	total := len(m.sessions)
	titleText := fmt.Sprintf(" c9s -- %d sessions", total)
	if count != total {
		titleText = fmt.Sprintf(" c9s -- %d/%d sessions", count, total)
	}
	if m.insideTmux && len(m.managedWindows) > 0 {
		titleText += fmt.Sprintf(" · %d windows", len(m.managedWindows))
	}
	b.WriteString(titleStyle.Render(titleText))
	b.WriteString("\n")
	if !m.hooksInstalled && len(m.managedWindows) > 0 {
		b.WriteString(dimStyle.Render(" live status disabled -- run `c9s install` to enable hooks"))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	items := m.items()

	if len(items) == 0 {
		if m.filter != "" {
			b.WriteString(" No sessions matching \"" + m.filter + "\"\n")
		} else {
			b.WriteString(" No Claude Code sessions found.\n")
		}
		b.WriteString("\n")
		b.WriteString(m.footer())
		return altView(b.String())
	}

	// Build table lines.
	tw := m.tableWidth()
	th := m.tableHeight()
	var tableLines []string

	// Header
	tableLines = append(tableLines, headerStyle.Render(m.renderHeader()))

	end := m.scroll + th
	if end > len(items) {
		end = len(items)
	}

	rowNum := 0
	for i := 0; i < m.scroll; i++ {
		if !items[i].isHeader {
			rowNum++
		}
	}

	for i := m.scroll; i < end; i++ {
		item := items[i]
		if item.isHeader {
			line := groupStyle.Render("── " + item.header + " ")
			padW := tw - lipgloss.Width(line)
			if padW > 0 {
				line += groupStyle.Render(strings.Repeat("─", padW))
			}
			tableLines = append(tableLines, line)
			continue
		}
		if item.isWorktreeRow {
			wt := item.worktree
			glyph := "▾"
			if m.collapsedWorktrees[wt.Path] {
				glyph = "▸"
			}
			branch := wt.Branch
			if branch == "" {
				branch = "(unnamed)"
			}
			label := "  " + glyph + " " + branch
			if wt.IsMain {
				label += " (main)"
			}
			// Status glyphs: ⚠ for dirty (tracked changes), ↑N ahead of
			// upstream, ↓N behind. Quiet when everything is clean & synced.
			if wt.Dirty {
				label += " ⚠"
			}
			if wt.Ahead > 0 {
				label += fmt.Sprintf(" ↑%d", wt.Ahead)
			}
			if wt.Behind > 0 {
				label += fmt.Sprintf(" ↓%d", wt.Behind)
			}
			row := m.renderColumns("", label, "", "", wt.Path, "", "", "", true)
			if i == m.cursor {
				row = selectedStyle.Width(tw).Render(row)
			}
			tableLines = append(tableLines, row)
			continue
		}
		if item.isEmptyWorktree {
			label := "      -- no sessions -- press Enter to start one here"
			row := m.renderColumns("", label, "", "", item.worktree.Path, "", "", "", true)
			if i == m.cursor {
				row = selectedStyle.Width(tw).Render(row)
			}
			tableLines = append(tableLines, row)
			continue
		}

		rowNum++
		row := m.renderRow(rowNum, item.session, item.indent)
		if i == m.cursor {
			row = selectedStyle.Width(tw).Render(row)
		}
		tableLines = append(tableLines, row)
	}

	// Fill empty space.
	for i := end - m.scroll; i < th; i++ {
		tableLines = append(tableLines, "")
	}

	// Render with or without preview.
	pw := m.previewWidth()
	if pw > 0 {
		previewLines := m.renderPreview(pw, th+1) // +1 for header
		// Join table and preview side by side.
		for i := 0; i < len(tableLines); i++ {
			line := tableLines[i]
			// Pad table line to tableWidth.
			lineW := lipgloss.Width(line)
			if lineW < tw {
				line += strings.Repeat(" ", tw-lineW)
			}
			// Add preview line.
			pline := ""
			if i < len(previewLines) {
				pline = previewLines[i]
			}
			b.WriteString(line + " " + pline + "\n")
		}
	} else {
		for _, line := range tableLines {
			b.WriteString(line + "\n")
		}
	}

	// Footer
	b.WriteString(m.footer())

	// Pad to full terminal height to prevent old content showing through.
	out := b.String()
	lines := strings.Count(out, "\n")
	for lines < m.height-1 {
		out += "\n"
		lines++
	}

	// Overlay project picker as a floating box on top of the dashboard.
	if m.pickingProject {
		out = m.overlayProjectPicker(out)
	}

	return altView(out)
}

// renderPreview renders the right-side preview panel, dispatching to a
// context-aware body based on the row under the cursor: session rows show
// session details, worktree rows show branch info + resident sessions,
// project headers show the project overview, and header/empty rows for
// worktrees explain what Enter will do there. Keeping all four paths inside
// this one function lets them share the border/padding math.
func (m model) renderPreview(width, height int) []string {
	items := m.items()
	innerW := width - 4

	var content []string
	if m.cursor >= 0 && m.cursor < len(items) {
		it := items[m.cursor]
		switch {
		case it.isEmptyWorktree:
			content = m.previewEmptyWorktree(it, items, innerW)
		case it.isWorktreeRow:
			content = m.previewWorktree(it, items, innerW)
		case it.isHeader:
			content = m.previewHeader(it, items, innerW)
		default:
			content = m.previewSession(&it.session, innerW)
		}
	}
	if len(content) == 0 {
		content = []string{previewDim.Render(" Nothing selected")}
	}

	maxLines := height - 2
	if len(content) > maxLines {
		content = content[:maxLines]
	}
	for len(content) < maxLines {
		content = append(content, "")
	}
	inner := strings.Join(content, "\n")
	bordered := previewBorder.Width(width - 2).Render(inner)
	return strings.Split(bordered, "\n")
}

// previewSession renders the classic per-session detail view.
func (m model) previewSession(s *claude.SessionInfo, innerW int) []string {
	var content []string
	addField := func(label, value string) {
		if value == "" {
			return
		}
		content = append(content, previewDim.Render(label+": ")+previewVal.Render(value))
	}
	addWrap := func(label, value string) {
		if value == "" {
			return
		}
		content = append(content, previewDim.Render(label+":"))
		for _, wl := range wordWrap(value, innerW) {
			content = append(content, previewVal.Render(wl))
		}
	}

	content = append(content, previewLabel.Render(trunc(s.DisplayName(), innerW)))
	content = append(content, "")

	addField("Status", m.effectiveStatus(*s))
	addField("Project", s.ProjectPath)
	if s.GitBranch != "" {
		addField("Branch", s.GitBranch)
	}
	addField("Messages", fmt.Sprintf("%d", s.MessageCount))
	if s.TotalTokens() > 0 {
		addField("Tokens", fmtTokens(s.TotalTokens()))
		addField("  Input", fmtTokens(s.InputTokens))
		addField("  Output", fmtTokens(s.OutputTokens))
		if s.CacheRead > 0 {
			addField("  Cache read", fmtTokens(s.CacheRead))
		}
		if s.CacheCreate > 0 {
			addField("  Cache write", fmtTokens(s.CacheCreate))
		}
	}
	addField("Created", fmtTime(s.Created))
	addField("Modified", fmtTime(s.Modified))
	addField("Session ID", s.SessionID)
	content = append(content, "")

	if s.FirstPrompt != "" {
		addWrap("First prompt", s.FirstPrompt)
	}
	if s.Summary != "" && s.Summary != s.DisplayName() {
		content = append(content, "")
		addWrap("Summary", s.Summary)
	}
	return content
}

// previewWorktree renders branch/state info for a worktree row plus a short
// roster of sessions currently living in that worktree (whatever items()
// bucketed under it).
func (m model) previewWorktree(it displayItem, items []displayItem, innerW int) []string {
	wt := it.worktree
	var content []string
	title := wt.Branch
	if wt.IsMain {
		title += "  (main)"
	}
	content = append(content, previewLabel.Render(trunc(title, innerW)))
	content = append(content, "")

	addField := func(label, value string) {
		if value == "" {
			return
		}
		content = append(content, previewDim.Render(label+": ")+previewVal.Render(value))
	}
	addField("Path", wt.Path)
	statusBits := []string{}
	if wt.Dirty {
		statusBits = append(statusBits, "dirty")
	}
	if wt.Ahead > 0 {
		statusBits = append(statusBits, fmt.Sprintf("↑%d", wt.Ahead))
	}
	if wt.Behind > 0 {
		statusBits = append(statusBits, fmt.Sprintf("↓%d", wt.Behind))
	}
	if len(statusBits) == 0 {
		statusBits = []string{"clean"}
	}
	addField("Status", strings.Join(statusBits, " · "))

	// Collect sessions bucketed under this worktree in the current items list.
	var mine []claude.SessionInfo
	for _, row := range items {
		if row.parentWorktree == wt.Path && !row.isHeader && !row.isWorktreeRow && !row.isEmptyWorktree {
			mine = append(mine, row.session)
		}
	}
	content = append(content, "")
	content = append(content, previewDim.Render(fmt.Sprintf("Sessions (%d):", len(mine))))
	if len(mine) == 0 {
		content = append(content, previewDim.Render("  (none -- press Enter to start one)"))
	} else {
		for _, s := range mine {
			content = append(content, previewVal.Render("  · "+trunc(s.DisplayName(), innerW-4)))
		}
	}
	content = append(content, "")
	content = append(content, previewDim.Render("Enter: new session here"))
	if !wt.IsMain {
		content = append(content, previewDim.Render("d:     delete worktree"))
	}
	content = append(content, previewDim.Render("a:     add another worktree"))
	return content
}

// previewEmptyWorktree is the tighter form used when the cursor lands on the
// placeholder row of a worktree that has no sessions yet -- it's basically the
// worktree preview with a nudge to press Enter.
func (m model) previewEmptyWorktree(it displayItem, items []displayItem, innerW int) []string {
	return m.previewWorktree(it, items, innerW)
}

// previewHeader shows an overview of whichever group the cursor is on. For
// project groups this includes the aggregated worktree state; for status
// groups it just counts.
func (m model) previewHeader(it displayItem, items []displayItem, innerW int) []string {
	var content []string
	content = append(content, previewLabel.Render(trunc(it.header, innerW)))
	content = append(content, "")

	if it.parentProject != "" {
		wts := m.lookupWorktrees(it.parentProject)
		dirty, ahead, behind := 0, 0, 0
		for _, wt := range wts {
			if wt.Dirty {
				dirty++
			}
			ahead += wt.Ahead
			behind += wt.Behind
		}
		content = append(content, previewDim.Render("Repo:     ")+previewVal.Render(it.parentProject))
		if len(wts) > 0 {
			bits := []string{fmt.Sprintf("%d worktree%s", len(wts), pluralS(len(wts)))}
			if dirty > 0 {
				bits = append(bits, fmt.Sprintf("%d dirty", dirty))
			}
			if ahead > 0 {
				bits = append(bits, fmt.Sprintf("↑%d", ahead))
			}
			if behind > 0 {
				bits = append(bits, fmt.Sprintf("↓%d", behind))
			}
			content = append(content, previewDim.Render("Worktrees: ")+previewVal.Render(strings.Join(bits, " · ")))
		}
	}

	content = append(content, "")
	content = append(content, previewDim.Render("w: toggle all worktrees"))
	content = append(content, previewDim.Render("a: add worktree"))
	return content
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// wordWrap wraps text to the given width.
func wordWrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	words := strings.Fields(s)
	current := ""
	for _, w := range words {
		if current == "" {
			current = w
		} else if len(current)+1+len(w) <= width {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func (m model) footer() string {
	if m.pickingProject {
		f := ""
		if m.projectFilter != "" {
			f = " /  " + m.projectFilter
		}
		return helpStyle.Render(" " +
			helpKeyStyle.Render("↑/↓") + " navigate  " +
			helpKeyStyle.Render("enter") + " select  " +
			"type to filter  " +
			helpKeyStyle.Render("esc") + " cancel" + f)
	}
	if m.pickingEffort {
		return helpStyle.Render(" effort: " +
			helpKeyStyle.Render("1") + " low  " +
			helpKeyStyle.Render("2") + " medium  " +
			helpKeyStyle.Render("3") + " high  " +
			helpKeyStyle.Render("4") + " max  " +
			helpKeyStyle.Render("esc") + " cancel")
	}
	if m.renaming || m.addingWorktree {
		return helpStyle.Render(m.renameInput.View())
	}
	if m.deletingWorktree {
		wt := m.deleteWorktree
		warn := ""
		if wt.Dirty {
			warn = errStyle.Render(" [dirty]")
		}
		prompt := fmt.Sprintf("delete worktree '%s' at %s%s? ", wt.Branch, wt.Path, warn)
		if m.deleteWorktreeForce {
			prompt = fmt.Sprintf("worktree '%s' has uncommitted changes -- force remove? ", wt.Branch)
		}
		return helpStyle.Render(prompt +
			helpKeyStyle.Render("y") + " yes  " +
			helpKeyStyle.Render("n") + " no")
	}
	if m.searching {
		return helpStyle.Render(m.searchInput.View())
	}

	// Status message takes priority.
	if m.statusText != "" {
		style := infoStyle
		if m.statusIsError {
			style = errStyle
		}
		return style.Render(" " + m.statusText)
	}

	var groupLabel string
	switch m.groupBy {
	case groupNone:
		groupLabel = "group:off"
	case groupProject:
		groupLabel = "group:project"
	case groupStatus:
		groupLabel = "group:status"
	}
	tokenLabel := "show tokens"
	if m.showTokens {
		tokenLabel = "hide tokens"
	}
	previewLabel := "show preview"
	if m.showPreview {
		previewLabel = "hide preview"
	}

	parts := []string{
		helpKeyStyle.Render("j/k") + " nav",
	}
	if m.insideTmux {
		parts = append(parts,
			helpKeyStyle.Render("enter") + " open",
			helpKeyStyle.Render("n/N") + " new",
			helpKeyStyle.Render("x") + " close",
		)
	}
	parts = append(parts,
		helpKeyStyle.Render("R") + " rename",
		helpKeyStyle.Render("b") + " backup",
		helpKeyStyle.Render("/") + " search",
		helpKeyStyle.Render("tab") + " " + groupLabel,
		helpKeyStyle.Render("p") + " " + previewLabel,
		helpKeyStyle.Render("t") + " " + tokenLabel,
	)
	if m.worktreesActive() {
		parts = append(parts,
			helpKeyStyle.Render("w")+" toggle wt",
			helpKeyStyle.Render("a")+" add wt",
			helpKeyStyle.Render("d")+" del wt",
		)
	}
	parts = append(parts,
		helpKeyStyle.Render("u") + " usage",
		helpKeyStyle.Render("c") + " config",
		helpKeyStyle.Render("q") + " quit",
	)
	if m.filter != "" {
		parts = append([]string{helpKeyStyle.Render("esc") + " clear"}, parts...)
	}

	return helpStyle.Render(" " + strings.Join(parts, "  "))
}

// effectiveStatus returns the status string to display for a session. When
// the session has a managed tmux window AND the hook-fed pane status is a
// specific value (processing/waiting/done), that wins -- the pane status is
// live and more accurate than the file-mtime lifecycle status. When the
// pane status is Unknown (hooks not installed, no hook fired yet, or the
// state file is missing/corrupt), we fall back to the lifecycle status
// because "unknown" alone leaves the user guessing.
func (m model) effectiveStatus(s claude.SessionInfo) string {
	if mw, ok := m.managedWindows[s.SessionID]; ok && mw.paneStatus != tmux.PaneUnknown {
		return mw.paneStatus.String()
	}
	return s.Status.String()
}

// filtered returns sessions matching the current search filter and any
// visibility toggles (hide-archived, replaced-by-fork).
func (m model) filtered() []claude.SessionInfo {
	var out []claude.SessionInfo
	f := strings.ToLower(m.filter)
	hideArchived := cfg.HideArchived == "on"
	for _, s := range m.sessions {
		if m.replacedSessions[s.SessionID] {
			continue
		}
		if hideArchived && s.Status == claude.StatusArchived {
			continue
		}
		if m.filter == "" {
			out = append(out, s)
			continue
		}
		if strings.Contains(strings.ToLower(s.DisplayName()), f) ||
			strings.Contains(strings.ToLower(s.ProjectPath), f) ||
			strings.Contains(strings.ToLower(s.SessionID), f) ||
			strings.Contains(strings.ToLower(s.GitBranch), f) {
			out = append(out, s)
		}
	}
	return out
}

// items returns the ordered list of display rows the dashboard renders. Three
// shapes come out of here depending on the grouping mode and whether the
// worktree feature has anything to show:
//
//  1. groupNone   → flat session list (no headers, no worktree layer).
//  2. groupStatus → status-bucketed session list with one header per bucket.
//  3. groupProject → hierarchical when the project has ≥2 worktrees and
//     worktrees=on: header → worktree rows → session rows nested under the
//     matching worktree. Falls back to flat sessions under the header when
//     only one worktree exists.
func (m model) items() []displayItem {
	sessions := m.filtered()

	// Flat: no grouping, no headers, no hierarchy.
	if m.groupBy == groupNone {
		out := make([]displayItem, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, displayItem{session: s})
		}
		return out
	}

	type group struct {
		name    string
		items   []claude.SessionInfo
		newest  time.Time
		order   int    // fixed order for status mode
		project string // canonical repo path (groupProject only)
	}
	groups := make(map[string]*group)
	var order []string

	for _, s := range sessions {
		var name string
		var sortOrder int
		var proj string
		switch m.groupBy {
		case groupProject:
			name = projectName(s.ProjectPath)
			if name == "" {
				name = "(no project)"
			}
			proj = s.ProjectPath
		case groupStatus:
			name = m.effectiveStatus(s)
			switch name {
			case "processing":
				sortOrder = 0
			case "waiting":
				sortOrder = 1
			case "background":
				sortOrder = 2
			case "active":
				sortOrder = 3
			case "idle":
				sortOrder = 4
			case "done":
				sortOrder = 5
			case "resumable":
				sortOrder = 6
			case "archived":
				sortOrder = 7
			}
		}

		g, ok := groups[name]
		if !ok {
			g = &group{name: name, order: sortOrder, project: proj}
			groups[name] = g
			order = append(order, name)
		} else if g.project == "" {
			g.project = proj
		}
		g.items = append(g.items, s)
		if s.Modified.After(g.newest) {
			g.newest = s.Modified
		}
	}

	if m.groupBy == groupStatus {
		sort.Slice(order, func(i, j int) bool {
			return groups[order[i]].order < groups[order[j]].order
		})
	} else {
		sort.Slice(order, func(i, j int) bool {
			return groups[order[i]].newest.After(groups[order[j]].newest)
		})
	}

	var result []displayItem
	for _, name := range order {
		g := groups[name]

		// Hierarchical branch: project + worktrees on + repo has ≥2 worktrees.
		if m.groupBy == groupProject && cfg.Worktrees == "on" && g.project != "" {
			wts := m.lookupWorktrees(g.project)
			if len(wts) >= 2 {
				result = append(result, m.hierarchicalGroup(g.name, g.project, g.items, wts)...)
				continue
			}
		}

		// Flat inside the group.
		result = append(result, displayItem{
			isHeader:      true,
			header:        fmt.Sprintf("%s (%d)", g.name, len(g.items)),
			indent:        0,
			parentProject: g.project,
		})
		for _, s := range g.items {
			result = append(result, displayItem{
				session:       s,
				indent:        1,
				parentProject: s.ProjectPath,
			})
		}
	}
	return result
}

// hierarchicalGroup builds the header + worktree-rows + nested session rows for
// one project group. Sessions bucket to a worktree by exact ProjectPath match;
// anything unmatched falls back to the main worktree so no session goes missing
// even if it was started outside git's worktree list.
func (m model) hierarchicalGroup(name, project string, sessions []claude.SessionInfo, wts []git.Worktree) []displayItem {
	var out []displayItem

	header := fmt.Sprintf("%s (%d %s · %d %s)",
		name,
		len(sessions), plural(len(sessions), "session", "sessions"),
		len(wts), plural(len(wts), "worktree", "worktrees"),
	)
	out = append(out, displayItem{
		isHeader:      true,
		header:        header,
		indent:        0,
		parentProject: project,
	})

	// Bucket sessions by worktree Path. Unmatched → main worktree.
	byWT := make(map[string][]claude.SessionInfo)
	mainPath := ""
	for _, wt := range wts {
		if wt.IsMain {
			mainPath = wt.Path
		}
	}
	if mainPath == "" && len(wts) > 0 {
		mainPath = wts[0].Path
	}
	for _, s := range sessions {
		// A session running inside a worktree records its ProjectPath as the
		// repo root (that's what Claude writes to history.jsonl), but its
		// JSONL is stored under the worktree's encoded path -- we surface
		// that as Cwd during discovery. Prefer Cwd for worktree bucketing
		// so worktree-scoped sessions nest under the right row, falling
		// back to ProjectPath when Cwd wasn't determined.
		candidates := []string{s.Cwd, s.ProjectPath}
		hit := ""
		for _, c := range candidates {
			if c == "" {
				continue
			}
			for _, wt := range wts {
				if c == wt.Path {
					hit = wt.Path
					break
				}
			}
			if hit != "" {
				break
			}
		}
		if hit == "" {
			hit = mainPath
		}
		byWT[hit] = append(byWT[hit], s)
	}

	for _, wt := range wts {
		out = append(out, displayItem{
			isWorktreeRow: true,
			worktree:      wt,
			indent:        1,
			parentProject: project,
		})
		if m.collapsedWorktrees[wt.Path] {
			continue
		}
		wtSessions := byWT[wt.Path]
		if len(wtSessions) == 0 {
			out = append(out, displayItem{
				isEmptyWorktree: true,
				worktree:        wt,
				indent:          2,
				parentProject:   project,
				parentWorktree:  wt.Path,
			})
			continue
		}
		for _, s := range wtSessions {
			out = append(out, displayItem{
				session:        s,
				indent:         2,
				parentProject:  project,
				parentWorktree: wt.Path,
			})
		}
	}
	return out
}

// plural returns singular when n==1, plural otherwise. Small helper to keep
// header strings honest without regex-y hacks.
func plural(n int, singular, plur string) string {
	if n == 1 {
		return singular
	}
	return plur
}

// worktreeCacheEntry stores the list of worktrees for one repo along with a
// fingerprint of the on-disk state that produced it. The fingerprint is the
// modtime of .git/worktrees/ and .git/index -- both files git touches when
// worktrees come and go or the working tree's HEAD moves. We use this to
// invalidate the cache when the user runs `git worktree add/remove` in another
// terminal without forcing a full git call every tick.
type worktreeCacheEntry struct {
	worktrees   []git.Worktree
	fingerprint string
}

// worktreeFingerprint returns a short string identifying the current state of
// the worktree-relevant files for a repo. Empty when none exist (still a valid
// fingerprint -- a cache hit on "" means "we already looked and there was
// nothing", so don't shell out again until something changes).
func worktreeFingerprint(repoDir string) string {
	var b strings.Builder
	for _, sub := range []string{".git/worktrees", ".git/index", ".git/HEAD"} {
		if fi, err := os.Stat(filepath.Join(repoDir, sub)); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d|", sub, fi.ModTime().UnixNano(), fi.Size())
		}
	}
	return b.String()
}

// lookupWorktrees returns the cached worktree list for dir, refreshing it
// when the on-disk fingerprint has shifted since the last cache write. Maps
// are reference types, so writes to m.worktreeCache survive across the
// value-receiver copy.
func (m model) lookupWorktrees(dir string) []git.Worktree {
	if m.demoMode {
		return claude.DemoWorktrees[dir]
	}
	if dir == "" {
		return nil
	}
	fp := worktreeFingerprint(dir)
	if e, ok := m.worktreeCache[dir]; ok && e.fingerprint == fp {
		return e.worktrees
	}
	wts := git.ListWorktrees(dir)
	m.worktreeCache[dir] = worktreeCacheEntry{worktrees: wts, fingerprint: fp}
	return wts
}

// mainWorktreeDir returns the main worktree path for the repo containing dir,
// or "" if dir is not inside a git repo. Preferred over raw ProjectPath when
// starting a new session, so `n` lands in main even if the selected row lives
// in a linked worktree.
func (m model) mainWorktreeDir(dir string) string {
	for _, wt := range m.lookupWorktrees(dir) {
		if wt.IsMain {
			return wt.Path
		}
	}
	return ""
}

func (m model) tableHeight() int {
	h := m.height - 5
	if h < 1 {
		return 1
	}
	return h
}

func (m *model) adjustScroll() {
	th := m.tableHeight()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+th {
		m.scroll = m.cursor - th + 1
	}
}

func (m model) renderHeader() string {
	return m.renderColumns("#", "NAME", "STATUS", "BRANCH", "PROJECT", "MSGS", "TOKENS", "MODIFIED", false)
}

func (m model) renderRow(num int, s claude.SessionInfo, indent int) string {
	tokStr := ""
	if s.TotalTokens() > 0 {
		tokStr = fmtTokens(s.TotalTokens())
	}
	status := m.effectiveStatus(s)
	dim := s.Status == claude.StatusArchived && status == "archived"

	// Indent nested rows (session under worktree gets extra padding on the
	// name column so the hierarchy reads visually).
	name := s.DisplayName()
	if indent > 0 {
		name = strings.Repeat("  ", indent) + name
	}

	// In hierarchical project view (indent=2), the branch and project columns
	// are redundant -- the enclosing worktree row shows the branch and the
	// project header shows the project. Blank them out; renderColumns will
	// still allocate zero width for hidden columns because that decision is
	// made globally per render pass in branchColWidth().
	branch := s.GitBranch
	proj := projectName(s.ProjectPath)
	if indent >= 2 {
		branch = ""
		proj = ""
	}

	return m.renderColumns(
		fmt.Sprintf("%d", num),
		name,
		status,
		branch,
		proj,
		fmt.Sprintf("%d", s.MessageCount),
		tokStr,
		fmtModified(s.Modified),
		dim,
	)
}

// branchColWidth returns the width of the BRANCH column, or 0 to hide it.
//
// The column disappears entirely in the hierarchical project view -- the
// worktree rows carry branch info there and a dedicated column would be pure
// redundancy. It also disappears when the worktree feature is off. Otherwise
// (flat / status grouping) it sizes to the widest branch name currently
// visible, clamped to [8, 20] so it never starves NAME or wastes space.
func (m model) branchColWidth() int {
	if cfg.Worktrees != "on" || m.groupBy == groupProject {
		return 0
	}
	max := 0
	for _, s := range m.filtered() {
		if n := len(s.GitBranch); n > max {
			max = n
		}
	}
	if max == 0 {
		return 0
	}
	if max < 8 {
		max = 8
	}
	if max > 20 {
		max = 20
	}
	return max
}

func (m model) renderColumns(num, name, status, branch, project, msgs, tokens, modified string, dim bool) string {
	tw := m.tableWidth()
	modW := 11
	msgsW := 5
	statW := 10
	tokW := 0
	if m.showTokens {
		tokW = 8
	}
	branchW := m.branchColWidth()
	projW := 0
	// Project column is redundant inside project-grouped view -- the header
	// already carries the project name.
	if tw >= 90 && m.groupBy != groupProject {
		projW = 18
	}
	numW := 3

	nameW := tw - numW - statW - msgsW - modW - 6
	if projW > 0 {
		nameW -= projW + 1
	}
	if tokW > 0 {
		nameW -= tokW + 1
	}
	if branchW > 0 {
		nameW -= branchW + 1
	}
	if nameW < 10 {
		nameW = 10
	}

	// Format status with color.
	statusCell := fmt.Sprintf("%-*s", statW, trunc(status, statW))
	if !dim {
		switch status {
		case "active":
			statusCell = stActive.Render(statusCell)
		case "idle":
			statusCell = stIdle.Render(statusCell)
		case "resumable":
			statusCell = stResumable.Render(statusCell)
		case "archived":
			statusCell = stArchived.Render(statusCell)
		case "background":
			statusCell = stBackground.Render(statusCell)
		case "waiting":
			statusCell = stWaiting.Render(statusCell)
		case "processing":
			statusCell = stProcessing.Render(statusCell)
		case "done":
			statusCell = stDone.Render(statusCell)
		case "unknown":
			statusCell = stUnknown.Render(statusCell)
		}
	}

	var parts []string
	parts = append(parts, fmt.Sprintf(" %-*s", numW, trunc(num, numW)))
	parts = append(parts, fmt.Sprintf("%-*s", nameW, trunc(name, nameW)))
	parts = append(parts, statusCell)
	if branchW > 0 {
		parts = append(parts, fmt.Sprintf("%-*s", branchW, trunc(branch, branchW)))
	}
	if projW > 0 {
		parts = append(parts, fmt.Sprintf("%-*s", projW, trunc(project, projW)))
	}
	parts = append(parts, fmt.Sprintf("%*s", msgsW, trunc(msgs, msgsW)))
	if tokW > 0 {
		parts = append(parts, fmt.Sprintf("%*s", tokW, trunc(tokens, tokW)))
	}
	parts = append(parts, fmt.Sprintf(" %-*s", modW, trunc(modified, modW)))

	row := strings.Join(parts, " ")
	if dim {
		row = dimStyle.Render(row)
	}
	return row
}

func trunc(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "~"
}

func projectName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtModified(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours()) / 24
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// usageHas returns true if the status_usage config includes the given component.
// Supports "all" as shorthand for all components, and comma-separated values.
func usageHas(component string) bool {
	if cfg.StatusUsage == "all" {
		return true
	}
	for _, s := range strings.Split(cfg.StatusUsage, ",") {
		if strings.TrimSpace(s) == component {
			return true
		}
	}
	return false
}

// formatDashboardUsage builds the global usage string for the dashboard status bar.
// Shows API-level usage (percent, reset time) and aggregate cost across all sessions.
func formatDashboardUsage(sessions []claude.SessionInfo) string {
	if cfg.StatusUsage == "off" {
		return ""
	}

	var apiPercent string
	var resetStr string
	if usageHas("percent") {
		if result, err := claude.FetchUsage(); err == nil {
			pct := fmt.Sprintf("%.0f%%", result.Usage.FiveHour.Utilization)
			if result.Stale {
				pct += "?"
			}
			apiPercent = pct
			resetStr = fmtResetTime(result.Usage.FiveHour.ResetsAt)
		}
	}

	var parts []string
	if usageHas("tokens") {
		var total int
		for _, s := range sessions {
			total += s.InputTokens + s.OutputTokens
		}
		if total > 0 {
			parts = append(parts, fmtTokens(total))
		}
	}
	if apiPercent != "" {
		if resetStr != "" {
			apiPercent += " " + resetStr
		}
		parts = append(parts, apiPercent)
	}
	return strings.Join(parts, " · ")
}

// formatUsage builds the usage string for the tmux status bar based on config.
// paneStatusFromState reads the per-session state file written by Claude
// Code hooks and maps it to a tmux.PaneStatus. Returns PaneUnknown when the
// file is missing -- typically because hooks aren't installed yet or the
// session started before the install.
func paneStatusFromState(sessionID string) tmux.PaneStatus {
	info, err := sessionstate.Read(sessionID)
	return paneStatusFromInfo(info, err)
}

// paneStatusFromInfo maps a sessionstate Info value (and the error from
// reading it) into a tmux.PaneStatus. Saves the caller a second Read when
// the Info has already been loaded for other purposes.
func paneStatusFromInfo(info sessionstate.Info, err error) tmux.PaneStatus {
	if err != nil {
		return tmux.PaneUnknown
	}
	switch info.State {
	case sessionstate.StateProcessing:
		return tmux.PaneProcessing
	case sessionstate.StateWaiting:
		return tmux.PaneWaiting
	case sessionstate.StateDone:
		return tmux.PaneDone
	}
	return tmux.PaneUnknown
}


func formatUsage(s claude.SessionInfo) string {
	return formatUsageInternal(s.InputTokens, s.OutputTokens, claude.GetLastModel(s.SessionID), s)
}

// formatUsageFromState renders the per-window usage string using hook-pushed
// token counts and model from the sessionstate. Falls back to JSONL-derived
// values when the state file is missing pieces (e.g. an old session whose
// hooks ran on a prior c9s install).
func formatUsageFromState(s claude.SessionInfo, info sessionstate.Info) string {
	inTok, outTok := info.InputTokens, info.OutputTokens
	if inTok == 0 && outTok == 0 {
		inTok, outTok = s.InputTokens, s.OutputTokens
	}
	model := info.Model
	if model == "" {
		model = claude.GetLastModel(s.SessionID)
	}
	return formatUsageInternal(inTok, outTok, model, s)
}

// formatUsageInternal is the shared formatter used by per-window and dashboard
// status-bar renderers.
func formatUsageInternal(inTok, outTok int, model string, _ claude.SessionInfo) string {
	if cfg.StatusUsage == "off" {
		return ""
	}
	budgetTokens := inTok + outTok
	if budgetTokens == 0 {
		return ""
	}

	// Get real usage percentage from Anthropic API (cached 5min).
	var apiPercent string
	var resetStr string
	if usageHas("percent") {
		if result, err := claude.FetchUsage(); err == nil {
			pct := fmt.Sprintf("%.0f%%", result.Usage.FiveHour.Utilization)
			if result.Stale {
				pct += "?"
			}
			apiPercent = pct
			resetStr = fmtResetTime(result.Usage.FiveHour.ResetsAt)
		}
	}

	var parts []string
	if usageHas("tokens") {
		parts = append(parts, fmtTokens(budgetTokens))
	}
	if apiPercent != "" {
		if resetStr != "" {
			apiPercent += " " + resetStr
		}
		parts = append(parts, apiPercent)
	}
	if cfg.StatusModel == "on" && model != "" {
		parts = append(parts, shortModelName(model))
	}
	return strings.Join(parts, " · ")
}

// fmtResetTime parses an RFC3339 reset time and returns a human-friendly duration.
func fmtResetTime(resetsAt string) string {
	if resetsAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return ""
	}
	d := time.Until(t)
	if d <= 0 {
		return ""
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("resets %dh%02dm", h, m)
	}
	return fmt.Sprintf("resets %dm", m)
}


func navKeys() tmux.NavKeys {
	return tmux.NavKeys{
		Dashboard:   cfg.Keys.Dashboard,
		NextSession: cfg.Keys.NextSession,
		PrevSession: cfg.Keys.PrevSession,
	}
}

// sessionsChanged returns true if the session list has meaningfully changed.
// Compares count, IDs, statuses, token counts, and mtimes to avoid unnecessary re-renders.
func sessionsChanged(old, new []claude.SessionInfo) bool {
	if len(old) != len(new) {
		return true
	}
	for i := range old {
		a, b := old[i], new[i]
		if a.SessionID != b.SessionID || a.Status != b.Status ||
			a.InputTokens != b.InputTokens || a.OutputTokens != b.OutputTokens ||
			a.CustomTitle != b.CustomTitle || !a.FileMtime.Equal(b.FileMtime) {
			return true
		}
	}
	return false
}

func refreshInterval() time.Duration {
	s := cfg.RefreshSeconds
	if s < 1 {
		s = 1
	}
	if s > 10 {
		s = 10
	}
	return time.Duration(s) * time.Second
}

func statusColors() tmux.StatusColors {
	c := cfg.EffectiveColors()
	return tmux.StatusColors{
		Bg:     c.StatusBg,
		Fg:     c.StatusFg,
		Accent: c.StatusAccent,
		Dim:    c.StatusDim,
	}
}

func main() {
	// Handle non-TUI subcommands before anything else so they don't load the
	// TUI, don't bootstrap tmux, and don't depend on a terminal being
	// attached. The `_hook` handler is invoked by Claude Code with JSON on
	// stdin and must exit quickly.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "install":
			os.Exit(runInstall())
		case "uninstall":
			os.Exit(runUninstall())
		case "_hook":
			event := ""
			if len(os.Args) >= 3 {
				event = os.Args[2]
			}
			os.Exit(runHook(event))
		case "_cycle":
			direction := ""
			if len(os.Args) >= 3 {
				direction = os.Args[2]
			}
			os.Exit(runCycle(direction))
		}
	}

	// Handle --version early before loading config.
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println("c9s " + version)
			return
		}
	}

	cfg = config.Load()
	applyColors(cfg.EffectiveColors())

	insideTmux := false
	demoMode := false
	debugMode := false
	forceNewSession := false
	args := os.Args[1:]

	// Parse flags from args (remove internal flags, keep user-facing ones for forwarding).
	var filtered []string
	for _, arg := range args {
		switch arg {
		case "--inside-tmux":
			insideTmux = true
		case "--demo":
			demoMode = true
			filtered = append(filtered, arg) // forward through tmux bootstrap
		case "--debug":
			debugMode = true
			filtered = append(filtered, arg) // forward through tmux bootstrap
		case "--new":
			// Force a fresh tmux session even if an idle c9s* one is reusable.
			// Not forwarded -- the child is already inside its chosen session.
			forceNewSession = true
		default:
			filtered = append(filtered, arg)
		}
	}
	args = filtered

	initDebugLog(debugMode)

	// If tmux is available and we're not already inside tmux, bootstrap.
	if !insideTmux && tmux.Available() && !tmux.InSession() {
		selfBin, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		// Pick the tmux session name for this terminal. A terminal whose
		// previous c9s detached (keep_alive) reuses that idle session;
		// otherwise (or with --new), we get the lowest free "c9s-<N>" name.
		sessionName := tmux.PickSessionName(forceNewSession)
		tmux.SetCurrentSession(sessionName)
		if tmux.SessionExists(sessionName) {
			// Session already exists. Re-create dashboard window if it was
			// closed (e.g., after keep_alive detach killed the bubbletea process).
			if !tmux.WindowExists(sessionName + ":" + tmux.DashboardWindow) {
				tmux.CreateDashboardWindow(selfBin, args)
				tmux.ConfigureStatusBar(navKeys(), statusColors(), version, cfg.ScrollSpeed, cfg.RefreshSeconds)
				tmux.SetupNavigationKeys(navKeys())
			}
			if err := tmux.Attach(); err != nil {
				fmt.Fprintf(os.Stderr, "tmux attach: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := tmux.Bootstrap(selfBin, args, navKeys(), statusColors(), version, cfg.ScrollSpeed, cfg.RefreshSeconds); err != nil {
			fmt.Fprintf(os.Stderr, "tmux bootstrap: %v\n", err)
			os.Exit(1)
		}
		return // Bootstrap exec's, so this is only reached if attach was used.
	}

	// Resolve our tmux session name from the live tmux server now that we're
	// inside the child process. Side effect: sets tmux.SessionName so every
	// subsequent tmux call targets the right c9s instance (c9s / c9s-2 / …).
	inC9s := tmux.InC9sSession()
	var sessions []claude.SessionInfo
	var loadErr error
	if demoMode {
		sessions = claude.DemoSessions()
	} else {
		sessions, loadErr = claude.ListAllSessions()
	}
	m := initialModel(sessions, loadErr, insideTmux || inC9s)
	m.demoMode = demoMode
	// On startup, scan existing tmux windows and recover or clean up orphans
	// from previous c9s runs (e.g. after keep_alive detach or a crash).
	if !demoMode {
		// Sessions killed ungracefully (e.g. `q` tearing down the whole tmux
		// server) never fire SessionEnd, so their state file's TmuxPane
		// lingers. If the tmux server was later restarted, pane IDs reset and
		// a stale file can collide with a brand-new pane -- purge anything
		// old enough that it can't belong to the current tmux server.
		if n := sessionstate.PurgeStale(staleStateAge); n > 0 {
			debugLog("startup → purged %d stale session state files", n)
		}
		m.reconcileStartupWindows(sessions)
	}
	if demoMode {
		m.showTokens = false // start clean, toggle on during demo
		cfg.Worktrees = "on"
		// Simulate managed windows with pane statuses for some sessions.
		for _, s := range sessions {
			switch s.DemoPaneStatus {
			case 1:
				m.managedWindows[s.SessionID] = managedWindow{
					windowID: "demo", sessionID: s.SessionID, project: s.ProjectPath,
					paneStatus: tmux.PaneProcessing,
				}
			case 2:
				m.managedWindows[s.SessionID] = managedWindow{
					windowID: "demo", sessionID: s.SessionID, project: s.ProjectPath,
					paneStatus: tmux.PaneWaiting,
				}
			case 3:
				m.managedWindows[s.SessionID] = managedWindow{
					windowID: "demo", sessionID: s.SessionID, project: s.ProjectPath,
					paneStatus: tmux.PaneDone,
				}
			}
		}
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
