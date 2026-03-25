package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicwestvold/gwt/git"
)

func TestWorktreeBaseDir(t *testing.T) {
	t.Run("bare repo returns repo dir", func(t *testing.T) {
		repo := &git.Repo{Dir: "/some/bare/repo", IsBare: true}
		baseDir, name, err := worktreeBaseDir(repo)
		if err != nil {
			t.Fatalf("worktreeBaseDir() error: %v", err)
		}
		if baseDir != "/some/bare/repo" {
			t.Errorf("baseDir = %q, want %q", baseDir, "/some/bare/repo")
		}
		// Name is derived from dir basename when no origin remote.
		if name != "repo" {
			t.Errorf("name = %q, want %q", name, "repo")
		}
	})

	t.Run("non-bare repo returns XDG data path", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)

		repo := &git.Repo{Dir: "/some/normal/repo", IsBare: false}
		baseDir, name, err := worktreeBaseDir(repo)
		if err != nil {
			t.Fatalf("worktreeBaseDir() error: %v", err)
		}
		want := filepath.Join(tmp, "gwt", "worktrees", "repo")
		if baseDir != want {
			t.Errorf("baseDir = %q, want %q", baseDir, want)
		}
		if name != "repo" {
			t.Errorf("name = %q, want %q", name, "repo")
		}
	})
}

func TestShellWrapperContainsCommands(t *testing.T) {
	for _, cmd := range []string{"add", "clone", "rm", "remove"} {
		if !strings.Contains(shellWrapper, `"`+cmd+`"`) {
			t.Errorf("shellWrapper missing command %q", cmd)
		}
	}
}
