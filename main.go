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
	"github.com/stefanoguerrini/c9s/internal/tmux"
)

// version is set at build time via ldflags.
var version = "dev"

// cfg is the global config loaded at startup.
var cfg config.Config

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
	infoStyle     lipgloss.Style
	errStyle      lipgloss.Style
	stWaiting     lipgloss.Style
	stProcessing  lipgloss.Style
	stDone        lipgloss.Style
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
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Info))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Error))
	stWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Waiting)).Bold(true)
	stProcessing = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Processing)).Bold(true)
	stDone = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Done))
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

// displayItem is either a group header, a session row, or a worktree sub-row.
type displayItem struct {
	isHeader      bool
	header        string
	session       claude.SessionInfo
	isWorktreeRow bool         // sub-row showing a worktree
	worktree      git.Worktree // worktree info for sub-row
	isLastWT      bool         // last worktree sub-row (for └─ vs ├─)
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
	insideTmux     bool // running inside tmux as dashboard
	managedWindows map[string]managedWindow // sessionID → window

	// Rename
	renaming      bool
	renameInput   textinput.Model
	renameSession *claude.SessionInfo // session being renamed

	// Effort picker
	pickingEffort bool
	effortWorkDir string // project dir for the new session

	// Worktree display
	showWorktrees     bool                      // global toggle (for "all" mode)
	expandedWorktrees map[int]bool              // per-cursor expanded state (for "selected" mode)
	worktreeCache     map[string][]git.Worktree // project dir → worktrees

	// Config screen
	configScreen  bool
	configDraft   config.Config
	configFields  []config.Field
	configCursor  int
	configScroll  int
	configEditing bool
	configEditIdx int
	configInput   textinput.Model
	configShowDesc bool // show field descriptions

	// Demo mode (--demo flag, fake data for screenshots)
	demoMode bool

	// Status bar message
	statusText    string
	statusIsError bool
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
		expandedWorktrees: make(map[int]bool),
		worktreeCache:     make(map[string][]git.Worktree),
		showTokens:        cfg.Dashboard.ShowTokens,
		showPreview:       cfg.Dashboard.ShowPreview,
		showWorktrees:     cfg.Dashboard.ShowWorktrees,
		groupBy:           groupMode(cfg.Dashboard.GroupBy),
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
			sessions, err := claude.ListAllSessions()
			if err == nil && sessionsChanged(m.sessions, sessions) {
				m.sessions = sessions
			}
			// Reload config if changed on disk (e.g. after editing via 'c').
			if newCfg, changed := config.LoadIfChanged(); changed {
				cfg = newCfg
				applyColors(cfg.EffectiveColors())
			}
			// Keep backups up to date with source files.
			claude.RefreshBackups()
			// Refresh worktree cache (cheap git calls, only when feature enabled).
			if cfg.Worktrees != "off" && git.Available() {
				for dir := range m.worktreeCache {
					m.worktreeCache[dir] = git.ListWorktrees(dir)
				}
			}
		}
		// Reconcile managed windows with actual running sessions.
		// Handles new sessions (tmpKey) and forked sessions (stale sessionID).
		if m.insideTmux && len(m.managedWindows) > 0 {
			procs := claude.ListClaudeProcesses()
			m.reconcileWindows(m.sessions, procs, tmux.GetPanePID, claude.ChildPIDs)
		}

		// Update pane statuses for managed windows.
		for key, mw := range m.managedWindows {
			if !tmux.WindowExists(mw.windowID) {
				delete(m.managedWindows, key)
				continue
			}

			// 1) Check file mtime — if recently written, claude is processing.
			recentlyActive := false
			for _, s := range m.sessions {
				if s.SessionID == mw.sessionID {
					if !s.FileMtime.IsZero() && time.Since(s.FileMtime) < 10*time.Second {
						recentlyActive = true
					}
					// Update usage in tmux status bar.
					if usage := formatUsage(s); usage != "" {
						tmux.SetWindowEnv(mw.windowID, "c9s-usage", usage)
					}
					break
				}
			}

			if recentlyActive {
				mw.paneStatus = tmux.PaneProcessing
			} else if tmux.IsAtMainPrompt(mw.windowID) {
				// 2) At the main ❯ prompt → done (task completed).
				mw.paneStatus = tmux.PaneDone
			} else {
				// 3) Not processing, not at prompt → waiting for user input
				//    (tool approval, question, etc.)
				mw.paneStatus = tmux.PaneWaiting
			}

			m.managedWindows[key] = mw
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
		if m.configScreen {
			return m.updateConfig(msg)
		}
		if m.pickingEffort {
			return m.updateEffortPicker(msg)
		}
		if m.renaming {
			return m.updateRename(msg)
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
		m.expandedWorktrees = make(map[int]bool)
		return m, nil
	case "enter":
		m.searching = false
		m.filter = m.searchInput.Value()
		m.searchInput.Blur()
		m.cursor = 0
		m.scroll = 0
		m.expandedWorktrees = make(map[int]bool)
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.filter = m.searchInput.Value()
	m.cursor = 0
	m.scroll = 0
	m.expandedWorktrees = make(map[int]bool)
	return m, cmd
}

func (m model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.items()
	switch msg.String() {
	case "q", "ctrl+c":
		if m.insideTmux {
			if cfg.KeepAlive == "on" {
				// Just detach — dashboard and all sessions keep running.
				// Re-running c9s will re-attach to the existing session.
				tmux.Detach()
				return m, nil // don't quit, keep dashboard alive
			}
			tmux.CleanupNavigationKeys(navKeys())
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
		if cfg.Worktrees != "off" {
			if cfg.WorktreeExpand == "selected" {
				m.expandedWorktrees[m.cursor] = !m.expandedWorktrees[m.cursor]
			} else {
				m.showWorktrees = !m.showWorktrees
			}
			// Populate worktree cache for visible sessions.
			if !m.demoMode {
				for _, s := range m.filtered() {
					if s.ProjectPath != "" {
						if _, ok := m.worktreeCache[s.ProjectPath]; !ok {
							m.worktreeCache[s.ProjectPath] = git.ListWorktrees(s.ProjectPath)
						}
					}
				}
			}
			m.saveDashboardState()
		}
	case "enter":
		return m.openSession(items)
	case "n":
		return m.newSession(items, "")
	case "N":
		return m.startEffortPicker(items)
	case "x":
		return m.closeWindow(items)
	case "R":
		return m.startRename(items)
	case "b":
		return m.backupSession(items)
	case "c":
		return m.enterConfigScreen()
	}
	m.adjustScroll()
	return m, nil
}

// openSession opens or switches to the selected session.
func (m model) openSession(items []displayItem) (tea.Model, tea.Cmd) {
	if !m.insideTmux {
		return m, statusCmd("tmux required — run c9s outside tmux to auto-bootstrap", true)
	}

	// If a worktree sub-row is selected, start a new session in that worktree dir.
	if m.cursor >= 0 && m.cursor < len(items) && items[m.cursor].isWorktreeRow {
		wt := items[m.cursor].worktree
		m.effortWorkDir = wt.Path
		return m.newSession(nil, "")
	}

	s := m.selectedSession(items)
	if s == nil {
		return m, nil
	}

	// If we already have a window for this session, try to switch to it.
	if mw, ok := m.managedWindows[s.SessionID]; ok {
		if err := tmux.SelectWindow(mw.windowID); err == nil {
			return m, nil
		}
		// Window was closed externally — clean up and fall through to re-open.
		delete(m.managedWindows, s.SessionID)
	}

	// Build the claude command.
	var claudeCmd string
	workDir := s.ProjectPath

	// Validate session ID before using in shell commands.
	if !claude.IsValidSessionID(s.SessionID) {
		return m, statusCmd("invalid session ID", true)
	}

	switch s.Status {
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
		// No backup — start a new session in the same project directory.
		claudeCmd = "claude"
	}

	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	name := truncWindowName(s.DisplayName())
	windowID, err := tmux.NewWindow(name, claudeCmd, workDir)
	if err != nil {
		return m, statusCmd(fmt.Sprintf("failed to open window: %v", err), true)
	}

	m.managedWindows[s.SessionID] = managedWindow{
		windowID:  windowID,
		sessionID: s.SessionID,
		project:   s.ProjectPath,
	}

	return m, nil
}

// newSession creates a brand new claude session in the selected project.
func (m model) newSession(items []displayItem, effort string) (tea.Model, tea.Cmd) {
	if !m.insideTmux {
		return m, statusCmd("tmux required — run c9s outside tmux to auto-bootstrap", true)
	}

	// Use the selected session's project dir, configured work_dir, or cwd.
	workDir := m.effortWorkDir
	if workDir == "" {
		if s := m.selectedSession(items); s != nil && s.ProjectPath != "" {
			workDir = s.ProjectPath
		}
	}
	if workDir == "" && cfg.WorkDir != "" {
		workDir = cfg.WorkDir
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	cmd := "claude"
	if effort != "" {
		cmd = fmt.Sprintf("claude --effort %s", effort)
	}

	// Name the window with project + short timestamp to distinguish multiple sessions.
	name := fmt.Sprintf("%s·%s", filepath.Base(workDir), time.Now().Format("15:04"))
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
		return m, statusCmd("tmux required — run c9s outside tmux to auto-bootstrap", true)
	}
	workDir := ""
	if s := m.selectedSession(items); s != nil && s.ProjectPath != "" {
		workDir = s.ProjectPath
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
		return m.newSession(nil, effort)
	}
	if key == "esc" || key == "q" {
		m.pickingEffort = false
		m.effortWorkDir = ""
		return m, nil
	}
	return m, nil
}

// getWorktrees returns cached worktrees for a project dir.
func (m *model) getWorktrees(dir string) []git.Worktree {
	if m.demoMode {
		if wts, ok := claude.DemoWorktrees[dir]; ok {
			return wts
		}
		return nil
	}
	if wts, ok := m.worktreeCache[dir]; ok {
		return wts
	}
	wts := git.ListWorktrees(dir)
	m.worktreeCache[dir] = wts
	return wts
}

// reconcileWindows matches managed windows to the claude sessions actually
// running inside them. This handles two cases:
// 1. New sessions (n key): tracked with tmpKey, sessionID empty
// 2. Forked sessions (/fork): old sessionID in map, new session running in window
//
// For forks, the claude process keeps the old --resume arg, so we can't trust
// process args alone. When the tracked session's JSONL is stale, we look for
// the most recently active session in the same project directory.
func (m *model) reconcileWindows(
	sessions []claude.SessionInfo,
	procs []claude.ClaudeProcess,
	getPanePID func(string) (int, error),
	getChildPIDs func(int) []int,
) {
	// Build PID → process lookup.
	pidToProc := make(map[int]claude.ClaudeProcess)
	for _, p := range procs {
		pidToProc[p.PID] = p
	}

	// Build sessionID → session lookup for mtime checks.
	sessionByID := make(map[string]*claude.SessionInfo)
	for i := range sessions {
		sessionByID[sessions[i].SessionID] = &sessions[i]
	}

	toDelete := []string{}
	toAdd := map[string]managedWindow{}

	for key, mw := range m.managedWindows {
		// Determine if the current session is stale (JSONL not written recently).
		currentStale := true
		if mw.sessionID != "" {
			if s, ok := sessionByID[mw.sessionID]; ok {
				if !s.FileMtime.IsZero() && time.Since(s.FileMtime) < 30*time.Second {
					currentStale = false
				}
			}
		}

		// If current session is active and healthy, skip reconciliation.
		if !currentStale {
			continue
		}

		// Get the pane PID for this tmux window.
		panePID, err := getPanePID(mw.windowID)
		if err != nil {
			continue
		}

		// Find claude process children of the pane shell.
		childPIDs := getChildPIDs(panePID)

		var matchedSessionID string
		var matchedProject string
		for _, cpid := range childPIDs {
			if proc, ok := pidToProc[cpid]; ok {
				matchedProject = proc.ProjectPath
				// Only trust --resume if the session it points to is active.
				// After a fork, the process still has --resume old-id but
				// the old session's JSONL is stale.
				if proc.SessionID != "" && proc.SessionID != mw.sessionID {
					if s, ok := sessionByID[proc.SessionID]; ok {
						if !s.FileMtime.IsZero() && time.Since(s.FileMtime) < 30*time.Second {
							matchedSessionID = proc.SessionID
							break
						}
					}
				}
				break
			}
		}

		// Fallback: find the most recently active session in the same project.
		// This handles forks (new session in same project, same process).
		if matchedSessionID == "" {
			project := matchedProject
			if project == "" {
				project = mw.project
			}
			if project != "" {
				var bestID string
				var bestMtime time.Time
				for _, s := range sessions {
					if s.ProjectPath == project &&
						s.SessionID != mw.sessionID &&
						!s.FileMtime.IsZero() &&
						time.Since(s.FileMtime) < 30*time.Second &&
						s.FileMtime.After(bestMtime) {
						bestID = s.SessionID
						bestMtime = s.FileMtime
					}
				}
				matchedSessionID = bestID
			}
		}

		if matchedSessionID == "" || matchedSessionID == key {
			continue
		}

		// Re-key: remove old entry, add under new sessionID.
		toDelete = append(toDelete, key)
		newMW := managedWindow{
			windowID:   mw.windowID,
			sessionID:  matchedSessionID,
			project:    mw.project,
			paneStatus: mw.paneStatus,
		}
		toAdd[matchedSessionID] = newMW

		// Rename the tmux window to reflect the new session.
		if s, ok := sessionByID[matchedSessionID]; ok {
			name := s.DisplayName()
			if len(name) > 30 {
				name = name[:30]
			}
			tmux.RenameWindow(mw.windowID, name)
		}
	}

	for _, k := range toDelete {
		delete(m.managedWindows, k)
	}
	for k, v := range toAdd {
		m.managedWindows[k] = v
	}
}

// saveDashboardState persists toggle states to config so they survive restarts.
func (m model) saveDashboardState() {
	cfg.Dashboard = config.Dashboard{
		ShowTokens:    m.showTokens,
		ShowPreview:   m.showPreview,
		ShowWorktrees: m.showWorktrees,
		GroupBy:       int(m.groupBy),
	}
	config.Save(cfg)
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
	if m.configEditing {
		return m.updateConfigEdit(msg)
	}
	return m.updateConfigNav(msg)
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

	b.WriteString(titleStyle.Render(" c9s — config"))
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
		if m.configEditing && item.fieldIdx == m.configEditIdx {
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

	tmux.KillWindow(mw.windowID)
	delete(m.managedWindows, s.SessionID)
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
		}
		return m, statusCmd("renamed to \""+newTitle+"\"", false)
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// selectedSession returns the session at the cursor, or nil.
func (m model) selectedSession(items []displayItem) *claude.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(items) {
		return nil
	}
	item := items[m.cursor]
	if item.isHeader {
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
	titleText := fmt.Sprintf(" c9s — %d sessions", total)
	if count != total {
		titleText = fmt.Sprintf(" c9s — %d/%d sessions", count, total)
	}
	if m.insideTmux && len(m.managedWindows) > 0 {
		titleText += fmt.Sprintf(" · %d windows", len(m.managedWindows))
	}
	b.WriteString(titleStyle.Render(titleText))
	b.WriteString("\n\n")

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
			prefix := "  ├─ "
			if item.isLastWT {
				prefix = "  └─ "
			}
			row := m.renderColumns(
				"",
				prefix+item.worktree.Branch,
				"",
				"",
				item.worktree.Path,
				"",
				"",
				"",
				true, // dim
			)
			if i == m.cursor {
				row = selectedStyle.Width(tw).Render(row)
			}
			tableLines = append(tableLines, row)
			continue
		}

		rowNum++
		row := m.renderRow(rowNum, item.session)
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
	return altView(out)
}

// renderPreview renders the session preview panel.
func (m model) renderPreview(width, height int) []string {
	items := m.items()
	s := m.selectedSession(items)
	if s == nil {
		// No session selected — show empty preview.
		lines := make([]string, height)
		lines[0] = previewBorder.Width(width - 2).Render(previewDim.Render(" No session selected"))
		return lines
	}

	innerW := width - 4 // border + padding

	var content []string
	addField := func(label, value string) {
		if value == "" {
			return
		}
		line := previewDim.Render(label+": ") + previewVal.Render(value)
		content = append(content, line)
	}
	addWrap := func(label, value string) {
		if value == "" {
			return
		}
		content = append(content, previewDim.Render(label+":"))
		// Word-wrap value to innerW.
		for _, wl := range wordWrap(value, innerW) {
			content = append(content, previewVal.Render(wl))
		}
	}

	// Title
	content = append(content, previewLabel.Render(trunc(s.DisplayName(), innerW)))
	content = append(content, "")

	// Status (with color)
	status := s.Status.String()
	if mw, ok := m.managedWindows[s.SessionID]; ok {
		status = mw.paneStatus.String()
	}
	addField("Status", status)
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

	// First prompt / summary
	if s.FirstPrompt != "" {
		addWrap("First prompt", s.FirstPrompt)
	}
	if s.Summary != "" && s.Summary != s.DisplayName() {
		content = append(content, "")
		addWrap("Summary", s.Summary)
	}

	// Truncate to fit height (minus 2 for border).
	maxLines := height - 2
	if len(content) > maxLines {
		content = content[:maxLines]
	}
	// Pad to fill.
	for len(content) < maxLines {
		content = append(content, "")
	}

	// Render inside border.
	inner := strings.Join(content, "\n")
	bordered := previewBorder.Width(width - 2).Render(inner)
	return strings.Split(bordered, "\n")
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
	if m.pickingEffort {
		return helpStyle.Render(" effort: " +
			helpKeyStyle.Render("1") + " low  " +
			helpKeyStyle.Render("2") + " medium  " +
			helpKeyStyle.Render("3") + " high  " +
			helpKeyStyle.Render("4") + " max  " +
			helpKeyStyle.Render("esc") + " cancel")
	}
	if m.renaming {
		return helpStyle.Render(m.renameInput.View())
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
	if cfg.Worktrees != "off" {
		wtLabel := "show worktrees"
		if cfg.WorktreeExpand == "selected" {
			wtLabel = "expand"
			if m.expandedWorktrees[m.cursor] {
				wtLabel = "collapse"
			}
		} else if m.showWorktrees {
			wtLabel = "hide worktrees"
		}
		parts = append(parts, helpKeyStyle.Render("w")+" "+wtLabel)
	}
	parts = append(parts,
		helpKeyStyle.Render("c") + " config",
		helpKeyStyle.Render("q") + " quit",
	)
	if m.filter != "" {
		parts = append([]string{helpKeyStyle.Render("esc") + " clear"}, parts...)
	}

	return helpStyle.Render(" " + strings.Join(parts, "  "))
}

// filtered returns sessions matching the current search filter.
func (m model) filtered() []claude.SessionInfo {
	if m.filter == "" {
		return m.sessions
	}
	f := strings.ToLower(m.filter)
	var out []claude.SessionInfo
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.DisplayName()), f) ||
			strings.Contains(strings.ToLower(s.ProjectPath), f) ||
			strings.Contains(strings.ToLower(s.SessionID), f) ||
			strings.Contains(strings.ToLower(s.GitBranch), f) {
			out = append(out, s)
		}
	}
	return out
}

// items returns display items, optionally grouped.
func (m model) items() []displayItem {
	sessions := m.filtered()
	var base []displayItem

	if m.groupBy == groupNone {
		base = make([]displayItem, len(sessions))
		for i, s := range sessions {
			base[i] = displayItem{session: s}
		}
	} else {
		type group struct {
			name   string
			items  []claude.SessionInfo
			newest time.Time
			order  int // for fixed ordering in status mode
		}
		groups := make(map[string]*group)
		var order []string

		for _, s := range sessions {
			var name string
			var sortOrder int
			switch m.groupBy {
			case groupProject:
				name = projectName(s.ProjectPath)
				if name == "" {
					name = "(no project)"
				}
			case groupStatus:
				// Use the effective status (pane status for managed windows).
				if mw, ok := m.managedWindows[s.SessionID]; ok {
					name = mw.paneStatus.String()
				} else {
					name = s.Status.String()
				}
				// Fixed order: processing, waiting, active, idle, done, resumable, archived.
				switch name {
				case "processing":
					sortOrder = 0
				case "waiting":
					sortOrder = 1
				case "active":
					sortOrder = 2
				case "idle":
					sortOrder = 3
				case "done":
					sortOrder = 4
				case "resumable":
					sortOrder = 5
				case "archived":
					sortOrder = 6
				}
			}

			g, ok := groups[name]
			if !ok {
				g = &group{name: name, order: sortOrder}
				groups[name] = g
				order = append(order, name)
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

		for _, name := range order {
			g := groups[name]
			base = append(base, displayItem{isHeader: true, header: fmt.Sprintf("%s (%d)", g.name, len(g.items))})
			for _, s := range g.items {
				base = append(base, displayItem{session: s})
			}
		}
	}

	// Insert worktree sub-rows if enabled.
	if cfg.Worktrees == "off" {
		return base
	}

	var result []displayItem
	sessionIdx := 0 // index of session rows only (for "selected" mode)
	for _, item := range base {
		result = append(result, item)
		if item.isHeader {
			continue
		}
		// Check if we should show worktree sub-rows for this session.
		show := false
		if cfg.WorktreeExpand == "all" {
			show = m.showWorktrees
		} else {
			show = m.expandedWorktrees[sessionIdx]
		}
		if show && item.session.ProjectPath != "" {
			wts := m.lookupWorktrees(item.session.ProjectPath)
			for i, wt := range wts {
				result = append(result, displayItem{
					isWorktreeRow: true,
					worktree:      wt,
					isLastWT:      i == len(wts)-1,
				})
			}
		}
		sessionIdx++
	}
	return result
}

// lookupWorktrees returns worktrees from cache (read-only, no cache mutation).
func (m model) lookupWorktrees(dir string) []git.Worktree {
	if m.demoMode {
		return claude.DemoWorktrees[dir]
	}
	return m.worktreeCache[dir]
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

func (m model) renderRow(num int, s claude.SessionInfo) string {
	tokStr := ""
	if s.TotalTokens() > 0 {
		tokStr = fmtTokens(s.TotalTokens())
	}
	// Override status with pane status for managed windows.
	status := s.Status.String()
	if mw, ok := m.managedWindows[s.SessionID]; ok {
		status = mw.paneStatus.String()
	}
	dim := s.Status == claude.StatusArchived && status == "archived"
	row := m.renderColumns(
		fmt.Sprintf("%d", num),
		s.DisplayName(),
		status,
		s.GitBranch,
		projectName(s.ProjectPath),
		fmt.Sprintf("%d", s.MessageCount),
		tokStr,
		fmtModified(s.Modified),
		dim,
	)
	return row
}

func (m model) showBranchCol() bool {
	return m.showWorktrees && cfg.Worktrees != "off"
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
	branchW := 0
	if m.showBranchCol() {
		branchW = 14
	}
	projW := 0
	if tw >= 90 {
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
		case "waiting":
			statusCell = stWaiting.Render(statusCell)
		case "processing":
			statusCell = stProcessing.Render(statusCell)
		case "done":
			statusCell = stDone.Render(statusCell)
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

// formatUsage builds the usage string for the tmux status bar based on config.
func formatUsage(s claude.SessionInfo) string {
	if cfg.StatusUsage == "off" {
		return ""
	}
	budgetTokens := s.InputTokens + s.OutputTokens
	if budgetTokens == 0 {
		return ""
	}

	// Get real usage percentage from Anthropic API (cached 30s).
	var apiPercent string
	if cfg.StatusUsage == "percent" || cfg.StatusUsage == "all" {
		if usage, err := claude.FetchUsage(); err == nil {
			apiPercent = fmt.Sprintf("%.0f%%", usage.FiveHour.Utilization)
		}
	}

	var parts []string
	switch cfg.StatusUsage {
	case "tokens":
		parts = append(parts, fmtTokens(budgetTokens))
	case "percent":
		if apiPercent != "" {
			parts = append(parts, apiPercent)
		}
	case "cost":
		parts = append(parts, fmt.Sprintf("~$%.2f", estimateCost(s)))
	case "all":
		parts = append(parts, fmtTokens(budgetTokens))
		if apiPercent != "" {
			parts = append(parts, apiPercent)
		}
		parts = append(parts, fmt.Sprintf("~$%.2f", estimateCost(s)))
	}
	return strings.Join(parts, " · ")
}

// estimateCost returns the estimated API-equivalent cost for a session.
// Uses Sonnet pricing: $3/M input, $15/M output, $0.30/M cache read.
func estimateCost(s claude.SessionInfo) float64 {
	inputCost := float64(s.InputTokens+s.CacheCreate) / 1_000_000 * 3.0
	outputCost := float64(s.OutputTokens) / 1_000_000 * 15.0
	cacheCost := float64(s.CacheRead) / 1_000_000 * 0.30
	return inputCost + outputCost + cacheCost
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
		default:
			filtered = append(filtered, arg)
		}
	}
	args = filtered

	// If tmux is available and we're not already inside tmux, bootstrap.
	if !insideTmux && tmux.Available() && !tmux.InSession() {
		selfBin, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if tmux.SessionExists() {
			// Session already exists. Re-create dashboard window if it was
			// closed (e.g., after keep_alive detach killed the bubbletea process).
			if !tmux.WindowExists(tmux.SessionName + ":" + tmux.DashboardWindow) {
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

	var sessions []claude.SessionInfo
	var loadErr error
	if demoMode {
		sessions = claude.DemoSessions()
	} else {
		sessions, loadErr = claude.ListAllSessions()
	}
	m := initialModel(sessions, loadErr, insideTmux || tmux.InC9sSession())
	m.demoMode = demoMode
	if demoMode {
		m.showTokens = true    // show tokens in demo for nicer screenshots
		m.showWorktrees = true // show worktrees in demo
		cfg.Worktrees = "always"
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
