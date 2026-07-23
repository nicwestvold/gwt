package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicwestvold/gwt/disk"
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
	} else if !isBare && filepath.Dir(commonDir) != dir {
		// Linked worktree of a regular repo — resolve to main repo root.
		dir = filepath.Dir(commonDir)
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

// RemoveResult reports the outcome of removing a single worktree.
type RemoveResult struct {
	RepoDir      string
	WorktreePath string
	Branch       string      // "" if detached
	Freed        disk.Result // on-disk space reclaimed
}

// Remove removes a worktree. If no positional path argument is provided,
// it auto-detects the current worktree directory. Returns the repo dir
// (for cd-back) and the removed worktree path (for cleanup).
func (r *Repo) Remove(args []string, keepBranch bool) (RemoveResult, error) {
	// Separate flags from positional args, respecting "--" separator.
	var flags []string
	var positional []string
	pastSeparator := false
	for _, a := range args {
		if !pastSeparator && a == "--" {
			pastSeparator = true
			flags = append(flags, a)
			continue
		}
		if !pastSeparator && strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}

	var worktreePath string
	if len(positional) == 0 {
		// Auto-detect current worktree from the user's working directory.
		var buf, stderr bytes.Buffer
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Stdout = &buf
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return RemoveResult{}, fmt.Errorf("not inside a worktree: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}
		worktreePath = strings.TrimSpace(buf.String())
	} else {
		// Resolve the provided path to absolute.
		var err error
		worktreePath, err = filepath.Abs(positional[0])
		if err != nil {
			return RemoveResult{}, fmt.Errorf("failed to resolve path: %w", err)
		}
	}

	// Resolve symlinks so the guard comparison works on systems
	// where paths diverge (e.g. macOS /var -> /private/var).
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		worktreePath = resolved
	}

	// Guard against removing the main working tree.
	resolvedDir := r.Dir
	if resolved, err := filepath.EvalSymlinks(r.Dir); err == nil {
		resolvedDir = resolved
	}
	if filepath.Clean(worktreePath) == filepath.Clean(resolvedDir) {
		return RemoveResult{}, fmt.Errorf("refusing to remove the main working tree: %s", worktreePath)
	}

	// Detect the branch checked out in the worktree before removal.
	var branch string
	{
		var buf bytes.Buffer
		bc := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		bc.Dir = worktreePath
		bc.Stdout = &buf
		if bc.Run() == nil {
			b := strings.TrimSpace(buf.String())
			if b != "HEAD" { // skip detached HEAD
				branch = b
			}
		}
	}

	freed, _ := disk.Size(worktreePath) // best-effort; never blocks removal

	gitArgs := []string{"worktree", "remove"}
	gitArgs = append(gitArgs, flags...)
	gitArgs = append(gitArgs, worktreePath)

	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = r.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return RemoveResult{}, fmt.Errorf("git worktree remove failed: %w", err)
	}

	// Best-effort branch deletion (non-force).
	if !keepBranch && branch != "" {
		delCmd := exec.Command("git", "branch", "-d", branch)
		delCmd.Dir = r.Dir
		delCmd.Stdout = os.Stdout
		delCmd.Stderr = os.Stderr
		if err := delCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not delete branch %q: %v\n", branch, err)
		}
	}

	return RemoveResult{
		RepoDir:      r.Dir,
		WorktreePath: worktreePath,
		Branch:       branch,
		Freed:        freed,
	}, nil
}

// CleanEmptyParents removes empty directories walking up from dir,
// stopping before removing stopAt or anything above it.
func CleanEmptyParents(dir, stopAt string) {
	dir = filepath.Clean(dir)
	stopAt = filepath.Clean(stopAt)
	prefix := stopAt + string(filepath.Separator)
	// Best-effort cleanup; errors are intentionally ignored.
	for dir != stopAt && strings.HasPrefix(dir, prefix) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		_ = os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// Add creates a worktree. baseDir is the parent directory where the
// worktree subdirectory will be created.
func (r *Repo) Add(args []string, baseDir string) (string, error) {
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
		_, _ = os.Stderr.Write(stderrBuf.Bytes())
		return "", fmt.Errorf("git worktree add failed: %w", err)
	}
	return worktreePath, nil
}

// BranchToDir converts a branch name into a filesystem-safe directory name.
func BranchToDir(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// AddArgs is the parsed form of `git worktree add` arguments.
type AddArgs struct {
	Flags      []string // all flags, including any branch flag + its value
	BranchFlag string   // "-b", "-B", "--orphan", or "" when checking out an existing branch
	Branch     string   // the branch name
	Extra      []string // trailing positionals (e.g. a start-point) when BranchFlag is set
}

// ParseAddArgs parses user-supplied `gwt add` arguments into AddArgs.
func ParseAddArgs(args []string) (AddArgs, error) {
	if len(args) == 0 {
		return AddArgs{}, fmt.Errorf("requires a branch name")
	}

	valueFlags := map[string]bool{"-b": true, "-B": true, "--orphan": true, "--reason": true}
	branchFlags := map[string]bool{"-b": true, "-B": true, "--orphan": true}

	var flags []string
	var positional []string
	var branchFlag, branchValue string
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
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if branchFlags[parts[0]] {
				branchFlag, branchValue = parts[0], parts[1]
			}
			flags = append(flags, arg)
			continue
		}
		// Support "-b=value" shorthand.
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if branchFlags[parts[0]] {
				branchFlag, branchValue = parts[0], parts[1]
				flags = append(flags, arg)
				continue
			}
		}
		if valueFlags[arg] {
			if i+1 >= len(args) {
				return AddArgs{}, fmt.Errorf("%s requires a value", arg)
			}
			if branchFlags[arg] {
				branchFlag, branchValue = arg, args[i+1]
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

	a := AddArgs{Flags: flags, BranchFlag: branchFlag}
	if branchFlag != "" {
		a.Branch = branchValue
		if len(positional) > 1 {
			return AddArgs{}, fmt.Errorf("too many positional arguments")
		}
		a.Extra = positional
	} else {
		if len(positional) == 0 {
			return AddArgs{}, fmt.Errorf("requires a branch name")
		}
		if len(positional) > 1 {
			return AddArgs{}, fmt.Errorf("too many positional arguments")
		}
		a.Branch = positional[0]
	}
	return a, nil
}

// Build produces the `git worktree add` argument list for a given worktree path.
func (a AddArgs) Build(worktreePath string) []string {
	out := append([]string{}, a.Flags...)
	if a.BranchFlag != "" {
		out = append(out, worktreePath)
		out = append(out, a.Extra...)
	} else {
		out = append(out, worktreePath, a.Branch)
	}
	return out
}

// buildAddArgs parses user args and returns transformed args for git worktree add
// plus the derived worktree path (worktree dir derived from the branch name).
func buildAddArgs(args []string, baseDir string) (gitArgs []string, worktreePath string, err error) {
	a, err := ParseAddArgs(args)
	if err != nil {
		return nil, "", err
	}
	worktreePath = filepath.Join(baseDir, BranchToDir(a.Branch))
	return a.Build(worktreePath), worktreePath, nil
}

func repoName(url string) string {
	name := ParseCanonicalName(url)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	if name != "" {
		return name
	}
	// Fallback for empty/unparseable URLs.
	n := strings.TrimRight(url, "/")
	n = strings.TrimSuffix(n, ".git")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		return n[i+1:]
	}
	return n
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
		_ = os.WriteFile(cdFile, []byte(path), 0o644)
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

// WorktreeEntry represents a single worktree from `git worktree list --porcelain`.
type WorktreeEntry struct {
	Path   string
	Branch string // short name, e.g. "main" or "feature/foo" (refs/heads/ stripped)
}

// ListWorktrees runs `git worktree list --porcelain` and returns parsed entries.
// Only entries with a branch (not detached HEAD or bare) are included.
func (r *Repo) ListWorktrees() ([]WorktreeEntry, error) {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseWorktreeList(buf.String()), nil
}

func parseWorktreeList(output string) []WorktreeEntry {
	var entries []WorktreeEntry
	var current WorktreeEntry
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			current = WorktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "" && current.Path != "" {
			if current.Branch != "" {
				entries = append(entries, current)
			}
			current = WorktreeEntry{}
		}
	}
	if current.Path != "" && current.Branch != "" {
		entries = append(entries, current)
	}
	return entries
}

// decorateLine prepends the active/inactive marker and, on a color terminal,
// wraps the active line in green. Shared by the plain and sized list renders.
func decorateLine(content string, active, color bool) string {
	const green = "\033[32m"
	const reset = "\033[0m"
	switch {
	case active && color:
		return green + "* " + content + reset
	case active:
		return "* " + content
	default:
		return "  " + content
	}
}

// renderWorktreeList annotates plain `git worktree list` output, marking the
// line for activePath with a "* " prefix (green when color is true) and
// indenting all other lines with two spaces to keep paths aligned. A line is
// considered active when its path field exactly matches activePath; the
// trailing-space check prevents a shared prefix (e.g. /a/b vs /a/bc) from
// matching the wrong worktree.
func renderWorktreeList(plain, activePath string, color bool) string {
	plain = strings.TrimRight(plain, "\n")
	if plain == "" {
		return ""
	}

	var b strings.Builder
	for _, line := range strings.Split(plain, "\n") {
		active := activePath != "" &&
			(line == activePath || strings.HasPrefix(line, activePath+" "))
		b.WriteString(decorateLine(line, active, color) + "\n")
	}
	return b.String()
}

// renderWorktreeTable renders the worktree list as an aligned table with the
// active worktree marked. Columns are path | [size] | sha | annotation. When
// sizes is nil the size column and total row are omitted (bare `ls`);
// otherwise sizes[i] corresponds to infos[i] and a size column plus a total
// row are included (`ls -s`).
func renderWorktreeTable(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string {
	withSize := sizes != nil

	pathW := 0
	if withSize {
		pathW = len("total")
	}
	shaW := 0
	sizeW := 0
	sizeStrs := make([]string, len(infos))
	var totalBytes int64
	anyApprox := false
	for i, in := range infos {
		if len(in.Path) > pathW {
			pathW = len(in.Path)
		}
		if len(in.SHA) > shaW {
			shaW = len(in.SHA)
		}
		if withSize {
			sizeStrs[i] = disk.Format(sizes[i])
			if len(sizeStrs[i]) > sizeW {
				sizeW = len(sizeStrs[i])
			}
			totalBytes += sizes[i].Bytes
			if sizes[i].Skipped > 0 {
				anyApprox = true
			}
		}
	}

	totalStr := ""
	if withSize {
		totalStr = disk.FormatApprox(totalBytes, anyApprox)
		if len(totalStr) > sizeW {
			sizeW = len(totalStr)
		}
	}

	var b strings.Builder
	for i, in := range infos {
		var content string
		if withSize {
			content = fmt.Sprintf("%-*s  %*s  %-*s  %s",
				pathW, in.Path, sizeW, sizeStrs[i], shaW, in.SHA, in.Annotation())
		} else {
			content = fmt.Sprintf("%-*s  %-*s  %s",
				pathW, in.Path, shaW, in.SHA, in.Annotation())
		}
		active := activePath != "" && in.Path == activePath
		b.WriteString(decorateLine(strings.TrimRight(content, " "), active, color) + "\n")
	}
	if withSize {
		totalContent := fmt.Sprintf("%-*s  %*s", pathW, "total", sizeW, totalStr)
		b.WriteString(decorateLine(strings.TrimRight(totalContent, " "), false, color) + "\n")
	}
	return b.String()
}

// PrintWorktreeList writes `git worktree list` to stdout with the worktree
// containing the caller's current directory marked. Color is enabled only on a
// terminal and when NO_COLOR is unset.
func (r *Repo) PrintWorktreeList() error {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "worktree", "list")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree list failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	fmt.Print(renderWorktreeList(buf.String(), currentWorktreeTop(), shouldColor()))
	return nil
}

// PrintSizedWorktreeList prints the worktree list with an on-disk size column.
// Sizes are computed concurrently across worktrees.
func (r *Repo) PrintSizedWorktreeList() error {
	infos, err := r.ListWorktreesFull()
	if err != nil {
		return err
	}
	sizes := make([]disk.Result, len(infos))
	var wg sync.WaitGroup
	for i := range infos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, _ := disk.Size(infos[i].Path) // best-effort; errors → zero size
			sizes[i] = res
		}(i)
	}
	wg.Wait()
	fmt.Print(renderWorktreeTable(infos, sizes, currentWorktreeTop(), shouldColor()))
	return nil
}

// currentWorktreeTop returns the top-level path of the worktree containing the
// process's current directory, or "" when not inside a worktree.
func currentWorktreeTop() string {
	var buf bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Stdout = &buf
	if cmd.Run() != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

// shouldColor reports whether colored output should be emitted: true only when
// stdout is a terminal and NO_COLOR is unset.
func shouldColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// FindWorktreeByBranch returns the path of the worktree checked out on the given branch.
func (r *Repo) FindWorktreeByBranch(branch string) (string, bool, error) {
	entries, err := r.ListWorktrees()
	if err != nil {
		return "", false, err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return e.Path, true, nil
		}
	}
	return "", false, nil
}

// shaAbbrevLen is the abbreviated-SHA width shown in the sized worktree list.
const shaAbbrevLen = 11

// WorktreeInfo is a complete parse of one `git worktree list --porcelain`
// entry — unlike WorktreeEntry, it retains detached/bare/locked rows.
type WorktreeInfo struct {
	Path     string
	SHA      string // abbreviated HEAD sha ("" for a bare repo)
	Branch   string // short name; "" if detached or bare
	Detached bool
	Bare     bool
	Locked   bool
	Prunable bool
}

// Annotation renders the trailing column git shows for this worktree.
func (w WorktreeInfo) Annotation() string {
	var a string
	switch {
	case w.Bare:
		a = "(bare)"
	case w.Detached:
		a = "(detached HEAD)"
	default:
		a = "[" + w.Branch + "]"
	}
	if w.Locked {
		a += " locked"
	}
	if w.Prunable {
		a += " prunable"
	}
	return a
}

func parseWorktreeListFull(output string) []WorktreeInfo {
	var out []WorktreeInfo
	var cur WorktreeInfo
	started := false
	flush := func() {
		if started && cur.Path != "" {
			out = append(out, cur)
		}
		cur = WorktreeInfo{}
		started = false
	}
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
			started = true
		case strings.HasPrefix(line, "HEAD "):
			sha := strings.TrimPrefix(line, "HEAD ")
			if len(sha) > shaAbbrevLen {
				sha = sha[:shaAbbrevLen]
			}
			cur.SHA = sha
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		case line == "":
			flush()
		}
	}
	flush()
	return out
}

// ListWorktreesFull returns every worktree (including detached/bare) with its
// abbreviated sha and lock/prune flags.
func (r *Repo) ListWorktreesFull() ([]WorktreeInfo, error) {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseWorktreeListFull(buf.String()), nil
}
