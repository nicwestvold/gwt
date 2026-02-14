package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	repoDir := "/repo"

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
			wantArgs: []string{"fix-thing", "fix/thing"},
			wantPath: "/repo/fix-thing",
		},
		{
			name:     "-b flag",
			args:     []string{"-b", "feat/x"},
			wantArgs: []string{"-b", "feat/x", "feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "-B flag",
			args:     []string{"-B", "feat/x"},
			wantArgs: []string{"-B", "feat/x", "feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "-b with start-point",
			args:     []string{"-b", "feat/x", "origin/main"},
			wantArgs: []string{"-b", "feat/x", "feat-x", "origin/main"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "boolean flag before -b",
			args:     []string{"--track", "-b", "feat/x"},
			wantArgs: []string{"--track", "-b", "feat/x", "feat-x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "--reason value flag",
			args:     []string{"--reason", "lock", "feat/x"},
			wantArgs: []string{"--reason", "lock", "feat-x", "feat/x"},
			wantPath: "/repo/feat-x",
		},
		{
			name:     "simple branch no slash",
			args:     []string{"develop"},
			wantArgs: []string{"develop", "develop"},
			wantPath: "/repo/develop",
		},
		{
			name:     "deeply nested branch",
			args:     []string{"a/b/c/d"},
			wantArgs: []string{"a-b-c-d", "a/b/c/d"},
			wantPath: "/repo/a-b-c-d",
		},
		{
			name:     "--orphan flag",
			args:     []string{"--orphan", "feat/x"},
			wantArgs: []string{"--orphan", "feat/x", "feat-x"},
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
			gotArgs, gotPath, err := buildAddArgs(tt.args, repoDir)
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

