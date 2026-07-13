package git

import (
	"os/exec"
	"strconv"
	"strings"
)

// Worktree represents a git worktree along with cheap status signals so the
// dashboard can render dirty/ahead/behind glyphs without a second round trip.
type Worktree struct {
	Path   string // absolute path
	Branch string // branch name (or "(detached)")
	IsMain bool   // first worktree = main
	Dirty  bool   // true if `git status --porcelain` has any tracked changes
	Ahead  int    // commits ahead of upstream (0 if no upstream)
	Behind int    // commits behind upstream (0 if no upstream)
}

// Available returns true if git is installed.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ListWorktrees returns the list of git worktrees for the repository at dir
// with cheap dirty/ahead/behind annotations. Returns nil (no error) if git is
// unavailable, dir is not a repo, or any other issue.
func ListWorktrees(dir string) []Worktree {
	cmd := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	wts := parsePorcelain(string(out))
	for i := range wts {
		if wts[i].Path == "" {
			continue
		}
		wts[i].Dirty = isDirty(wts[i].Path)
		wts[i].Ahead, wts[i].Behind = aheadBehind(wts[i].Path)
	}
	return wts
}

// HasWorktrees returns true if the repo at dir has 2 or more worktrees.
func HasWorktrees(dir string) bool {
	return len(ListWorktrees(dir)) >= 2
}

// CreateWorktree creates a new worktree with a new branch. Returns the
// absolute path of the created worktree. The new worktree is a sibling of the
// repo root, named <repo>-<branch>.
func CreateWorktree(repoDir, branch string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--show-toplevel")
	topOut, err := cmd.Output()
	if err != nil {
		return "", err
	}
	topLevel := strings.TrimSpace(string(topOut))

	wtPath := topLevel + "-" + branch
	createCmd := exec.Command("git", "-C", repoDir, "worktree", "add", "-b", branch, wtPath)
	if err := createCmd.Run(); err != nil {
		return "", err
	}
	return wtPath, nil
}

// RemoveWorktree deletes a worktree at the given path. If force is true, dirty
// worktrees are removed too (equivalent to `git worktree remove --force`).
func RemoveWorktree(repoDir, wtPath string, force bool) error {
	args := []string{"-C", repoDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wtPath)
	return exec.Command("git", args...).Run()
}

// isDirty returns true if the working tree at path has any tracked changes.
// Untracked files are ignored — they are too noisy to be a useful signal.
func isDirty(path string) bool {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// aheadBehind returns (ahead, behind) counts vs the upstream of the worktree's
// HEAD. Returns (0, 0) when no upstream is configured or git errors out — the
// caller treats both as "nothing to surface".
func aheadBehind(path string) (int, int) {
	out, err := exec.Command("git", "-C", path, "rev-list", "--left-right", "--count", "@{u}...HEAD").Output()
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return 0, 0
	}
	behind, _ := strconv.Atoi(parts[0])
	ahead, _ := strconv.Atoi(parts[1])
	return ahead, behind
}

// parsePorcelain parses `git worktree list --porcelain` output.
func parsePorcelain(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree
	first := true

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "HEAD "):
			// ignore HEAD line
		case strings.HasPrefix(line, "bare"):
			// bare worktree
		case strings.HasPrefix(line, "detached"):
			current.Branch = "(detached)"
		case line == "":
			if current.Path != "" {
				if first {
					current.IsMain = true
					first = false
				}
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	if current.Path != "" {
		if first {
			current.IsMain = true
		}
		worktrees = append(worktrees, current)
	}
	return worktrees
}
