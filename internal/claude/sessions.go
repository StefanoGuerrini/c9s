package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// DebugLog is set by main when --debug is enabled. No-op by default.
var DebugLog = func(string, ...any) {}

// validSessionID matches UUID-like session IDs (hex + dashes only).
var validSessionID = regexp.MustCompile(`^[a-fA-F0-9-]+$`)

// IsValidSessionID returns true if the session ID is safe to use in commands.
func IsValidSessionID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && validSessionID.MatchString(id)
}

// --- Caching layer to avoid re-reading files that haven't changed ---

var cache struct {
	mu sync.Mutex

	// history.jsonl cache
	historyMtime time.Time
	historySess  []SessionInfo

	// Token usage cache: sessionID → (mtime, tokens)
	tokens map[string]cachedTokens

	// Process list cache (short TTL, just avoid calling lsof too often)
	procsTime time.Time
	procs     []ClaudeProcess
}

type cachedTokens struct {
	mtime       time.Time
	input       int
	output      int
	cacheRead   int
	cacheCreate int
	models      map[string]int // model name → total tokens
	lastModel   string         // most recent model used in session
	forkedFrom  string         // parent session ID (from forkedFrom field)
}

// Status represents the lifecycle state of a Claude Code session.
type Status int

const (
	StatusArchived  Status = iota // no JSONL file on disk
	StatusResumable               // JSONL file exists, no active process
	StatusIdle                    // claude process running, but not recently active
	StatusActive                  // JSONL file modified within ActiveThreshold
)

// ActiveThreshold is how recently a session JSONL must be modified to count as "active".
const ActiveThreshold = 5 * time.Minute

func (s Status) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusIdle:
		return "idle"
	case StatusResumable:
		return "resumable"
	default:
		return "archived"
	}
}

// SessionInfo represents a single Claude Code session found on disk.
type SessionInfo struct {
	SessionID    string
	Summary      string
	CustomTitle  string
	FirstPrompt  string
	MessageCount int
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheCreate  int
	Created      time.Time
	Modified     time.Time
	FileMtime    time.Time // mtime of session JSONL file (zero if not on disk)
	GitBranch    string
	ProjectPath  string
	Dir             string // actual project directory path (e.g. ~/.claude/projects/-Users-foo-bar)
	Status          Status
	DemoPaneStatus  int // 0=none, 1=processing, 2=waiting, 3=done (only used in --demo mode)
}

// TotalTokens returns the sum of input and output tokens.
func (s SessionInfo) TotalTokens() int {
	return s.InputTokens + s.OutputTokens + s.CacheRead + s.CacheCreate
}

// DisplayName returns the best available name for display.
// Precedence: customTitle > summary > truncated firstPrompt > ID prefix.
func (s SessionInfo) DisplayName() string {
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	if s.Summary != "" {
		return s.Summary
	}
	if s.FirstPrompt != "" {
		r := []rune(s.FirstPrompt)
		if len(r) > 60 {
			return string(r[:57]) + "..."
		}
		return s.FirstPrompt
	}
	if len(s.SessionID) > 8 {
		return s.SessionID[:8] + "..."
	}
	return s.SessionID
}

// claudeDir returns the Claude config directory.
// Respects CLAUDE_CONFIG_DIR if set, otherwise defaults to ~/.claude.
func claudeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// ProjectDir returns the Claude project directory for a given project path.
// Claude encodes paths by replacing separators with dashes and prepending a dash.
// Example: /Users/foo/bar → ~/.claude/projects/-Users-foo-bar
func ProjectDir(projectPath string) string {
	encoded := "-" + strings.ReplaceAll(strings.TrimPrefix(projectPath, "/"), "/", "-")
	return filepath.Join(claudeDir(), "projects", encoded)
}

// ListAllSessions returns all Claude sessions discovered from
// ~/.claude/history.jsonl, enriched with metadata from sessions-index.json
// files where available. Sorted by modified time (newest first).
func ListAllSessions() ([]SessionInfo, error) {
	historyPath := filepath.Join(claudeDir(), "history.jsonl")
	return listAllSessionsFrom(historyPath)
}

func listAllSessionsFrom(historyPath string) ([]SessionInfo, error) {
	sessions, err := readHistory(historyPath)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	// Set Dir for each session based on ProjectPath.
	for i := range sessions {
		if sessions[i].ProjectPath != "" {
			sessions[i].Dir = ProjectDir(sessions[i].ProjectPath)
		}
	}

	// Enrich with metadata from sessions-index.json files.
	byDir := make(map[string][]int)
	for i, s := range sessions {
		if s.Dir != "" {
			byDir[s.Dir] = append(byDir[s.Dir], i)
		}
	}

	for dir, indices := range byDir {
		indexed, err := readSessionsIndex(dir)
		if err != nil || len(indexed) == 0 {
			continue
		}
		indexMap := make(map[string]SessionInfo)
		for _, s := range indexed {
			indexMap[s.SessionID] = s
		}
		for _, idx := range indices {
			if meta, ok := indexMap[sessions[idx].SessionID]; ok {
				if meta.CustomTitle != "" {
					sessions[idx].CustomTitle = meta.CustomTitle
				}
				if meta.Summary != "" && sessions[idx].Summary == "" {
					sessions[idx].Summary = meta.Summary
				}
				if meta.GitBranch != "" {
					sessions[idx].GitBranch = meta.GitBranch
				}
			}
		}
	}

	// Check JSONL files on disk: resumability, mtime, token usage.
	now := time.Now()
	for i := range sessions {
		if sessions[i].Dir == "" {
			continue
		}
		path := filepath.Join(sessions[i].Dir, sessions[i].SessionID+".jsonl")
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		// Skip stub files (e.g. only a last-prompt entry, < 500 bytes).
		// These aren't truly resumable.
		if fi.Size() < 500 {
			continue
		}
		sessions[i].Status = StatusResumable
		sessions[i].FileMtime = fi.ModTime()

		// If file was modified very recently, it's actively being used.
		if now.Sub(fi.ModTime()) < ActiveThreshold {
			sessions[i].Status = StatusActive
		}

		readTokenUsage(&sessions[i])
	}

	// Detect running claude processes for idle detection.
	procs := ListClaudeProcesses()
	procBySessionID := make(map[string]bool)
	procByProject := make(map[string]bool)
	for _, p := range procs {
		if p.SessionID != "" {
			procBySessionID[p.SessionID] = true
		}
		if p.ProjectPath != "" {
			procByProject[p.ProjectPath] = true
		}
	}

	// Promote resumable → idle if a claude process is running for this session.
	for i := range sessions {
		if sessions[i].Status == StatusActive {
			continue // already the highest state
		}
		if sessions[i].Status != StatusResumable {
			continue // archived sessions can't be idle
		}

		// Direct match: --resume <session-id>
		if procBySessionID[sessions[i].SessionID] {
			sessions[i].Status = StatusIdle
			continue
		}

		// Project match: a claude process is running in this project dir.
		if sessions[i].ProjectPath != "" && procByProject[sessions[i].ProjectPath] {
			sessions[i].Status = StatusIdle
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	return sessions, nil
}

// backupDir returns the path to the c9s backup directory.
// Can be overridden in tests via BackupDirOverride.
var BackupDirOverride string

func backupDir() string {
	if BackupDirOverride != "" {
		return BackupDirOverride
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".c9s", "backups")
}

// BackupSession copies a session's JSONL file to ~/.c9s/backups/ and writes
// a .meta file containing the source directory path so it can be restored later.
func BackupSession(s *SessionInfo) error {
	if s.Dir == "" {
		return fmt.Errorf("no project directory for this session")
	}
	src := filepath.Join(s.Dir, s.SessionID+".jsonl")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("session file not found: %w", err)
	}

	dir := backupDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	dst := filepath.Join(dir, s.SessionID+".jsonl")
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy backup: %w", err)
	}

	// Write metadata file with the source directory.
	metaPath := filepath.Join(dir, s.SessionID+".meta")
	if err := os.WriteFile(metaPath, []byte(s.Dir), 0644); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}
	return nil
}

// RestoreSession restores a backed-up session JSONL file to its original location.
// Returns true if a backup was found and restored.
func RestoreSession(sessionID string) (bool, error) {
	dir := backupDir()
	backupPath := filepath.Join(dir, sessionID+".jsonl")
	metaPath := filepath.Join(dir, sessionID+".meta")

	if _, err := os.Stat(backupPath); err != nil {
		return false, nil // no backup
	}

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return false, fmt.Errorf("read backup meta: %w", err)
	}
	destDir := strings.TrimSpace(string(metaData))
	if destDir == "" {
		return false, fmt.Errorf("empty backup metadata")
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return false, fmt.Errorf("create dest dir: %w", err)
	}

	dst := filepath.Join(destDir, sessionID+".jsonl")
	if err := copyFile(backupPath, dst); err != nil {
		return false, fmt.Errorf("restore backup: %w", err)
	}
	return true, nil
}

// RefreshBackups checks all backed-up sessions and re-copies the source JSONL
// if it has a newer mtime than the backup. This is cheap (just os.Stat per backup)
// and only does I/O when something actually changed.
func RefreshBackups() {
	dir := backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no backup dir yet
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".meta")
		metaData, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		srcDir := strings.TrimSpace(string(metaData))
		if srcDir == "" {
			continue
		}
		src := filepath.Join(srcDir, sessionID+".jsonl")
		dst := filepath.Join(dir, sessionID+".jsonl")

		srcInfo, err := os.Stat(src)
		if err != nil {
			continue // source gone, nothing to update
		}
		dstInfo, err := os.Stat(dst)
		if err != nil {
			continue // backup gone
		}
		if srcInfo.ModTime().After(dstInfo.ModTime()) {
			_ = copyFile(src, dst)
		}
	}
}

// HasBackup returns true if a backup exists for the given session ID.
func HasBackup(sessionID string) bool {
	dir := backupDir()
	_, err := os.Stat(filepath.Join(dir, sessionID+".jsonl"))
	return err == nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// readTokenUsage reads token counts from a session's JSONL file,
// using a cache to avoid re-scanning unchanged files.
func readTokenUsage(s *SessionInfo) {
	cache.mu.Lock()
	if cache.tokens == nil {
		cache.tokens = make(map[string]cachedTokens)
	}

	// If mtime hasn't changed, use cached values.
	if ct, ok := cache.tokens[s.SessionID]; ok && ct.mtime.Equal(s.FileMtime) {
		s.InputTokens = ct.input
		s.OutputTokens = ct.output
		s.CacheRead = ct.cacheRead
		s.CacheCreate = ct.cacheCreate
		cache.mu.Unlock()
		return
	}
	cache.mu.Unlock()

	path := filepath.Join(s.Dir, s.SessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var models map[string]int
	var lastModel string
	var forkedFrom string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var entry struct {
			ForkedFrom *struct {
				SessionID string `json:"sessionId"`
			} `json:"forkedFrom"`
			Message *struct {
				Model string `json:"model"`
				Usage *struct {
					InputTokens              int `json:"input_tokens"`
					OutputTokens             int `json:"output_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if forkedFrom == "" && entry.ForkedFrom != nil && entry.ForkedFrom.SessionID != "" {
			forkedFrom = entry.ForkedFrom.SessionID
		}
		if entry.Message != nil && entry.Message.Usage != nil {
			u := entry.Message.Usage
			s.InputTokens += u.InputTokens
			s.OutputTokens += u.OutputTokens
			s.CacheRead += u.CacheReadInputTokens
			s.CacheCreate += u.CacheCreationInputTokens
			if entry.Message.Model != "" {
				lastModel = entry.Message.Model
				if models == nil {
					models = make(map[string]int)
				}
				models[entry.Message.Model] += u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			}
		}
	}

	cache.mu.Lock()
	cache.tokens[s.SessionID] = cachedTokens{
		mtime:       s.FileMtime,
		input:       s.InputTokens,
		output:      s.OutputTokens,
		cacheRead:   s.CacheRead,
		cacheCreate: s.CacheCreate,
		models:      models,
		lastModel:   lastModel,
		forkedFrom:  forkedFrom,
	}
	cache.mu.Unlock()
}

// GetSupersededSessions returns session IDs that have been replaced by a newer
// fork or compaction. A session is superseded if another session's forkedFrom
// points to it AND the successor has a newer mtime.
// Uses the cached forkedFrom data populated by readTokenUsage.
func GetSupersededSessions() map[string]bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	superseded := make(map[string]bool)
	for _, ct := range cache.tokens {
		if ct.forkedFrom == "" {
			continue
		}
		parent, ok := cache.tokens[ct.forkedFrom]
		if !ok {
			continue // parent not in cache (archived, no JSONL)
		}
		// Only supersede if the child is newer than the parent.
		if ct.mtime.After(parent.mtime) {
			superseded[ct.forkedFrom] = true
		}
	}
	return superseded
}

// ReadTokenUsageForTest exposes readTokenUsage for use in external test packages.
func ReadTokenUsageForTest(s *SessionInfo) { readTokenUsage(s) }

// GetLastModel returns the most recent model used in a session.
func GetLastModel(sessionID string) string {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if ct, ok := cache.tokens[sessionID]; ok {
		return ct.lastModel
	}
	return ""
}

// GetModelBreakdown returns total tokens per model across all given sessions,
// using the cached token data populated by readTokenUsage.
func GetModelBreakdown(sessions []SessionInfo) map[string]int {
	result := make(map[string]int)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, s := range sessions {
		if ct, ok := cache.tokens[s.SessionID]; ok {
			for m, t := range ct.models {
				result[m] += t
			}
		}
	}
	return result
}

// readHistory parses ~/.claude/history.jsonl and returns one SessionInfo per
// unique sessionId, aggregating message counts and timestamps.
// Uses mtime-based caching to skip re-parsing if unchanged.
func readHistory(path string) ([]SessionInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cache.mu.Lock()
	if fi.ModTime().Equal(cache.historyMtime) && cache.historySess != nil {
		// Return a copy so callers can mutate without affecting cache.
		result := make([]SessionInfo, len(cache.historySess))
		copy(result, cache.historySess)
		cache.mu.Unlock()
		return result, nil
	}
	cache.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	type sessionAgg struct {
		sessionID   string
		projectPath string
		firstPrompt string
		minTS       int64
		maxTS       int64
		count       int
	}

	byID := make(map[string]*sessionAgg)
	var order []string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var entry struct {
			Display   string `json:"display"`
			Timestamp int64  `json:"timestamp"`
			Project   string `json:"project"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.SessionID == "" {
			continue
		}

		agg, ok := byID[entry.SessionID]
		if !ok {
			agg = &sessionAgg{
				sessionID:   entry.SessionID,
				projectPath: entry.Project,
				firstPrompt: entry.Display,
				minTS:       entry.Timestamp,
				maxTS:       entry.Timestamp,
			}
			byID[entry.SessionID] = agg
			order = append(order, entry.SessionID)
		}
		agg.count++
		if entry.Timestamp < agg.minTS {
			agg.minTS = entry.Timestamp
		}
		if entry.Timestamp > agg.maxTS {
			agg.maxTS = entry.Timestamp
		}
	}

	sessions := make([]SessionInfo, 0, len(order))
	for _, id := range order {
		agg := byID[id]
		prompt := firstLine(agg.firstPrompt)
		if len(prompt) > 200 {
			prompt = prompt[:200]
		}
		sessions = append(sessions, SessionInfo{
			SessionID:    agg.sessionID,
			ProjectPath:  agg.projectPath,
			FirstPrompt:  prompt,
			MessageCount: agg.count,
			Created:      time.UnixMilli(agg.minTS),
			Modified:     time.UnixMilli(agg.maxTS),
			Status:       StatusArchived, // default, will be promoted if file exists
		})
	}

	cache.mu.Lock()
	cache.historyMtime = fi.ModTime()
	cache.historySess = make([]SessionInfo, len(sessions))
	copy(cache.historySess, sessions)
	cache.mu.Unlock()

	return sessions, nil
}

// firstLine returns the first non-empty line of text, trimmed of whitespace.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// decodeProjectDirName converts a Claude project directory name back to a path.
func decodeProjectDirName(name string) string {
	return "/" + strings.ReplaceAll(strings.TrimPrefix(name, "-"), "-", "/")
}

// ClaudeProcess represents a running claude CLI process.
type ClaudeProcess struct {
	PID         int
	SessionID   string // from --resume flag, if present
	ProjectPath string // cwd of the process
}

// ListClaudeProcesses finds running `claude` CLI processes and extracts
// session IDs (from --resume) and working directories (from lsof).
// Results are cached for 5 seconds.
func ListClaudeProcesses() []ClaudeProcess {
	cache.mu.Lock()
	if time.Since(cache.procsTime) < 5*time.Second && cache.procs != nil {
		result := cache.procs
		cache.mu.Unlock()
		return result
	}
	cache.mu.Unlock()

	out, err := exec.Command("ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil
	}

	type procInfo struct {
		pid  int
		args string
	}
	var procs []procInfo
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		pid := 0
		fmt.Sscanf(parts[0], "%d", &pid)
		if pid == 0 {
			continue
		}
		args := strings.TrimSpace(parts[1])
		// Only match bare "claude" commands, not Claude.app or claude-something
		if !strings.HasPrefix(args, "claude") || strings.Contains(args, "Claude.app") || strings.HasPrefix(args, "claude-") {
			continue
		}
		procs = append(procs, procInfo{pid: pid, args: args})
	}

	result := make([]ClaudeProcess, 0, len(procs))
	for _, p := range procs {
		cp := ClaudeProcess{PID: p.pid}

		// Extract --resume session ID if present.
		if idx := strings.Index(p.args, "--resume "); idx >= 0 {
			rest := p.args[idx+len("--resume "):]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				cp.SessionID = fields[0]
			}
		}

		result = append(result, cp)
	}

	// Batch lsof call for all PIDs at once.
	if len(result) > 0 {
		var pids []string
		for _, cp := range result {
			pids = append(pids, fmt.Sprintf("%d", cp.PID))
		}
		pidArg := strings.Join(pids, ",")
		if cwdOut, err := exec.Command("lsof", "-d", "cwd", "-p", pidArg, "-Fn").Output(); err == nil {
			// lsof output: "p<pid>\nn<path>\n" blocks per process.
			cwdByPID := make(map[int]string)
			currentPID := 0
			for _, l := range strings.Split(string(cwdOut), "\n") {
				if strings.HasPrefix(l, "p") {
					fmt.Sscanf(l[1:], "%d", &currentPID)
				} else if strings.HasPrefix(l, "n/") && l != "n/" {
					cwdByPID[currentPID] = l[1:]
				}
			}
			for i := range result {
				if cwd, ok := cwdByPID[result[i].PID]; ok {
					result[i].ProjectPath = cwd
				}
			}
		}
	}

	cache.mu.Lock()
	cache.procs = result
	cache.procsTime = time.Now()
	cache.mu.Unlock()

	return result
}

// ChildPIDs returns the PIDs of direct child processes of the given PID.
func ChildPIDs(parentPID int) []int {
	out, err := exec.Command("pgrep", "-P", fmt.Sprintf("%d", parentPID)).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid := 0
		fmt.Sscanf(line, "%d", &pid)
		if pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// --- Internal types matching Claude's sessions-index.json ---

type rawIndex struct {
	Version int               `json:"version"`
	Entries []json.RawMessage `json:"entries"`
}

type rawEntry struct {
	SessionID    string `json:"sessionId"`
	FullPath     string `json:"fullPath,omitempty"`
	FileMtime    int64  `json:"fileMtime,omitempty"`
	FirstPrompt  string `json:"firstPrompt,omitempty"`
	Summary      string `json:"summary,omitempty"`
	CustomTitle  string `json:"customTitle,omitempty"`
	MessageCount int    `json:"messageCount,omitempty"`
	Created      string `json:"created,omitempty"`
	Modified     string `json:"modified,omitempty"`
	GitBranch    string `json:"gitBranch,omitempty"`
	ProjectPath  string `json:"projectPath,omitempty"`
	IsSidechain  bool   `json:"isSidechain,omitempty"`
}

func readRawIndex(path string) (*rawIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx rawIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// RenameSession sets the customTitle for a session in its sessions-index.json.
// If the session doesn't exist in the index, it adds a new entry.
func RenameSession(projectDir, sessionID, newTitle string) error {
	path := filepath.Join(projectDir, "sessions-index.json")

	idx, err := readRawIndex(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create a new index with just this entry.
			idx = &rawIndex{Version: 1}
		} else {
			return err
		}
	}

	found := false
	for i, raw := range idx.Entries {
		var e rawEntry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.SessionID == sessionID {
			e.CustomTitle = newTitle
			updated, err := json.Marshal(e)
			if err != nil {
				return err
			}
			idx.Entries[i] = updated
			found = true
			break
		}
	}

	if !found {
		e := rawEntry{SessionID: sessionID, CustomTitle: newTitle}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		idx.Entries = append(idx.Entries, raw)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readSessionsIndex(dir string) ([]SessionInfo, error) {
	idx, err := readRawIndex(filepath.Join(dir, "sessions-index.json"))
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, raw := range idx.Entries {
		var e rawEntry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		s := SessionInfo{
			SessionID:    e.SessionID,
			Summary:      e.Summary,
			CustomTitle:  e.CustomTitle,
			FirstPrompt:  e.FirstPrompt,
			MessageCount: e.MessageCount,
			GitBranch:    e.GitBranch,
			ProjectPath:  e.ProjectPath,
		}
		if t, err := time.Parse(time.RFC3339, e.Created); err == nil {
			s.Created = t
		}
		if t, err := time.Parse(time.RFC3339, e.Modified); err == nil {
			s.Modified = t
		}
		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	return sessions, nil
}
