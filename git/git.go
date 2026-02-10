package git

import (
	"bytes"
	"errors"
	"fmt"
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

	// If .git in cwd is a file (gitdir pointer to a bare repo),
	// use cwd as Dir so worktrees are created as siblings.
	if isBare {
		if info, statErr := os.Lstat(".git"); statErr == nil && !info.IsDir() {
			if cwd, cwdErr := filepath.Abs("."); cwdErr == nil {
				dir = cwd
			}
		}
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

func repoName(url string) string {
	name := strings.TrimRight(url, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".git")
	return name
}

func Clone(url, dir string) (_ string, retErr error) {
	if dir == "" {
		dir = repoName(url)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	if err := os.Mkdir(absDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	defer func() {
		if retErr != nil {
			os.RemoveAll(absDir)
		}
	}()

	cloneCmd := exec.Command("git", "clone", "--bare", url, ".bare")
	cloneCmd.Dir = absDir
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return "", fmt.Errorf("git clone --bare failed: %w", err)
	}

	gitFile := filepath.Join(absDir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		return "", fmt.Errorf("failed to write .git file: %w", err)
	}

	repo := &Repo{Dir: absDir, IsBare: true}
	if err := repo.ConfigureFetch(); err != nil {
		return "", fmt.Errorf("failed to configure fetch: %w", err)
	}

	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = absDir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return "", fmt.Errorf("git fetch failed: %w", err)
	}

	return absDir, nil
}

func ExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func (r *Repo) HooksDir() (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to find git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(buf.String())
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(r.Dir, commonDir)
	}
	return filepath.Join(commonDir, "hooks"), nil
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

