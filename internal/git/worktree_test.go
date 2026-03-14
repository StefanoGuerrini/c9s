package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAvailable(t *testing.T) {
	// Git should be available in CI/dev environments.
	if !Available() {
		t.Skip("git not installed")
	}
}

func TestListWorktrees_NotARepo(t *testing.T) {
	dir := t.TempDir()
	wts := ListWorktrees(dir)
	if len(wts) != 0 {
		t.Errorf("expected 0 worktrees for non-repo, got %d", len(wts))
	}
}

func TestListWorktrees_SingleWorktree(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}

	dir := realpath(t, t.TempDir())
	initRepo(t, dir)

	wts := ListWorktrees(dir)
	if len(wts) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(wts))
	}
	if wts[0].Path != dir {
		t.Errorf("path = %q, want %q", wts[0].Path, dir)
	}
	if !wts[0].IsMain {
		t.Error("first worktree should be marked as main")
	}
}

func TestListWorktrees_MultipleWorktrees(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}

	dir := realpath(t, t.TempDir())
	initRepo(t, dir)

	// Add a worktree.
	wtPath := filepath.Join(realpath(t, t.TempDir()), "feature")
	run(t, dir, "git", "worktree", "add", "-b", "feature-x", wtPath)

	wts := ListWorktrees(dir)
	if len(wts) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(wts))
	}
	if !wts[0].IsMain {
		t.Error("first worktree should be main")
	}
	if wts[1].IsMain {
		t.Error("second worktree should not be main")
	}
	if wts[1].Branch != "feature-x" {
		t.Errorf("branch = %q, want feature-x", wts[1].Branch)
	}
	if wts[1].Path != wtPath {
		t.Errorf("path = %q, want %q", wts[1].Path, wtPath)
	}
}

func TestHasWorktrees(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}

	dir := realpath(t, t.TempDir())
	initRepo(t, dir)

	if HasWorktrees(dir) {
		t.Error("single worktree should return false")
	}

	wtPath := filepath.Join(realpath(t, t.TempDir()), "wt")
	run(t, dir, "git", "worktree", "add", "-b", "wt-branch", wtPath)

	if !HasWorktrees(dir) {
		t.Error("two worktrees should return true")
	}
}

func TestCreateWorktree(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}

	dir := filepath.Join(realpath(t, t.TempDir()), "myrepo")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, dir)

	wtPath, err := CreateWorktree(dir, "new-feature")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Check the worktree was created.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree dir not created: %v", err)
	}

	// Expected path is sibling: <dir>-new-feature.
	expected := dir + "-new-feature"
	if wtPath != expected {
		t.Errorf("path = %q, want %q", wtPath, expected)
	}

	// Verify it shows up in the worktree list.
	wts := ListWorktrees(dir)
	if len(wts) != 2 {
		t.Fatalf("expected 2 worktrees after create, got %d", len(wts))
	}
}

func TestParsePorcelain(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project-feature
HEAD def456
branch refs/heads/feature-x

`
	wts := parsePorcelain(input)
	if len(wts) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(wts))
	}
	if wts[0].Path != "/home/user/project" {
		t.Errorf("wt[0].Path = %q", wts[0].Path)
	}
	if wts[0].Branch != "main" {
		t.Errorf("wt[0].Branch = %q", wts[0].Branch)
	}
	if !wts[0].IsMain {
		t.Error("wt[0] should be main")
	}
	if wts[1].Branch != "feature-x" {
		t.Errorf("wt[1].Branch = %q", wts[1].Branch)
	}
	if wts[1].IsMain {
		t.Error("wt[1] should not be main")
	}
}

func TestParsePorcelain_Detached(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project-detached
HEAD def456
detached

`
	wts := parsePorcelain(input)
	if len(wts) != 2 {
		t.Fatalf("expected 2, got %d", len(wts))
	}
	if wts[1].Branch != "(detached)" {
		t.Errorf("detached branch = %q", wts[1].Branch)
	}
}

func TestParsePorcelain_Empty(t *testing.T) {
	wts := parsePorcelain("")
	if len(wts) != 0 {
		t.Errorf("expected 0, got %d", len(wts))
	}
}

// initRepo creates a minimal git repo with one commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	// Create a file and commit so branches can diverge.
	f := filepath.Join(dir, "README")
	if err := os.WriteFile(f, []byte("init"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// realpath resolves symlinks (needed on macOS where /var → /private/var).
func realpath(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
