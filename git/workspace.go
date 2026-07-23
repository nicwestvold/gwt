package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nicwestvold/gwt/disk"
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

// MemberRemoval reports the outcome of removing one workspace member worktree.
type MemberRemoval struct {
	Freed      disk.Result // reclaimed space (zero if removal failed)
	BranchKept string      // branch left undeleted because it was not merged
	Err        error       // worktree-removal error; nil on success
}

// RemoveMemberWorktree removes one member's worktree and, unless keepBranch,
// safely deletes its branch. It returns structured results rather than
// printing, so the caller can aggregate across members.
func RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) MemberRemoval {
	var branch string
	var buf bytes.Buffer
	bc := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	bc.Stdout = &buf
	if bc.Run() == nil {
		if b := strings.TrimSpace(buf.String()); b != "HEAD" {
			branch = b
		}
	}

	freed, _ := disk.Size(worktreePath) // best-effort, before removal

	args := []string{"-C", repoDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return MemberRemoval{Err: fmt.Errorf("git worktree remove failed for %s: %w", worktreePath, err)}
	}

	mr := MemberRemoval{Freed: freed}
	if !keepBranch && branch != "" {
		del := exec.Command("git", "-C", repoDir, "branch", "-d", branch)
		if err := del.Run(); err != nil {
			mr.BranchKept = branch // not fully merged; caller reports it
		}
	}
	return mr
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
