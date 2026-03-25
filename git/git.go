package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nicwestvold/gwt/config"
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

// Remove removes a worktree. If no positional path argument is provided,
// it auto-detects the current worktree directory. Returns the repo dir
// (for cd-back) and the removed worktree path (for cleanup).
func (r *Repo) Remove(args []string) (repoDir, worktreePath string, err error) {
	// Separate flags from positional args to detect if a path was given.
	var flags []string
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}

	if len(positional) == 0 {
		// Auto-detect current worktree.
		var buf, stderr bytes.Buffer
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Stdout = &buf
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", "", fmt.Errorf("not inside a worktree: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}
		worktreePath = strings.TrimSpace(buf.String())
		positional = append(positional, worktreePath)
	} else {
		// Resolve the provided path to absolute.
		worktreePath, err = filepath.Abs(positional[0])
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve path: %w", err)
		}
		positional[0] = worktreePath
	}

	gitArgs := []string{"worktree", "remove"}
	gitArgs = append(gitArgs, flags...)
	gitArgs = append(gitArgs, positional...)

	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("git worktree remove failed: %w", err)
	}

	return r.Dir, worktreePath, nil
}

// CleanEmptyParents removes empty directories walking up from dir,
// stopping before removing stopAt or anything above it.
func CleanEmptyParents(dir, stopAt string) {
	dir = filepath.Clean(dir)
	stopAt = filepath.Clean(stopAt)
	for dir != stopAt && strings.HasPrefix(dir, stopAt+string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// WorktreeBaseDir returns the parent directory for new worktrees.
// For bare repos, this is r.Dir. For regular repos, this is
// ~/.local/share/gwt/worktrees/<owner>/<repo>.
func (r *Repo) WorktreeBaseDir() (string, error) {
	if r.IsBare {
		return r.Dir, nil
	}
	name, err := r.CanonicalName()
	if err != nil {
		return "", fmt.Errorf("failed to determine canonical repo name: %w", err)
	}
	return filepath.Join(config.DataDir(), "worktrees", name), nil
}

// autoRegister adds this repo to the gwt config if not already present.
func (r *Repo) autoRegister() error {
	name, err := r.CanonicalName()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if _, ok := cfg.Lookup(name); ok {
		return nil
	}
	cfg.Register(name, config.RepoEntry{Path: r.Dir})
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func (r *Repo) Add(args []string) (string, error) {
	baseDir, err := r.WorktreeBaseDir()
	if err != nil {
		return "", err
	}

	if !r.IsBare {
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create worktree directory: %w", err)
		}
		if err := r.autoRegister(); err != nil {
			return "", err
		}
	}

	gitArgs, worktreePath, err := buildAddArgs(args, baseDir)
	if err != nil {
		return "", err
	}

	fullArgs := append([]string{"worktree", "add"}, gitArgs...)

	// First attempt: capture stderr to detect "invalid reference"
	var stderrBuf bytes.Buffer
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderrBuf
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderrBuf.String(), "invalid reference:") {
			// Branch not fetched yet — fetch and retry
			fetchCmd := exec.Command("git", "fetch", "origin")
			fetchCmd.Dir = r.Dir
			fetchCmd.Stdout = os.Stdout
			fetchCmd.Stderr = os.Stderr
			if fetchErr := fetchCmd.Run(); fetchErr != nil {
				return "", fmt.Errorf("git fetch failed: %w", fetchErr)
			}
			// Retry with normal stderr passthrough
			retryCmd := exec.Command("git", fullArgs...)
			retryCmd.Dir = r.Dir
			retryCmd.Stdout = os.Stdout
			retryCmd.Stderr = os.Stderr
			retryCmd.Stdin = os.Stdin
			if retryErr := retryCmd.Run(); retryErr != nil {
				return "", fmt.Errorf("git worktree add failed: %w", retryErr)
			}
			return worktreePath, nil
		}
		// Other error — flush captured stderr so user sees it
		os.Stderr.Write(stderrBuf.Bytes())
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
		// flags... worktreePath [start-point]
		gitArgs = append(gitArgs, flags...)
		gitArgs = append(gitArgs, worktreePath)
		gitArgs = append(gitArgs, positional...)
	} else {
		// flags... worktreePath branch
		gitArgs = append(gitArgs, flags...)
		gitArgs = append(gitArgs, worktreePath, branch)
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

