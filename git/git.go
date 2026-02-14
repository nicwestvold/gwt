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
	var buf, stderr bytes.Buffer

	cmd := exec.Command("git", "rev-parse", "--is-bare-repository")
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("not in a git repository: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	isBare := strings.TrimSpace(buf.String()) == "true"
	buf.Reset()
	stderr.Reset()

	var dir string
	if isBare {
		cmd = exec.Command("git", "rev-parse", "--git-dir")
	} else {
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	}
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to detect repo directory: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	dir = strings.TrimSpace(buf.String())

	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo path: %w", err)
	}

	// Detect bare-repo worktree structure (e.g. gwt clone):
	// Resolve the common dir and check if it's bare to find the project root.
	// This works from any depth within a worktree, not just the worktree root.
	buf.Reset()
	stderr.Reset()
	cmd = exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to detect git common dir: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	commonDir, absErr := filepath.Abs(strings.TrimSpace(buf.String()))
	if absErr != nil {
		return nil, fmt.Errorf("failed to resolve git common dir: %w", absErr)
	}

	buf.Reset()
	stderr.Reset()
	cmd = exec.Command("git", "-C", commonDir, "rev-parse", "--is-bare-repository")
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to check bare status of common dir: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if strings.TrimSpace(buf.String()) == "true" {
		dir = filepath.Dir(commonDir)
		isBare = true
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
	gitArgs, worktreePath, err := buildAddArgs(args, r.Dir)
	if err != nil {
		return "", err
	}

	fullArgs := append([]string{"worktree", "add"}, gitArgs...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %w", err)
	}
	return worktreePath, nil
}

func branchToDir(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// buildAddArgs parses user args and returns transformed args for git worktree add
// plus the derived worktree path.
func buildAddArgs(args []string, repoDir string) (gitArgs []string, worktreePath string, err error) {
	if len(args) == 0 {
		return nil, "", fmt.Errorf("requires a branch name")
	}

	valueFlags := map[string]bool{
		"-b": true, "-B": true, "--orphan": true, "--reason": true,
	}
	branchFlags := map[string]bool{
		"-b": true, "-B": true, "--orphan": true,
	}

	var flags []string
	var positional []string
	var branchFlag string
	var branchValue string
	pastFlags := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if pastFlags {
			positional = append(positional, arg)
			continue
		}

		if arg == "--" {
			pastFlags = true
			continue
		}

		// Handle --flag=value syntax
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if branchFlags[parts[0]] {
				branchFlag = parts[0]
				branchValue = parts[1]
			}
			flags = append(flags, arg)
			continue
		}

		if valueFlags[arg] {
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("%s requires a value", arg)
			}
			if branchFlags[arg] {
				branchFlag = arg
				branchValue = args[i+1]
			}
			flags = append(flags, arg, args[i+1])
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			continue
		}

		positional = append(positional, arg)
	}

	var branch string
	if branchFlag != "" {
		branch = branchValue
		if len(positional) > 1 {
			return nil, "", fmt.Errorf("too many positional arguments")
		}
	} else {
		if len(positional) == 0 {
			return nil, "", fmt.Errorf("requires a branch name")
		}
		if len(positional) > 1 {
			return nil, "", fmt.Errorf("too many positional arguments")
		}
		branch = positional[0]
	}

	dir := branchToDir(branch)
	worktreePath = filepath.Join(repoDir, dir)

	if branchFlag != "" {
		// flags... dir [start-point]
		gitArgs = append(gitArgs, flags...)
		gitArgs = append(gitArgs, dir)
		gitArgs = append(gitArgs, positional...)
	} else {
		// flags... dir branch
		gitArgs = append(gitArgs, flags...)
		gitArgs = append(gitArgs, dir, branch)
	}

	return gitArgs, worktreePath, nil
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
			if rmErr := os.RemoveAll(absDir); rmErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to clean up %s: %v\n", absDir, rmErr)
			}
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
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func WriteCdFile(path string) {
	if cdFile := os.Getenv("GWT_CD_FILE"); cdFile != "" && path != "" {
		os.WriteFile(cdFile, []byte(path), 0o644)
	}
}

func (r *Repo) HooksDir() (string, error) {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to find git common dir: %w (%s)", err, strings.TrimSpace(stderr.String()))
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
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set remote.origin.fetch: %w", err)
	}
	return nil
}

