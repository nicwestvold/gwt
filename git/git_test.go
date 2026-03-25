package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchToDir(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"main", "main"},
		{"fix/thing", "fix-thing"},
		{"a/b/c/d", "a-b-c-d"},
		{"no-slashes", "no-slashes"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := branchToDir(tt.branch)
			if got != tt.want {
				t.Errorf("branchToDir(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

func TestBuildAddArgs(t *testing.T) {
	baseDir := "/repo"

	tests := []struct {
		name     string
		args     []string
		wantArgs []string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "existing branch with slash",
			args:     []string{"fix/thing"},
			wantArgs: []string{"/repo/fix-thing", "fix/thing"},
			wantPath: "/repo/fix-thing",
		},
		{
			name:     "-b flag",
			args:     []string{"-b", "feat/x"},
			wantArgs: []string{"-b", "feat/x", "/repo/feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "-B flag",
			args:     []string{"-B", "feat/x"},
			wantArgs: []string{"-B", "feat/x", "/repo/feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "-b with start-point",
			args:     []string{"-b", "feat/x", "origin/main"},
			wantArgs: []string{"-b", "feat/x", "/repo/feat-x", "origin/main"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "boolean flag before -b",
			args:     []string{"--track", "-b", "feat/x"},
			wantArgs: []string{"--track", "-b", "feat/x", "/repo/feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "--reason value flag",
			args:     []string{"--reason", "lock", "feat/x"},
			wantArgs: []string{"--reason", "lock", "/repo/feat-x", "feat/x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "simple branch no slash",
			args:     []string{"develop"},
			wantArgs: []string{"/repo/develop", "develop"},
			wantPath: "/repo/develop",
		},
		{
			name:     "deeply nested branch",
			args:     []string{"a/b/c/d"},
			wantArgs: []string{"/repo/a-b-c-d", "a/b/c/d"},
			wantPath: "/repo/a-b-c-d",
		},
		{
			name:     "--orphan flag",
			args:     []string{"--orphan", "feat/x"},
			wantArgs: []string{"--orphan", "feat/x", "/repo/feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "-b with no value",
			args:    []string{"-b"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotPath, err := buildAddArgs(tt.args, baseDir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildAddArgs(%v) expected error, got nil", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildAddArgs(%v) unexpected error: %v", tt.args, err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("buildAddArgs(%v) path = %q, want %q", tt.args, gotPath, tt.wantPath)
			}
			if fmt.Sprintf("%v", gotArgs) != fmt.Sprintf("%v", tt.wantArgs) {
				t.Errorf("buildAddArgs(%v) args = %v, want %v", tt.args, gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil error", nil, 0},
		{"non-ExitError", errors.New("something"), 1},
		{"ExitError with code 2", &exec.ExitError{ProcessState: exitState(t, 2)}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitCode(tt.err)
			if got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestRepoName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo.git", "repo"},
		{"https://github.com/user/repo", "repo"},
		{"git@github.com:user/repo.git", "repo"},
		{"git@github.com:repo.git", "repo"},
		{"/path/to/repo.git", "repo"},
		{"https://github.com/user/repo/", "repo"},
		{"https://github.com/user/repo.git/", "repo"},
		{"repo", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := repoName(tt.url)
			if got != tt.want {
				t.Errorf("repoName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestWriteCdFile(t *testing.T) {
	t.Run("writes path when env var is set", func(t *testing.T) {
		cdFile := filepath.Join(t.TempDir(), "cd-target")
		t.Setenv("GWT_CD_FILE", cdFile)

		WriteCdFile("/some/path")

		data, err := os.ReadFile(cdFile)
		if err != nil {
			t.Fatalf("failed to read cd file: %v", err)
		}
		if string(data) != "/some/path" {
			t.Errorf("cd file = %q, want %q", string(data), "/some/path")
		}
	})

	t.Run("no-op when env var is unset", func(t *testing.T) {
		t.Setenv("GWT_CD_FILE", "")

		// Should not panic or create any file
		WriteCdFile("/some/path")
	})

	t.Run("no-op when path is empty", func(t *testing.T) {
		cdFile := filepath.Join(t.TempDir(), "cd-target")
		t.Setenv("GWT_CD_FILE", cdFile)

		WriteCdFile("")

		if _, err := os.Stat(cdFile); err == nil {
			t.Error("cd file should not exist when path is empty")
		}
	})
}

func TestNewRepo(t *testing.T) {
	// Skip if git is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	// Build a gwt-style bare repo structure:
	//   tmp/project/.bare/   (bare repo)
	//   tmp/project/.git     (file: "gitdir: ./.bare\n")
	//   tmp/project/main/    (worktree)
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")

	// Create a source repo with one commit, then clone it bare.
	sourceDir := filepath.Join(tmp, "source")
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = gitEnv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
	}

	run("git", "init", sourceDir)
	run("git", "-C", sourceDir, "commit", "--allow-empty", "-m", "init")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	run("git", "clone", "--bare", sourceDir, filepath.Join(project, ".bare"))

	// Write .git file pointing to .bare (gwt convention)
	if err := os.WriteFile(filepath.Join(project, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve symlinks so comparisons work on macOS (/var -> /private/var).
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	// Add a worktree at project/main
	worktreeDir := filepath.Join(project, "main")
	run("git", "-C", project, "worktree", "add", worktreeDir)

	t.Run("from project root", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		if err := os.Chdir(project); err != nil {
			t.Fatal(err)
		}

		repo, err := NewRepo()
		if err != nil {
			t.Fatalf("NewRepo() error: %v", err)
		}
		if repo.Dir != project {
			t.Errorf("Dir = %q, want %q", repo.Dir, project)
		}
		if !repo.IsBare {
			t.Error("IsBare = false, want true")
		}
	})

	t.Run("from inside worktree", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		if err := os.Chdir(worktreeDir); err != nil {
			t.Fatal(err)
		}

		repo, err := NewRepo()
		if err != nil {
			t.Fatalf("NewRepo() error: %v", err)
		}
		if repo.Dir != project {
			t.Errorf("Dir = %q, want %q", repo.Dir, project)
		}
		if !repo.IsBare {
			t.Error("IsBare = false, want true")
		}
	})

	t.Run("from nested worktree subdirectory", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		nestedDir := filepath.Join(worktreeDir, "sub", "deep")
		if err := os.MkdirAll(nestedDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(nestedDir); err != nil {
			t.Fatal(err)
		}

		repo, err := NewRepo()
		if err != nil {
			t.Fatalf("NewRepo() error: %v", err)
		}
		if repo.Dir != project {
			t.Errorf("Dir = %q, want %q", repo.Dir, project)
		}
		if !repo.IsBare {
			t.Error("IsBare = false, want true")
		}
	})
}

func TestNewRepoNormalRepo(t *testing.T) {
	// Skip if git is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "normalrepo")

	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = gitEnv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
	}

	run("git", "init", repoDir)
	run("git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")

	// Resolve symlinks so comparisons work on macOS (/var -> /private/var).
	repoDir, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("from repo root", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		if err := os.Chdir(repoDir); err != nil {
			t.Fatal(err)
		}

		repo, err := NewRepo()
		if err != nil {
			t.Fatalf("NewRepo() error: %v", err)
		}
		if repo.Dir != repoDir {
			t.Errorf("Dir = %q, want %q", repo.Dir, repoDir)
		}
		if repo.IsBare {
			t.Error("IsBare = true, want false")
		}
	})

	t.Run("from subdirectory", func(t *testing.T) {
		origDir, _ := os.Getwd()
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		subDir := filepath.Join(repoDir, "sub", "deep")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(subDir); err != nil {
			t.Fatal(err)
		}

		repo, err := NewRepo()
		if err != nil {
			t.Fatalf("NewRepo() error: %v", err)
		}
		if repo.Dir != repoDir {
			t.Errorf("Dir = %q, want %q", repo.Dir, repoDir)
		}
		if repo.IsBare {
			t.Error("IsBare = true, want false")
		}
	})
}

func TestAdd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	tmp := t.TempDir()
	sourceDir := filepath.Join(tmp, "source")
	project := filepath.Join(tmp, "project")

	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = gitEnv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
	}

	// Create source repo with initial commit on main
	run("git", "init", "-b", "main", sourceDir)
	run("git", "-C", sourceDir, "commit", "--allow-empty", "-m", "init")

	// Clone bare into project/.bare and write .git file
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	run("git", "clone", "--bare", sourceDir, filepath.Join(project, ".bare"))
	if err := os.WriteFile(filepath.Join(project, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve symlinks for macOS (/var -> /private/var)
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	sourceDir, err = filepath.EvalSymlinks(sourceDir)
	if err != nil {
		t.Fatal(err)
	}

	repo := &Repo{Dir: project, IsBare: true}

	// Configure fetch refspec and do initial fetch so origin/main is known
	if err := repo.ConfigureFetch(); err != nil {
		t.Fatalf("ConfigureFetch: %v", err)
	}
	run("git", "-C", project, "fetch", "origin")

	t.Run("success on first attempt", func(t *testing.T) {
		got, err := repo.Add([]string{"main"}, project)
		if err != nil {
			t.Fatalf("Add([\"main\"]) unexpected error: %v", err)
		}
		want := filepath.Join(project, "main")
		if got != want {
			t.Errorf("Add([\"main\"]) = %q, want %q", got, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("worktree directory does not exist: %v", err)
		}
	})

	t.Run("auto-fetch on invalid reference", func(t *testing.T) {
		// Create a new branch on source that the bare repo doesn't know about yet
		run("git", "-C", sourceDir, "checkout", "-b", "new-feature")
		run("git", "-C", sourceDir, "commit", "--allow-empty", "-m", "new feature")

		got, err := repo.Add([]string{"new-feature"}, project)
		if err != nil {
			t.Fatalf("Add([\"new-feature\"]) unexpected error: %v", err)
		}
		want := filepath.Join(project, "new-feature")
		if got != want {
			t.Errorf("Add([\"new-feature\"]) = %q, want %q", got, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("worktree directory does not exist: %v", err)
		}
	})

	t.Run("fails cleanly for nonexistent branch", func(t *testing.T) {
		_, err := repo.Add([]string{"totally-fake"}, project)
		if err == nil {
			t.Fatal("Add([\"totally-fake\"]) expected error, got nil")
		}
	})
}

func TestRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	tmp := t.TempDir()
	sourceDir := filepath.Join(tmp, "source")
	project := filepath.Join(tmp, "project")

	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = gitEnv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
	}

	// Create source repo with initial commit on main
	run("git", "init", "-b", "main", sourceDir)
	run("git", "-C", sourceDir, "commit", "--allow-empty", "-m", "init")

	// Clone bare into project/.bare and write .git file
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	run("git", "clone", "--bare", sourceDir, filepath.Join(project, ".bare"))
	if err := os.WriteFile(filepath.Join(project, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve symlinks for macOS (/var -> /private/var)
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	repo := &Repo{Dir: project, IsBare: true}
	if err := repo.ConfigureFetch(); err != nil {
		t.Fatalf("ConfigureFetch: %v", err)
	}
	run("git", "-C", project, "fetch", "origin")

	t.Run("remove with explicit path", func(t *testing.T) {
		// Add a worktree, then remove it by path.
		wtDir := filepath.Join(project, "feat-remove-test")
		run("git", "-C", project, "worktree", "add", "-b", "remove-test", wtDir)

		repoDir, removedPath, err := repo.Remove([]string{wtDir})
		if err != nil {
			t.Fatalf("Remove() error: %v", err)
		}
		if repoDir != project {
			t.Errorf("repoDir = %q, want %q", repoDir, project)
		}
		if removedPath != wtDir {
			t.Errorf("worktreePath = %q, want %q", removedPath, wtDir)
		}
		if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
			t.Error("worktree directory should not exist after removal")
		}
	})

	t.Run("remove with force flag", func(t *testing.T) {
		wtDir := filepath.Join(project, "feat-force-test")
		run("git", "-C", project, "worktree", "add", "-b", "force-test", wtDir)

		// Create an untracked file to make it dirty
		_ = os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("dirty"), 0o644)

		repoDir, _, err := repo.Remove([]string{"--force", wtDir})
		if err != nil {
			t.Fatalf("Remove(--force) error: %v", err)
		}
		if repoDir != project {
			t.Errorf("repoDir = %q, want %q", repoDir, project)
		}
	})

	t.Run("auto-detect current worktree", func(t *testing.T) {
		wtDir := filepath.Join(project, "feat-autodetect")
		run("git", "-C", project, "worktree", "add", "-b", "autodetect", wtDir)

		// Change into the worktree so auto-detect finds it.
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(wtDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		repoDir, removedPath, err := repo.Remove([]string{})
		if err != nil {
			t.Fatalf("Remove() auto-detect error: %v", err)
		}
		if repoDir != project {
			t.Errorf("repoDir = %q, want %q", repoDir, project)
		}
		if removedPath != wtDir {
			t.Errorf("worktreePath = %q, want %q", removedPath, wtDir)
		}
	})

	t.Run("refuses to remove main working tree", func(t *testing.T) {
		_, _, err := repo.Remove([]string{project})
		if err == nil {
			t.Fatal("Remove(project) should error when targeting main working tree")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error = %q, want to contain 'refusing to remove'", err.Error())
		}
	})
}

func TestCleanEmptyParents(t *testing.T) {
	t.Run("removes empty dirs up to stopAt", func(t *testing.T) {
		tmp := t.TempDir()
		stopAt := filepath.Join(tmp, "worktrees")
		deep := filepath.Join(stopAt, "owner", "repo", "feat-x")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}

		CleanEmptyParents(deep, stopAt)

		// deep, repo, and owner should all be gone (empty)
		if _, err := os.Stat(filepath.Join(stopAt, "owner")); !os.IsNotExist(err) {
			t.Error("owner dir should be removed")
		}
		// stopAt itself should remain
		if _, err := os.Stat(stopAt); err != nil {
			t.Error("stopAt dir should still exist")
		}
	})

	t.Run("stops at non-empty dir", func(t *testing.T) {
		tmp := t.TempDir()
		stopAt := filepath.Join(tmp, "worktrees")
		repoDir := filepath.Join(stopAt, "owner", "repo")
		wt1 := filepath.Join(repoDir, "feat-a")
		wt2 := filepath.Join(repoDir, "feat-b")
		if err := os.MkdirAll(wt1, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(wt2, 0o755); err != nil {
			t.Fatal(err)
		}

		// Remove wt1 — wt2 still exists so repo/ and owner/ should stay
		CleanEmptyParents(wt1, stopAt)

		if _, err := os.Stat(repoDir); err != nil {
			t.Error("repo dir should still exist (has wt2)")
		}
		if _, err := os.Stat(wt2); err != nil {
			t.Error("wt2 should still exist")
		}
	})

	t.Run("no-op when dir equals stopAt", func(t *testing.T) {
		tmp := t.TempDir()
		stopAt := filepath.Join(tmp, "worktrees")
		if err := os.MkdirAll(stopAt, 0o755); err != nil {
			t.Fatal(err)
		}

		CleanEmptyParents(stopAt, stopAt)

		if _, err := os.Stat(stopAt); err != nil {
			t.Error("stopAt should still exist when dir == stopAt")
		}
	})

	t.Run("handles already-deleted dir gracefully", func(t *testing.T) {
		tmp := t.TempDir()
		stopAt := filepath.Join(tmp, "worktrees")
		parent := filepath.Join(stopAt, "owner", "repo")
		deleted := filepath.Join(parent, "feat-x")

		// Create parent but not the leaf — simulates post-removal state.
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}

		// Should not panic; parent is empty so it gets cleaned.
		CleanEmptyParents(deleted, stopAt)
	})

	t.Run("cleans from parent of deleted dir", func(t *testing.T) {
		// This tests the recommended usage pattern: start from filepath.Dir(worktreePath).
		tmp := t.TempDir()
		stopAt := filepath.Join(tmp, "worktrees")
		parent := filepath.Join(stopAt, "owner", "repo")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}

		CleanEmptyParents(parent, stopAt)

		if _, err := os.Stat(filepath.Join(stopAt, "owner")); !os.IsNotExist(err) {
			t.Error("owner dir should be removed")
		}
		if _, err := os.Stat(stopAt); err != nil {
			t.Error("stopAt should still exist")
		}
	})
}

// exitState creates an *os.ProcessState with the given exit code by running a
// subprocess that exits with that code.
func exitState(t *testing.T, code int) *os.ProcessState {
	t.Helper()
	cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	return exitErr.ProcessState
}
