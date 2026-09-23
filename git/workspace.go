package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
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
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// MemberRemoval reports the outcome of removing one workspace member worktree.
type MemberRemoval struct {
	BranchKept string // branch left undeleted because it was not merged
	Err        error  // worktree-removal error; nil on success
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

	mr := MemberRemoval{}
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

// ClearStaleWorktreePath removes a directory left behind at path by a worktree
// git no longer tracks. `git worktree add` refuses any path that already
// exists, so a single surviving file — an ignored build artifact, say — blocks
// that branch name forever.
//
// Two cases are refused instead of deleted, because both mean live files:
// a path git still lists as a worktree, and a path holding a .git entry
// (a checkout whose registration was lost, e.g. after the repo was re-cloned).
func ClearStaleWorktreePath(repoDir, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	infos, err := (&Repo{Dir: repoDir}).ListWorktreesFull()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if samePath(info.Path, path) {
			return fmt.Errorf("%s is already a worktree; remove it with `gwt rm %s`", path, path)
		}
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
		return fmt.Errorf("%s looks like a checkout git has lost track of (it still has a .git entry); inspect it and delete it by hand", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to clear stale worktree dir %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "cleared stale worktree dir: %s\n", path)
	return nil
}

// samePath compares two paths with symlinks resolved, so a worktree registered
// under /var/... matches the same dir reached via /private/var/....
func samePath(a, b string) bool {
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
}
