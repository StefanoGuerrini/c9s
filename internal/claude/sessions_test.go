package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testIndex(version int, entries ...rawEntry) rawIndex {
	idx := rawIndex{Version: version}
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		idx.Entries = append(idx.Entries, raw)
	}
	return idx
}

func TestProjectDir(t *testing.T) {
	dir := ProjectDir("/Users/stefano/Personal/c9s")
	if !filepath.IsAbs(dir) {
		t.Errorf("ProjectDir returned non-absolute path: %s", dir)
	}
	base := filepath.Base(dir)
	if base != "-Users-stefano-Personal-c9s" {
		t.Errorf("ProjectDir base = %q, want %q", base, "-Users-stefano-Personal-c9s")
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name string
		s    SessionInfo
		want string
	}{
		{"customTitle takes precedence", SessionInfo{SessionID: "abc", Summary: "sum", CustomTitle: "custom"}, "custom"},
		{"summary when no customTitle", SessionInfo{SessionID: "abc", Summary: "my summary"}, "my summary"},
		{"firstPrompt when no summary", SessionInfo{SessionID: "abc", FirstPrompt: "do something"}, "do something"},
		{"firstPrompt truncated", SessionInfo{SessionID: "abc", FirstPrompt: "a very long prompt that goes on and on and on and keeps going to exceed sixty characters"}, "a very long prompt that goes on and on and on and keeps g..."},
		{"sessionID prefix fallback", SessionInfo{SessionID: "abcdef12-3456-7890"}, "abcdef12..."},
		{"short sessionID", SessionInfo{SessionID: "abc"}, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadHistory(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.jsonl")

	history := `{"display":"fix the auth bug","timestamp":1768478264857,"project":"/Users/test/projectA","sessionId":"sess-a1"}
{"display":"yes do it","timestamp":1768478300000,"project":"/Users/test/projectA","sessionId":"sess-a1"}
{"display":"add tests","timestamp":1768479126289,"project":"/Users/test/projectB","sessionId":"sess-b1"}
{"display":"hello world","timestamp":1768400000000,"project":"/Users/test/projectA","sessionId":"sess-a2"}
`
	os.WriteFile(historyPath, []byte(history), 0644)

	sessions, err := readHistory(historyPath)
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}

	byID := make(map[string]SessionInfo)
	for _, s := range sessions {
		byID[s.SessionID] = s
	}

	a1 := byID["sess-a1"]
	if a1.MessageCount != 2 {
		t.Errorf("sess-a1 MessageCount = %d, want 2", a1.MessageCount)
	}
	if a1.ProjectPath != "/Users/test/projectA" {
		t.Errorf("sess-a1 ProjectPath = %q", a1.ProjectPath)
	}
	if a1.FirstPrompt != "fix the auth bug" {
		t.Errorf("sess-a1 FirstPrompt = %q", a1.FirstPrompt)
	}
	if a1.Created.After(a1.Modified) {
		t.Errorf("Created (%v) should be before Modified (%v)", a1.Created, a1.Modified)
	}

	b1 := byID["sess-b1"]
	if b1.MessageCount != 1 {
		t.Errorf("sess-b1 MessageCount = %d, want 1", b1.MessageCount)
	}
}

func TestReadHistoryEmpty(t *testing.T) {
	dir := t.TempDir()

	sessions, err := readHistory(filepath.Join(dir, "nonexistent.jsonl"))
	if err != nil {
		t.Fatalf("readHistory: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListAllSessionsFromHistory(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	historyPath := filepath.Join(tmpHome, ".claude", "history.jsonl")
	os.MkdirAll(filepath.Dir(historyPath), 0755)
	history := `{"display":"fix bug","timestamp":1768478264857,"project":"/Users/test/projectA","sessionId":"sess-a1"}
{"display":"add feature","timestamp":1768479126289,"project":"/Users/test/projectB","sessionId":"sess-b1"}
`
	os.WriteFile(historyPath, []byte(history), 0644)

	// Create sessions-index.json for projectA with a customTitle.
	projADir := ProjectDir("/Users/test/projectA")
	os.MkdirAll(projADir, 0755)
	idx := testIndex(1, rawEntry{SessionID: "sess-a1", CustomTitle: "My Bug Fix", Summary: "Fixed auth"})
	data, _ := json.MarshalIndent(idx, "", "  ")
	os.WriteFile(filepath.Join(projADir, "sessions-index.json"), data, 0644)

	sessions, err := ListAllSessions()
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	var foundA, foundB bool
	for _, s := range sessions {
		if s.SessionID == "sess-a1" {
			foundA = true
			if s.CustomTitle != "My Bug Fix" {
				t.Errorf("sess-a1 CustomTitle = %q, want %q", s.CustomTitle, "My Bug Fix")
			}
			if s.Summary != "Fixed auth" {
				t.Errorf("sess-a1 Summary = %q, want %q", s.Summary, "Fixed auth")
			}
		}
		if s.SessionID == "sess-b1" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("foundA=%v foundB=%v", foundA, foundB)
	}
}

func TestListAllSessionsEmptyHistory(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	sessions, err := ListAllSessions()
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"hello\nworld", "hello"},
		{"\n\nhello\nworld", "hello"},
		{"  hello  \nworld", "hello"},
		{"\n\n\n", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := firstLine(tt.input); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadTokenUsage(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	projDir := ProjectDir("/Users/test/proj")
	os.MkdirAll(projDir, 0755)

	// Write a session JSONL with usage data on assistant messages.
	jsonl := `{"type":"user","message":{"role":"user","content":"hello"}}
{"message":{"role":"assistant","type":"message","usage":{"input_tokens":10,"output_tokens":50,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}
{"message":{"role":"assistant","type":"message","usage":{"input_tokens":5,"output_tokens":30,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}}}
{"type":"summary","summary":"test session"}
`
	os.WriteFile(filepath.Join(projDir, "sess-1.jsonl"), []byte(jsonl), 0644)

	s := &SessionInfo{
		SessionID: "sess-1",
		Dir:       projDir,
	}
	readTokenUsage(s)

	if s.InputTokens != 15 {
		t.Errorf("InputTokens = %d, want 15", s.InputTokens)
	}
	if s.OutputTokens != 80 {
		t.Errorf("OutputTokens = %d, want 80", s.OutputTokens)
	}
	if s.CacheRead != 300 {
		t.Errorf("CacheRead = %d, want 300", s.CacheRead)
	}
	if s.CacheCreate != 20 {
		t.Errorf("CacheCreate = %d, want 20", s.CacheCreate)
	}
	if s.TotalTokens() != 415 {
		t.Errorf("TotalTokens = %d, want 415", s.TotalTokens())
	}
}

func TestReadTokenUsageMissingFile(t *testing.T) {
	s := &SessionInfo{
		SessionID: "nonexistent",
		Dir:       t.TempDir(),
	}
	readTokenUsage(s) // should not panic
	if s.TotalTokens() != 0 {
		t.Errorf("expected 0 tokens for missing file, got %d", s.TotalTokens())
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusArchived, "archived"},
		{StatusResumable, "resumable"},
		{StatusIdle, "idle"},
		{StatusActive, "active"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestSessionStatusFromFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	historyPath := filepath.Join(tmpHome, ".claude", "history.jsonl")
	os.MkdirAll(filepath.Dir(historyPath), 0755)

	// Two sessions: one with a JSONL file, one without.
	history := `{"display":"has file","timestamp":1768478264857,"project":"/Users/test/proj","sessionId":"sess-with-file"}
{"display":"no file","timestamp":1768479126289,"project":"/Users/test/proj","sessionId":"sess-no-file"}
`
	os.WriteFile(historyPath, []byte(history), 0644)

	// Create JSONL file only for sess-with-file (recently modified → active).
	projDir := ProjectDir("/Users/test/proj")
	os.MkdirAll(projDir, 0755)
	// File must be >= 500 bytes to not be treated as a stub.
	bigEntry := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 500) + `"}}` + "\n"
	os.WriteFile(filepath.Join(projDir, "sess-with-file.jsonl"), []byte(bigEntry), 0644)

	sessions, err := ListAllSessions()
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	byID := make(map[string]SessionInfo)
	for _, s := range sessions {
		byID[s.SessionID] = s
	}

	withFile := byID["sess-with-file"]
	if withFile.Status == StatusArchived {
		t.Errorf("sess-with-file should not be archived (file exists)")
	}
	if withFile.FileMtime.IsZero() {
		t.Error("sess-with-file should have FileMtime set")
	}

	noFile := byID["sess-no-file"]
	if noFile.Status != StatusArchived {
		t.Errorf("sess-no-file status = %v, want archived", noFile.Status)
	}
}

func TestRenameSession(t *testing.T) {
	dir := t.TempDir()

	// Create an existing index with one session.
	idx := testIndex(1, rawEntry{SessionID: "sess-1", Summary: "original"})
	data, _ := json.MarshalIndent(idx, "", "  ")
	os.WriteFile(filepath.Join(dir, "sessions-index.json"), data, 0644)

	// Rename existing session.
	if err := RenameSession(dir, "sess-1", "My New Title"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}

	sessions, err := readSessionsIndex(dir)
	if err != nil {
		t.Fatalf("readSessionsIndex: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CustomTitle != "My New Title" {
		t.Errorf("CustomTitle = %q, want %q", sessions[0].CustomTitle, "My New Title")
	}
	if sessions[0].Summary != "original" {
		t.Errorf("Summary = %q, want %q (should be preserved)", sessions[0].Summary, "original")
	}
}

func TestRenameSessionNewEntry(t *testing.T) {
	dir := t.TempDir()

	// Create an existing index with one session.
	idx := testIndex(1, rawEntry{SessionID: "sess-1"})
	data, _ := json.MarshalIndent(idx, "", "  ")
	os.WriteFile(filepath.Join(dir, "sessions-index.json"), data, 0644)

	// Rename a session that doesn't exist in the index — should add it.
	if err := RenameSession(dir, "sess-new", "Brand New"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}

	sessions, err := readSessionsIndex(dir)
	if err != nil {
		t.Fatalf("readSessionsIndex: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	byID := make(map[string]SessionInfo)
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	if byID["sess-new"].CustomTitle != "Brand New" {
		t.Errorf("new session CustomTitle = %q, want %q", byID["sess-new"].CustomTitle, "Brand New")
	}
}

func TestRenameSessionNoIndex(t *testing.T) {
	dir := t.TempDir()

	// No sessions-index.json exists — should create one.
	if err := RenameSession(dir, "sess-1", "Created Fresh"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}

	sessions, err := readSessionsIndex(dir)
	if err != nil {
		t.Fatalf("readSessionsIndex: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CustomTitle != "Created Fresh" {
		t.Errorf("CustomTitle = %q, want %q", sessions[0].CustomTitle, "Created Fresh")
	}
}

func TestBackupSession(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	BackupDirOverride = backupDir
	t.Cleanup(func() { BackupDirOverride = "" })

	// Create a fake session JSONL file.
	projDir := filepath.Join(tmpDir, "projects", "-Users-test-proj")
	os.MkdirAll(projDir, 0755)
	jsonlContent := `{"type":"user","message":{"role":"user","content":"hello world"}}` + "\n"
	os.WriteFile(filepath.Join(projDir, "sess-1.jsonl"), []byte(jsonlContent), 0644)

	s := &SessionInfo{
		SessionID: "sess-1",
		Dir:       projDir,
		Status:    StatusResumable,
	}

	// Backup should succeed.
	if err := BackupSession(s); err != nil {
		t.Fatalf("BackupSession: %v", err)
	}

	// Verify backup file exists.
	backupPath := filepath.Join(backupDir, "sess-1.jsonl")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(data) != jsonlContent {
		t.Errorf("backup content = %q, want %q", string(data), jsonlContent)
	}

	// Verify meta file.
	metaData, err := os.ReadFile(filepath.Join(backupDir, "sess-1.meta"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if string(metaData) != projDir {
		t.Errorf("meta = %q, want %q", string(metaData), projDir)
	}
}

func TestBackupSessionNoDir(t *testing.T) {
	s := &SessionInfo{
		SessionID: "sess-1",
		Dir:       "",
	}
	if err := BackupSession(s); err == nil {
		t.Error("expected error for session with no Dir")
	}
}

func TestBackupSessionNoFile(t *testing.T) {
	s := &SessionInfo{
		SessionID: "sess-1",
		Dir:       t.TempDir(),
	}
	if err := BackupSession(s); err == nil {
		t.Error("expected error for missing JSONL file")
	}
}

func TestRestoreSession(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	BackupDirOverride = backupDir
	t.Cleanup(func() { BackupDirOverride = "" })

	destDir := filepath.Join(tmpDir, "projects", "-Users-test-proj")

	// Create backup files.
	os.MkdirAll(backupDir, 0755)
	jsonlContent := `{"type":"user","message":"hello"}` + "\n"
	os.WriteFile(filepath.Join(backupDir, "sess-1.jsonl"), []byte(jsonlContent), 0644)
	os.WriteFile(filepath.Join(backupDir, "sess-1.meta"), []byte(destDir), 0644)

	// Restore should succeed.
	restored, err := RestoreSession("sess-1")
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if !restored {
		t.Error("expected restored=true")
	}

	// Verify restored file.
	data, err := os.ReadFile(filepath.Join(destDir, "sess-1.jsonl"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(data) != jsonlContent {
		t.Errorf("restored content = %q, want %q", string(data), jsonlContent)
	}
}

func TestRestoreSessionNoBackup(t *testing.T) {
	tmpDir := t.TempDir()
	BackupDirOverride = filepath.Join(tmpDir, "backups")
	t.Cleanup(func() { BackupDirOverride = "" })

	restored, err := RestoreSession("nonexistent")
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if restored {
		t.Error("expected restored=false for missing backup")
	}
}

func TestHasBackup(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	BackupDirOverride = backupDir
	t.Cleanup(func() { BackupDirOverride = "" })

	// No backup yet.
	if HasBackup("sess-1") {
		t.Error("expected no backup")
	}

	// Create backup.
	os.MkdirAll(backupDir, 0755)
	os.WriteFile(filepath.Join(backupDir, "sess-1.jsonl"), []byte("data"), 0644)

	if !HasBackup("sess-1") {
		t.Error("expected backup to exist")
	}
}

func TestRefreshBackups(t *testing.T) {
	tmpDir := t.TempDir()
	bkDir := filepath.Join(tmpDir, "backups")
	srcDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(bkDir, 0755)
	os.MkdirAll(srcDir, 0755)
	BackupDirOverride = bkDir
	t.Cleanup(func() { BackupDirOverride = "" })

	sessionID := "test-sess"
	srcFile := filepath.Join(srcDir, sessionID+".jsonl")
	bkFile := filepath.Join(bkDir, sessionID+".jsonl")
	metaFile := filepath.Join(bkDir, sessionID+".meta")

	// Create source and backup with old content.
	os.WriteFile(srcFile, []byte("old"), 0644)
	os.WriteFile(bkFile, []byte("old"), 0644)
	os.WriteFile(metaFile, []byte(srcDir), 0644)

	// Update source with new content and ensure mtime is newer.
	os.WriteFile(srcFile, []byte("new content"), 0644)

	RefreshBackups()

	data, err := os.ReadFile(bkFile)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("backup content = %q, want %q", string(data), "new content")
	}
}

func TestRefreshBackupsNoUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	bkDir := filepath.Join(tmpDir, "backups")
	srcDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(bkDir, 0755)
	os.MkdirAll(srcDir, 0755)
	BackupDirOverride = bkDir
	t.Cleanup(func() { BackupDirOverride = "" })

	sessionID := "test-sess"
	srcFile := filepath.Join(srcDir, sessionID+".jsonl")
	bkFile := filepath.Join(bkDir, sessionID+".jsonl")
	metaFile := filepath.Join(bkDir, sessionID+".meta")

	// Source file doesn't exist — backup should stay unchanged.
	os.WriteFile(bkFile, []byte("backup"), 0644)
	os.WriteFile(metaFile, []byte(srcDir), 0644)

	RefreshBackups() // should not panic or error

	data, _ := os.ReadFile(bkFile)
	if string(data) != "backup" {
		t.Errorf("backup should be unchanged, got %q", string(data))
	}

	// Now create source but with older mtime — backup should still stay.
	os.WriteFile(srcFile, []byte("older"), 0644)
	// Just verify no crash with source present but not newer.
	RefreshBackups()
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	content := "hello world\nline 2\n"
	os.WriteFile(src, []byte(content), 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != content {
		t.Errorf("copied content = %q, want %q", string(data), content)
	}
}

func TestDecodeProjectDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"-Users-foo-bar", "/Users/foo/bar"},
		{"-Users-stefano-Personal-c9s", "/Users/stefano/Personal/c9s"},
		{"-tmp", "/tmp"},
	}
	for _, tt := range tests {
		if got := decodeProjectDirName(tt.input); got != tt.want {
			t.Errorf("decodeProjectDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
