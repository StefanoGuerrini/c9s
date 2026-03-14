package git

import (
	"os/exec"
	"strings"
)

// Worktree represents a git worktree.
type Worktree struct {
	Path   string // absolute path
	Branch string // branch name
	IsMain bool   // first worktree = main
}

// Available returns true if git is installed.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ListWorktrees returns the list of git worktrees for the repository at dir.
// Returns nil (no error) if git is unavailable, dir is not a repo, or any other issue.
func ListWorktrees(dir string) []Worktree {
	cmd := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parsePorcelain(string(out))
}

// HasWorktrees returns true if the repo at dir has 2 or more worktrees.
func HasWorktrees(dir string) bool {
	return len(ListWorktrees(dir)) >= 2
}

// CreateWorktree creates a new worktree with a new branch.
// Returns the absolute path of the created worktree.
func CreateWorktree(repoDir, branch string) (string, error) {
	// Determine worktree path: sibling directory named <repo>-<branch>
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
	// Handle last entry if no trailing newline.
	if current.Path != "" {
		if first {
			current.IsMain = true
		}
		worktrees = append(worktrees, current)
	}
	return worktrees
}
