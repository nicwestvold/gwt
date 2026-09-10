package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepoWithMain creates a git repo at dir with one commit on "main".
func initRepoWithMain(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = testGitEnv()
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}

func TestBranchExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoWithMain(t, dir)

	if !BranchExists(dir, "main") {
		t.Error("BranchExists(main) = false, want true")
	}
	if BranchExists(dir, "nope") {
		t.Error("BranchExists(nope) = true, want false")
	}
}

func TestMainBranchRef(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoWithMain(t, dir)
	// No origin remote -> falls back to the local branch name.
	if got := MainBranchRef(dir, "main"); got != "main" {
		t.Errorf("MainBranchRef = %q, want main", got)
	}
}

func TestAddWorktreeAt(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	wt := filepath.Join(root, "wt", "feat")
	args := []string{"-b", "feat/x", wt, "main"}
	if err := AddWorktreeAt(repo, args); err != nil {
		t.Fatalf("AddWorktreeAt error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README")); err != nil {
		t.Errorf("worktree not created: %v", err)
	}
}

func TestRemoveMemberWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)
	wt := filepath.Join(root, "wt", "feat")
	if err := AddWorktreeAt(repo, []string{"-b", "feat/x", wt, "main"}); err != nil {
		t.Fatal(err)
	}

	if mr := RemoveMemberWorktree(repo, wt, false, false); mr.Err != nil {
		t.Fatalf("RemoveMemberWorktree error: %v", mr.Err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("worktree dir still present")
	}
	// Branch should be deleted (keepBranch=false).
	if BranchExists(repo, "feat/x") {
		t.Error("branch feat/x still exists, want deleted")
	}
}

func TestRemoveMemberWorktreeKeepBranch(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)
	wt := filepath.Join(root, "wt", "feat")
	if err := AddWorktreeAt(repo, []string{"-b", "feat/x", wt, "main"}); err != nil {
		t.Fatal(err)
	}
	if mr := RemoveMemberWorktree(repo, wt, true, false); mr.Err != nil {
		t.Fatal(mr.Err)
	}
	if !BranchExists(repo, "feat/x") {
		t.Error("branch feat/x deleted, want kept")
	}
}

func TestRunSetup(t *testing.T) {
	dir := t.TempDir()
	if err := RunSetup("touch marker", dir); err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Errorf("setup did not run in dir: %v", err)
	}
	if err := RunSetup("exit 3", dir); err == nil {
		t.Error("RunSetup should return error on non-zero exit")
	}
}

func TestMemberRemovalShape(t *testing.T) {
	mr := MemberRemoval{
		BranchKept: "feature-x",
		Err:        nil,
	}
	if mr.BranchKept != "feature-x" || mr.Err != nil {
		t.Fatal("fields")
	}
}

func TestClearStaleWorktreePathRemovesLeftovers(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	// A previous worktree left one ignored build artifact behind. Git no longer
	// tracks the path, but its presence makes `git worktree add` refuse.
	stale := filepath.Join(root, "worktrees", "feat-x")
	if err := os.MkdirAll(filepath.Join(stale, "public", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "public", "build", "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ClearStaleWorktreePath(repo, stale); err != nil {
		t.Fatalf("ClearStaleWorktreePath error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dir still present: %v", err)
	}
}

func TestClearStaleWorktreePathIgnoresMissingPath(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	if err := ClearStaleWorktreePath(repo, filepath.Join(root, "nope")); err != nil {
		t.Errorf("ClearStaleWorktreePath on missing path = %v, want nil", err)
	}
}

func TestClearStaleWorktreePathKeepsRegisteredWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	live := filepath.Join(root, "worktrees", "feat-x")
	if err := AddWorktreeAt(repo, []string{"-b", "feat/x", live}); err != nil {
		t.Fatal(err)
	}

	if err := ClearStaleWorktreePath(repo, live); err == nil {
		t.Error("ClearStaleWorktreePath on a registered worktree = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(live, "README")); err != nil {
		t.Errorf("registered worktree was damaged: %v", err)
	}
}

func TestClearStaleWorktreePathKeepsCheckoutWithGitDir(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	// Registration lost (e.g. the main repo was re-cloned) but the checkout is
	// still there. Deleting it would throw away real work.
	orphan := filepath.Join(root, "worktrees", "feat-x")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, ".git"), []byte("gitdir: /gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ClearStaleWorktreePath(repo, orphan); err == nil {
		t.Error("ClearStaleWorktreePath on a checkout with .git = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(orphan, ".git")); err != nil {
		t.Errorf("orphan checkout was deleted: %v", err)
	}
}
