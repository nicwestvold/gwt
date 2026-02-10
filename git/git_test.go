package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestExtractWorktreePath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bare path", []string{"path"}, "path"},
		{"-b flag", []string{"-b", "branch", "path"}, "path"},
		{"-B flag", []string{"-B", "branch", "path"}, "path"},
		{"--reason flag", []string{"--reason", "text", "path"}, "path"},
		{"boolean + value flag combo", []string{"--track", "-b", "branch", "path"}, "path"},
		{"-- separator", []string{"--", "path"}, "path"},
		{"flags then -- separator", []string{"-b", "branch", "--", "path"}, "path"},
		{"--orphan flag", []string{"--orphan", "branch", "path"}, "path"},
		{"empty args", []string{}, ""},
		{"no path just flags", []string{"-b", "branch"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractWorktreePath(tt.args)
			if got != tt.want {
				t.Errorf("extractWorktreePath(%v) = %q, want %q", tt.args, got, tt.want)
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

func TestCloneCreatesGitignore(t *testing.T) {
	// Create a temporary bare repo to clone from (avoids network).
	upstream := t.TempDir()
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run(upstream, "git", "init", "--bare")

	dest := t.TempDir()
	target := dest + "/myrepo"

	dir, err := Clone(upstream, target)
	if err != nil {
		t.Fatalf("Clone() error: %v", err)
	}

	gitignorePath := dir + "/.gitignore"
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("expected .gitignore at %s, got error: %v", gitignorePath, err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore content = %q, want %q", string(data), "*\n")
	}
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

