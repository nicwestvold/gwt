package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct {
	Dir    string
	IsBare bool
}

func NewRepo() (*Repo, error) {
	var buf bytes.Buffer

	cmd := exec.Command("git", "rev-parse", "--is-bare-repository")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, errors.New("not in a git repository")
	}
	isBare := strings.TrimSpace(buf.String()) == "true"
	buf.Reset()

	var dir string
	if isBare {
		cmd = exec.Command("git", "rev-parse", "--git-dir")
	} else {
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	}
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to detect repo directory: %w", err)
	}
	dir = strings.TrimSpace(buf.String())

	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	return &Repo{Dir: dir, IsBare: isBare}, nil
}

func (r *Repo) Passthrough(args []string) error {
	gitArgs := append([]string{"worktree"}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (r *Repo) Add(args []string) (string, error) {
	gitArgs := append([]string{"worktree", "add"}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	path := extractWorktreePath(args)
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Dir, path)
	}
	return path, nil
}

// extractWorktreePath finds the <path> positional argument from git worktree add args.
func extractWorktreePath(args []string) string {
	valueFlags := map[string]bool{
		"-b": true, "-B": true, "--reason": true,
	}

	skipNext := false
	pastFlags := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			pastFlags = true
			continue
		}
		if !pastFlags {
			if valueFlags[arg] {
				skipNext = true
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
		}
		return arg
	}
	return ""
}

func ExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func (r *Repo) ConfigureFetch() error {
	var buf bytes.Buffer
	cmd := exec.Command("git", "config", "remote.origin.fetch")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	err := cmd.Run()

	expected := "+refs/heads/*:refs/remotes/origin/*"
	if err == nil && strings.TrimSpace(buf.String()) == expected {
		return nil
	}

	cmd = exec.Command("git", "config", "remote.origin.fetch", expected)
	cmd.Dir = r.Dir
	return cmd.Run()
}

func (r *Repo) WorktreePathForBranch(branch string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	var currentPath string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		}
		if strings.HasPrefix(line, "branch refs/heads/") {
			b := strings.TrimPrefix(line, "branch refs/heads/")
			if b == branch {
				return currentPath, nil
			}
		}
	}

	return "", fmt.Errorf("no worktree found for branch %q", branch)
}

func CopyFileToWorktree(srcDir, dstDir, filename string) error {
	srcPath := filepath.Join(srcDir, filename)
	dstPath := filepath.Join(dstDir, filename)

	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("failed to resolve source path: %w", err)
	}
	if !strings.HasPrefix(srcAbs, srcDir+string(filepath.Separator)) && srcAbs != srcDir {
		return fmt.Errorf("path %q escapes source directory", filename)
	}

	dstAbs, err := filepath.Abs(dstPath)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}
	if !strings.HasPrefix(dstAbs, dstDir+string(filepath.Separator)) && dstAbs != dstDir {
		return fmt.Errorf("path %q escapes destination directory", filename)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
