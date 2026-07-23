package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nicwestvold/gwt/disk"
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
		Freed:      disk.Result{Bytes: 2202009600}, // ~2.05 GiB
		BranchKept: "feature-x",
		Err:        nil,
	}
	if mr.BranchKept != "feature-x" || mr.Err != nil {
		t.Fatal("fields")
	}
	if mr.Freed.Bytes == 0 {
		t.Fatal("freed")
	}
}
