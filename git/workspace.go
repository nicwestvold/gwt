package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BranchExists reports whether branch exists locally or as an origin
// remote-tracking ref in the repo at repoDir.
func BranchExists(repoDir, branch string) bool {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", ref)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

// MainBranchRef returns "origin/<mainBranch>" when that remote-tracking ref
// exists, otherwise the local "<mainBranch>". Used as the base for new
// follower branches.
func MainBranchRef(repoDir, mainBranch string) string {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+mainBranch)
	if cmd.Run() == nil {
		return "origin/" + mainBranch
	}
	return mainBranch
}

// AddWorktreeAt runs `git -C repoDir worktree add <gitArgs>`, retrying once
// after `git fetch origin` if the ref was not found.
func AddWorktreeAt(repoDir string, gitArgs []string) error {
	base := []string{"-C", repoDir, "worktree", "add"}
	var stderr bytes.Buffer
	cmd := exec.Command("git", append(append([]string{}, base...), gitArgs...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "invalid reference:") {
			fetch := exec.Command("git", "-C", repoDir, "fetch", "origin")
			fetch.Stdout = os.Stdout
			fetch.Stderr = os.Stderr
			if ferr := fetch.Run(); ferr != nil {
				return fmt.Errorf("git fetch failed: %w", ferr)
			}
			retry := exec.Command("git", append(append([]string{}, base...), gitArgs...)...)
			retry.Stdout = os.Stdout
			retry.Stderr = os.Stderr
			retry.Stdin = os.Stdin
			if rerr := retry.Run(); rerr != nil {
				return fmt.Errorf("git worktree add failed: %w", rerr)
			}
			return nil
		}
		_, _ = os.Stderr.Write(stderr.Bytes())
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// RemoveMemberWorktree removes the worktree at worktreePath belonging to the
// repo at repoDir, then deletes its branch unless keepBranch is set.
func RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) error {
	var branch string
	var buf bytes.Buffer
	bc := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	bc.Stdout = &buf
	if bc.Run() == nil {
		if b := strings.TrimSpace(buf.String()); b != "HEAD" {
			branch = b
		}
	}

	args := []string{"-C", repoDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree remove failed for %s: %w", worktreePath, err)
	}

	if !keepBranch && branch != "" {
		del := exec.Command("git", "-C", repoDir, "branch", "-d", branch)
		del.Stdout = os.Stdout
		del.Stderr = os.Stderr
		if err := del.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not delete branch %q in %s: %v\n", branch, repoDir, err)
		}
	}
	return nil
}

// RunSetup runs a shell command in dir, streaming stdio.
// command is trusted configuration from the user's own config.toml (same trust
// level as a git hook) — NOT untrusted input; no sanitization is needed.
func RunSetup(command, dir string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup command %q (in %s) failed: %w", command, dir, err)
	}
	return nil
}
