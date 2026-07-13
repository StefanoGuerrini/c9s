package main

import (
	"testing"

	"github.com/stefanoguerrini/c9s/internal/claude"
	"github.com/stefanoguerrini/c9s/internal/config"
	"github.com/stefanoguerrini/c9s/internal/git"
)

// stubModel builds a minimal model with a pre-populated worktree cache so the
// items() branches can be tested without shelling out to git. The worktree
// list is inserted with an already-matching fingerprint so lookupWorktrees()
// returns the cached value verbatim.
func stubModel(t *testing.T, sessions []claude.SessionInfo, wtByProject map[string][]git.Worktree) model {
	t.Helper()
	cfg = config.Default()
	cfg.Worktrees = "on"
	cache := make(map[string]worktreeCacheEntry, len(wtByProject))
	for dir, wts := range wtByProject {
		fp := worktreeFingerprint(dir) // may be "" for tempdirs without .git; that's fine
		cache[dir] = worktreeCacheEntry{worktrees: wts, fingerprint: fp}
	}
	return model{
		sessions:           sessions,
		groupBy:            groupProject,
		collapsedWorktrees: make(map[string]bool),
		worktreeCache:      cache,
		replacedSessions:   make(map[string]bool),
		managedWindows:     make(map[string]managedWindow),
		height:             40,
		width:              120,
	}
}

func TestItems_HierarchicalGroupsSessionsUnderWorktrees(t *testing.T) {
	repo := "/tmp/proj"
	wtFeat := "/tmp/proj-feat"

	sessions := []claude.SessionInfo{
		{SessionID: "s1", ProjectPath: repo, CustomTitle: "PR Review"},
		{SessionID: "s2", ProjectPath: repo, CustomTitle: "Classify"},
	}
	wts := []git.Worktree{
		{Path: repo, Branch: "main", IsMain: true},
		{Path: wtFeat, Branch: "feat"},
	}
	m := stubModel(t, sessions, map[string][]git.Worktree{repo: wts})

	items := m.items()

	// Expected shape:
	//   0: header  (proj (2 sessions · 2 worktrees))
	//   1: worktree main
	//   2: session s1 (indent 2)
	//   3: session s2 (indent 2)
	//   4: worktree feat
	//   5: emptyWorktree placeholder for feat
	if len(items) != 6 {
		t.Fatalf("expected 6 items, got %d\nitems=%+v", len(items), items)
	}
	if !items[0].isHeader {
		t.Errorf("row 0 should be header, got %+v", items[0])
	}
	if !items[1].isWorktreeRow || items[1].worktree.Branch != "main" {
		t.Errorf("row 1 should be main worktree, got %+v", items[1])
	}
	if items[2].session.SessionID != "s1" || items[2].indent != 2 {
		t.Errorf("row 2 should be s1 nested at indent 2, got %+v", items[2])
	}
	if items[3].session.SessionID != "s2" || items[3].parentWorktree != repo {
		t.Errorf("row 3 should be s2 under main worktree, got %+v", items[3])
	}
	if !items[4].isWorktreeRow || items[4].worktree.Branch != "feat" {
		t.Errorf("row 4 should be feat worktree, got %+v", items[4])
	}
	if !items[5].isEmptyWorktree || items[5].parentWorktree != wtFeat {
		t.Errorf("row 5 should be empty-worktree placeholder for feat, got %+v", items[5])
	}
}

func TestItems_SingleWorktreeFallsBackToFlat(t *testing.T) {
	repo := "/tmp/proj"
	sessions := []claude.SessionInfo{
		{SessionID: "s1", ProjectPath: repo},
	}
	m := stubModel(t, sessions, map[string][]git.Worktree{
		repo: {{Path: repo, Branch: "main", IsMain: true}},
	})
	items := m.items()

	// Flat under a project header: header + 1 session at indent 1.
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].isHeader {
		t.Error("row 0 should be header")
	}
	if items[1].indent != 1 || items[1].session.SessionID != "s1" {
		t.Errorf("row 1 should be s1 at indent 1, got %+v", items[1])
	}
}

func TestItems_CollapsedWorktreeHidesSessions(t *testing.T) {
	repo := "/tmp/proj"
	wtFeat := "/tmp/proj-feat"
	sessions := []claude.SessionInfo{{SessionID: "s1", ProjectPath: repo}}
	wts := []git.Worktree{
		{Path: repo, Branch: "main", IsMain: true},
		{Path: wtFeat, Branch: "feat"},
	}
	m := stubModel(t, sessions, map[string][]git.Worktree{repo: wts})
	m.collapsedWorktrees[repo] = true // main collapsed → its s1 hidden

	items := m.items()

	// header, main (collapsed), feat, empty-placeholder for feat = 4 rows.
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d\nitems=%+v", len(items), items)
	}
	// No item may be a session row after the collapse.
	for i, it := range items {
		if !it.isHeader && !it.isWorktreeRow && !it.isEmptyWorktree {
			t.Errorf("row %d should not be a session row when main is collapsed, got %+v", i, it)
		}
	}
}

func TestItems_WorktreesOffKeepsFlatUnderProjectHeader(t *testing.T) {
	repo := "/tmp/proj"
	sessions := []claude.SessionInfo{
		{SessionID: "s1", ProjectPath: repo},
		{SessionID: "s2", ProjectPath: repo},
	}
	m := stubModel(t, sessions, map[string][]git.Worktree{
		repo: {
			{Path: repo, Branch: "main", IsMain: true},
			{Path: "/tmp/proj-feat", Branch: "feat"},
		},
	})
	cfg.Worktrees = "off"
	t.Cleanup(func() { cfg.Worktrees = "on" })

	items := m.items()

	// Header + 2 session rows, no worktree rows regardless of how many exist.
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	for _, it := range items[1:] {
		if it.isWorktreeRow || it.isEmptyWorktree {
			t.Errorf("no worktree rows should appear when worktrees=off, got %+v", it)
		}
	}
}

func TestItems_GroupByStatusHasNoWorktreeRows(t *testing.T) {
	repo := "/tmp/proj"
	sessions := []claude.SessionInfo{{SessionID: "s1", ProjectPath: repo, Status: claude.StatusResumable}}
	m := stubModel(t, sessions, map[string][]git.Worktree{
		repo: {
			{Path: repo, Branch: "main", IsMain: true},
			{Path: "/tmp/proj-feat", Branch: "feat"},
		},
	})
	m.groupBy = groupStatus

	for _, it := range m.items() {
		if it.isWorktreeRow || it.isEmptyWorktree {
			t.Errorf("groupBy=status must not emit worktree rows, got %+v", it)
		}
	}
}
