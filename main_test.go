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

		// Uses a non-existent dir — exercises CanonicalName's filepath.Base fallback.
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

func TestStripKeepBranchFlag(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantArgs       []string
		wantKeepBranch bool
	}{
		{
			name:           "no flag",
			args:           []string{"my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: false,
		},
		{
			name:           "long flag",
			args:           []string{"--keep-branch", "my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "short flag",
			args:           []string{"-k", "my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "flag after branch",
			args:           []string{"my-branch", "--keep-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "mixed with git flags",
			args:           []string{"--force", "-k", "my-branch"},
			wantArgs:       []string{"--force", "my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "no args just flag",
			args:           []string{"--keep-branch"},
			wantArgs:       []string{},
			wantKeepBranch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotKeep := stripKeepBranch(tt.args)
			if gotKeep != tt.wantKeepBranch {
				t.Errorf("keepBranch = %v, want %v", gotKeep, tt.wantKeepBranch)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("args = %v (len %d), want %v (len %d)", gotArgs, len(gotArgs), tt.wantArgs, len(tt.wantArgs))
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestShellWrapperContainsCommands(t *testing.T) {
	for _, cmd := range []string{"add", "clone", "rm", "remove", "use"} {
		if !strings.Contains(shellWrapper, `"`+cmd+`"`) {
			t.Errorf("shellWrapper missing command %q", cmd)
		}
	}
}
