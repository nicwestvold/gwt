package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/nicwestvold/gwt/config"
	"github.com/nicwestvold/gwt/detect"
	"github.com/nicwestvold/gwt/disk"
	"github.com/nicwestvold/gwt/git"
	"github.com/nicwestvold/gwt/hook"
	"github.com/spf13/cobra"
)

var version = "dev"

var aliases = map[string]string{
	"ls": "list",
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of gwt",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(resolveVersion())
	},
}

var rootCmd = &cobra.Command{
	Use:   "gwt [command]",
	Short: "A convenience wrapper around git worktree",
	Long: `gwt is a thin wrapper around git worktree.

The following git worktree commands are passed through directly.
For example, 'gwt list' runs 'git worktree list'.

Pass-through commands:
  list    List worktrees
  lock    Lock a worktree
  move    Move a worktree
  prune   Prune stale worktree information
  repair  Repair worktree administrative files
  unlock  Unlock a worktree

Aliases:
  ls    list

Additional commands:
  clone      Clone a repo into a bare-repo worktree structure
  init       Generate a post-checkout hook for worktree setup
  shell-init Print shell integration for auto-cd

Enhanced commands:
  add        Create a worktree (setup handled by post-checkout hook)
  list/ls    List worktrees, marking the active one with '*' (green on a TTY)
  remove/rm  Remove a worktree by path or branch name (auto-cd back)
  use        Switch to an existing worktree by branch name`,
}

type hookOptions struct {
	mainBranch     string
	copyFiles      []string
	versionManager string
	packageManager string
	force          bool
}

func repoBasePath(repo *git.Repo, mainBranch string) string {
	if repo.IsBare {
		return filepath.Join(repo.Dir, mainBranch)
	}
	return repo.Dir
}

func setupHook(repo *git.Repo, opts hookOptions) error {
	if repo.IsBare {
		if err := repo.ConfigureFetch(); err != nil {
			return fmt.Errorf("failed to configure fetch: %w", err)
		}
	}

	basePath := repoBasePath(repo, opts.mainBranch)

	hooksDir, err := repo.HooksDir()
	if err != nil {
		return err
	}

	data := hook.HookData{
		BasePath:       basePath,
		CopyFiles:      opts.copyFiles,
		VersionManager: opts.versionManager,
		PackageManager: opts.packageManager,
	}

	if err := hook.Install(hooksDir, data, opts.force); err != nil {
		return err
	}

	fmt.Printf("post-checkout hook installed: %s/post-checkout\n", hooksDir)
	return nil
}

const noHookMsg = "no version or package manager detected — no hook generated (use -c to copy files, or -v/-p to set them manually)"
const fixItMsg = "If auto-detection got it wrong, re-run with explicit flags, e.g. gwt init -f -v asdf -p yarn"

// fileSourceFor returns a detection source for the repo's main branch content:
// the working directory when it exists, otherwise the main branch git tree
// (for a bare repo whose main worktree is not checked out yet).
func fileSourceFor(repo *git.Repo, mainBranch string) detect.FileSource {
	basePath := repoBasePath(repo, mainBranch)
	if fi, err := os.Stat(basePath); err == nil && fi.IsDir() {
		return detect.DirSource{Root: basePath}
	}
	return detect.GitSource{RepoDir: repo.Dir, Ref: git.MainBranchRef(repo.Dir, mainBranch)}
}

// mergeDetected fills the version/package manager dimensions of opts that were
// not set explicitly (vmSet/pmSet) from res, returning the updated opts, the
// user-facing messages to print, and whether anything was auto-detected.
func mergeDetected(opts hookOptions, res detect.Result, vmSet, pmSet bool) (hookOptions, []string, bool) {
	var msgs []string
	detected := false
	if !vmSet && res.VersionManager != "" {
		opts.versionManager = res.VersionManager
		msgs = append(msgs, fmt.Sprintf("auto-detected %s, adding to hook", res.VersionManager))
		detected = true
	}
	if !pmSet && res.PackageManager != "" {
		opts.packageManager = res.PackageManager
		msgs = append(msgs, fmt.Sprintf("auto-detected %s, adding to hook", res.PackageManager))
		detected = true
	}
	return opts, msgs, detected
}

// detectAndMerge runs detection for the repo and merges the result into opts,
// printing the auto-detected messages. Returns the updated opts and whether
// anything was auto-detected.
func detectAndMerge(repo *git.Repo, opts hookOptions, vmSet, pmSet bool) (hookOptions, bool) {
	res := detect.Detect(fileSourceFor(repo, opts.mainBranch), exec.LookPath)
	opts, msgs, detected := mergeDetected(opts, res, vmSet, pmSet)
	for _, m := range msgs {
		fmt.Println(m)
	}
	return opts, detected
}

// hookHasWork reports whether a generated hook would do anything.
func hookHasWork(opts hookOptions) bool {
	return len(opts.copyFiles) > 0 || opts.versionManager != "" || opts.packageManager != ""
}

// worktreeBaseDir returns the parent directory for new worktrees and the
// canonical repo name. For bare repos, the base dir is repo.Dir. For regular
// repos, it is ~/.local/share/gwt/worktrees/<owner>/<repo>.
func worktreeBaseDir(repo *git.Repo) (baseDir, canonicalName string, err error) {
	name, err := repo.CanonicalName()
	if err != nil {
		return "", "", fmt.Errorf("failed to determine canonical repo name: %w", err)
	}
	if repo.IsBare {
		return repo.Dir, name, nil
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dataDir, "worktrees", name), name, nil
}

var cloneCmd = &cobra.Command{
	Use:   "clone <repository> [<directory>]",
	Short: "Clone a repo into a bare-repo worktree structure",
	Long: `Clones a repository as a bare repo inside a .bare/ directory,
creates a .git file pointing to it, configures fetch, and fetches all branches.

If init flags (--main, --copy, --version-manager, --package-manager, --with-hook)
are provided, a post-checkout hook is also created. Otherwise, run 'gwt init'
afterward to generate the hook.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		var dir string
		if len(args) > 1 {
			dir = args[1]
		}

		absDir, err := git.Clone(url, dir)
		if err != nil {
			return err
		}

		mainBranch, _ := cmd.Flags().GetString("main")
		copyFiles, _ := cmd.Flags().GetStringSlice("copy")
		versionManager, _ := cmd.Flags().GetString("version-manager")
		packageManager, _ := cmd.Flags().GetString("package-manager")

		if versionManager != "" && !validVersionManagers[versionManager] {
			return fmt.Errorf("invalid version manager %q: must be one of: asdf, mise", versionManager)
		}
		if packageManager != "" && !validPackageManagers[packageManager] {
			return fmt.Errorf("invalid package manager %q: must be one of: pnpm, npm, yarn", packageManager)
		}

		repo := &git.Repo{Dir: absDir, IsBare: true}
		opts := hookOptions{
			mainBranch:     mainBranch,
			copyFiles:      copyFiles,
			versionManager: versionManager,
			packageManager: packageManager,
		}

		initFlags := []string{"main", "copy", "version-manager", "package-manager", "with-hook"}
		wantHook := false
		for _, f := range initFlags {
			if cmd.Flags().Changed(f) {
				wantHook = true
				break
			}
		}

		detected := false
		if wantHook {
			opts, detected = detectAndMerge(repo, opts, cmd.Flags().Changed("version-manager"), cmd.Flags().Changed("package-manager"))
		}

		if regErr := registerRepo(repo, opts); regErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", regErr)
		}

		hookCreated := false
		if wantHook {
			if hookHasWork(opts) {
				if err := setupHook(repo, opts); err != nil {
					fmt.Fprintf(os.Stderr, "Clone succeeded, but hook setup failed: %v\n", err)
					fmt.Fprintf(os.Stderr, "You can retry with: cd %s && gwt init\n", absDir)
					return err
				}
				hookCreated = true
				if detected {
					fmt.Println(fixItMsg)
				}
			} else {
				fmt.Println(noHookMsg)
			}
		}

		git.WriteCdFile(absDir)

		fmt.Printf("Cloned into %s\n", absDir)
		fmt.Println("Next steps:")
		fmt.Println("  cd", absDir)
		if !hookCreated {
			fmt.Println("  gwt init       # generate post-checkout hook")
		}
		fmt.Println("  gwt add <branch>")
		return nil
	},
}

var addCmd = &cobra.Command{
	Use:   "add [flags] <branch>",
	Short: "Create a worktree",
	Long: `Create a new worktree for the given branch.

The worktree directory is derived from the branch name by replacing
'/' with '-'. For example, 'fix/login-bug' becomes 'fix-login-bug'.

Use -b to create a new branch:
  gwt add -b feat/new-feature
  gwt add -b feat/new-feature origin/main   # with start-point

Check out an existing branch:
  gwt add fix/login-bug

File copying and project setup are handled by the post-checkout hook
installed via 'gwt init'.`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				return cmd.Help()
			}
		}

		repo, err := git.NewRepo()
		if err != nil {
			return err
		}

		// Workspace fan-out: if this repo is a workspace member, create
		// worktrees for all members instead of the single-repo flow.
		if canonical, nameErr := repo.CanonicalName(); nameErr == nil {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if wsName, ws, ok := cfg.WorkspaceForRepo(canonical); ok {
					cd, addErr := runWorkspaceAdd(cfg, wsName, ws, args)
					if addErr != nil {
						return addErr
					}
					git.WriteCdFile(cd)
					return nil
				}
			}
		}

		baseDir, canonicalName, err := worktreeBaseDir(repo)
		if err != nil {
			return err
		}

		if !repo.IsBare {
			if err := os.MkdirAll(baseDir, 0o755); err != nil {
				return fmt.Errorf("failed to create worktree directory: %w", err)
			}
			if err := ensureRegistered(repo, canonicalName); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", err)
			}
		}

		path, err := repo.Add(args, baseDir)
		if err == nil && path != "" {
			git.WriteCdFile(path)
		}
		return err
	},
}

// partitionRemoveArgs separates flags from positional arguments for the remove
// command. It sets force true when a flag is -f, --force, or starts with
// --force=. A -- separator causes all subsequent args to be treated as
// positionals, mirroring the style used by buildAddArgs and Remove.
func partitionRemoveArgs(args []string) (force bool, positionals []string) {
	pastSeparator := false
	for _, a := range args {
		if pastSeparator {
			positionals = append(positionals, a)
			continue
		}
		if a == "--" {
			pastSeparator = true
			continue
		}
		if a == "-f" || a == "--force" || strings.HasPrefix(a, "--force=") {
			force = true
		} else if strings.HasPrefix(a, "-") {
			// other flag; ignore for force detection
		} else {
			positionals = append(positionals, a)
		}
	}
	return force, positionals
}

func stripKeepBranch(args []string) (cleaned []string, keepBranch bool) {
	for _, a := range args {
		if a == "--keep-branch" || a == "-k" {
			keepBranch = true
		} else {
			cleaned = append(cleaned, a)
		}
	}
	if cleaned == nil {
		cleaned = []string{}
	}
	return cleaned, keepBranch
}

var removeCmd = &cobra.Command{
	Use:     "remove [flags] [<worktree>]",
	Aliases: []string{"rm"},
	Short:   "Remove a worktree",
	Long: `Remove a worktree. If no path is given, removes the current worktree.

Accepts a branch name or a path. If the argument matches a worktree branch,
it resolves to that worktree's path.

In a workspace, the whole branch group (all member worktrees) is removed;
the argument may be a branch name, a member worktree path, or the group
directory.

After removal, the shell wrapper (from 'gwt shell-init') will cd back
to the repository root.

Flags:
  -k, --keep-branch   Keep the branch after removing the worktree

Supports all git worktree remove flags (e.g., --force).`,
	DisableFlagParsing: true,
	ValidArgsFunction:  completeWorktreeBranches,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				return cmd.Help()
			}
		}

		args, keepBranch := stripKeepBranch(args)

		repo, err := git.NewRepo()
		if err != nil {
			return err
		}

		// Workspace teardown: if this repo is a workspace member, remove the
		// whole branch group, identified by the argument (branch name or
		// path) or, absent one, by the current worktree.
		if canonical, nameErr := repo.CanonicalName(); nameErr == nil {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if wsName, ws, ok := cfg.WorkspaceForRepo(canonical); ok {
					force, positionals := partitionRemoveArgs(args)
					if len(positionals) > 1 {
						return fmt.Errorf("expected at most one worktree, got %d: %v", len(positionals), positionals)
					}
					members, mErr := cfg.ResolveMembers(ws)
					if mErr != nil {
						return mErr
					}
					root, rErr := ws.ResolveWorktreeRoot(wsName)
					if rErr != nil {
						return rErr
					}
					var group string
					if len(positionals) == 1 {
						group, err = resolveWorkspaceGroup(root, members, positionals[0])
					} else {
						var buf bytes.Buffer
						tl := exec.Command("git", "rev-parse", "--show-toplevel")
						tl.Stdout = &buf
						if tl.Run() != nil {
							return fmt.Errorf("not inside a git worktree — cd into a member worktree or pass a branch name/path")
						}
						current := strings.TrimSpace(buf.String())
						group, err = validateGroup(root, "the current worktree", filepath.Dir(current))
					}
					if err != nil {
						return err
					}
					cd, rmErr := runWorkspaceRemove(cfg, wsName, ws, group, keepBranch, force)
					if cd != "" {
						git.WriteCdFile(cd)
					}
					if rmErr != nil {
						return rmErr
					}
					return nil
				}
			}
		}

		// Resolve branch names to worktree paths.
		resolvedArgs := make([]string, len(args))
		copy(resolvedArgs, args)
		for i, a := range resolvedArgs {
			if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "/") || strings.HasPrefix(a, ".") {
				continue
			}
			if path, found, err := repo.FindWorktreeByBranch(a); err == nil && found {
				resolvedArgs[i] = path
			}
		}

		res, err := repo.Remove(resolvedArgs, keepBranch)
		if err != nil {
			return err
		}

		// Clean up empty parent dirs for centralized worktrees.
		dataDir, dataErr := config.DataDir()
		if dataErr == nil {
			worktreeRoot := filepath.Join(dataDir, "worktrees")
			if strings.HasPrefix(res.WorktreePath, worktreeRoot+string(filepath.Separator)) {
				git.CleanEmptyParents(filepath.Dir(res.WorktreePath), worktreeRoot)
			}
		}

		name := res.Branch
		if name == "" {
			name = filepath.Base(res.WorktreePath)
		}
		if res.Freed.Bytes > 0 {
			fmt.Printf("removed worktree %s — freed %s\n", name, disk.Format(res.Freed))
		} else {
			fmt.Printf("removed worktree %s\n", name)
		}

		git.WriteCdFile(res.RepoDir)
		return nil
	},
}

// completeWorktreeBranches provides tab-completion of worktree branch names.
func completeWorktreeBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	repo, err := git.NewRepo()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	entries, err := repo.ListWorktrees()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Branch, toComplete) {
			names = append(names, e.Branch)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

var useCmd = &cobra.Command{
	Use:   "use <branch>",
	Short: "Switch to an existing worktree by branch name",
	Long: `Navigate to an existing worktree that has the given branch checked out.

If no worktree is found for the branch, suggests creating one with 'gwt add'.

Requires shell integration (eval "$(gwt shell-init)") for the cd to work.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktreeBranches,
	RunE: func(cmd *cobra.Command, args []string) error {
		branch := args[0]

		repo, err := git.NewRepo()
		if err != nil {
			return err
		}

		path, found, err := repo.FindWorktreeByBranch(branch)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no worktree found for branch %q\nRun 'gwt add %s' to create one", branch, branch)
		}

		git.WriteCdFile(path)
		fmt.Println(path)
		return nil
	},
}

var shellInitCmd = &cobra.Command{
	Use:   "shell-init",
	Short: "Print shell integration code for auto-cd",
	Long: `Outputs a shell wrapper function that automatically cd's into newly
created worktrees or cloned repos.

Add to your shell profile:
  eval "$(gwt shell-init)"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(shellWrapper)
		return nil
	},
}

const shellWrapper = `gwt() {
    if [ "${1}" = "add" ] || [ "${1}" = "clone" ] || [ "${1}" = "rm" ] || [ "${1}" = "remove" ] || [ "${1}" = "use" ]; then
        local _gwt_cd_file
        _gwt_cd_file=$(mktemp)
        GWT_CD_FILE="$_gwt_cd_file" command gwt "$@"
        local _gwt_exit=$?
        if [ -s "$_gwt_cd_file" ]; then
            builtin cd "$(cat "$_gwt_cd_file")" || true
        fi
        rm -f "$_gwt_cd_file"
        return $_gwt_exit
    else
        command gwt "$@"
    fi
}

# Enable tab-completion for gwt
if [ -n "${ZSH_VERSION:-}" ]; then
    eval "$(command gwt completion zsh)"
elif [ -n "${BASH_VERSION:-}" ]; then
    eval "$(command gwt completion bash)"
fi
`

var validVersionManagers = map[string]bool{"asdf": true, "mise": true}
var validPackageManagers = map[string]bool{"pnpm": true, "npm": true, "yarn": true}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a post-checkout hook for worktree setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := git.NewRepo()
		if err != nil {
			return err
		}

		mainBranch, _ := cmd.Flags().GetString("main")
		copyFiles, _ := cmd.Flags().GetStringSlice("copy")
		versionManager, _ := cmd.Flags().GetString("version-manager")
		packageManager, _ := cmd.Flags().GetString("package-manager")
		force, _ := cmd.Flags().GetBool("force")

		if versionManager != "" && !validVersionManagers[versionManager] {
			return fmt.Errorf("invalid version manager %q: must be one of: asdf, mise", versionManager)
		}
		if packageManager != "" && !validPackageManagers[packageManager] {
			return fmt.Errorf("invalid package manager %q: must be one of: pnpm, npm, yarn", packageManager)
		}

		opts := hookOptions{
			mainBranch:     mainBranch,
			copyFiles:      copyFiles,
			versionManager: versionManager,
			packageManager: packageManager,
			force:          force,
		}

		wantHook := cmd.Flags().Changed("copy") || cmd.Flags().Changed("version-manager") ||
			cmd.Flags().Changed("package-manager") || cmd.Flags().Changed("with-hook")

		detected := false
		if wantHook {
			opts, detected = detectAndMerge(repo, opts, cmd.Flags().Changed("version-manager"), cmd.Flags().Changed("package-manager"))
		}

		if err := registerRepo(repo, opts); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", err)
		}

		if !wantHook {
			basePath := repoBasePath(repo, mainBranch)
			if _, err := os.Stat(filepath.Join(basePath, ".env")); err == nil {
				fmt.Println("hint: .env file found; to copy it to new worktrees, run:")
				fmt.Println("  gwt init -c .env")
			}
			return nil
		}

		if !hookHasWork(opts) {
			fmt.Println(noHookMsg)
			return nil
		}

		if err := setupHook(repo, opts); err != nil {
			return err
		}
		if detected {
			fmt.Println(fixItMsg)
		}
		return nil
	},
}

// registerRepo saves a repo to the config, overwriting if the configuration has changed.
// Used by init and clone to persist the hook configuration.
func registerRepo(repo *git.Repo, opts hookOptions) error {
	name, err := repo.CanonicalName()
	if err != nil {
		return fmt.Errorf("failed to determine repo name: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	entry := config.RepoEntry{
		Path:           repo.Dir,
		Bare:           repo.IsBare,
		PackageManager: opts.packageManager,
		VersionManager: opts.versionManager,
		CopyFiles:      opts.copyFiles,
		MainBranch:     opts.mainBranch,
	}

	if existing, ok := cfg.Lookup(name); ok && existing.Equal(entry) {
		return nil
	}

	cfg.Register(name, entry)

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Registered %s in config\n", name)
	return nil
}

// memberSetupDir returns the absolute path of the worktree whose short name
// matches ref (the setup_cwd), defaulting to the primary's worktree.
func memberSetupDir(group string, members []config.ResolvedMember, ref string) string {
	for _, m := range members {
		if ref != "" && (m.Short == ref || m.Name == ref) {
			return filepath.Join(group, m.Short)
		}
	}
	for _, m := range members {
		if m.IsPrimary {
			return filepath.Join(group, m.Short)
		}
	}
	return filepath.Join(group, members[0].Short)
}

// runWorkspaceAdd creates a worktree for every workspace member under a shared
// per-branch group directory, mirroring the branch to followers, then runs the
// workspace setup command. Returns the primary worktree path to cd into.
func runWorkspaceAdd(cfg *config.Config, wsName string, ws config.WorkspaceEntry, args []string) (string, error) {
	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		return "", err
	}
	parsed, err := git.ParseAddArgs(args)
	if err != nil {
		return "", err
	}
	root, err := ws.ResolveWorktreeRoot(wsName)
	if err != nil {
		return "", err
	}
	group := filepath.Join(root, git.BranchToDir(parsed.Branch))
	if err := os.MkdirAll(group, 0o755); err != nil {
		return "", fmt.Errorf("failed to create group dir: %w", err)
	}

	var created []string
	for _, m := range members {
		worktreePath := filepath.Join(group, m.Short)
		var gitArgs []string
		if m.IsPrimary {
			gitArgs = parsed.Build(worktreePath)
		} else if git.BranchExists(m.Path, parsed.Branch) {
			gitArgs = []string{worktreePath, parsed.Branch}
		} else {
			gitArgs = []string{"-b", parsed.Branch, worktreePath, git.MainBranchRef(m.Path, m.MainBranch)}
		}
		if err := git.AddWorktreeAt(m.Path, gitArgs); err != nil {
			return "", fmt.Errorf("creating worktree for %s failed: %w\ncreated so far: %v\nrun `gwt rm` from one of them to unwind", m.Name, err, created)
		}
		created = append(created, worktreePath)
		fmt.Printf("worktree: %s @ %s\n", worktreePath, parsed.Branch)
	}

	if ws.Setup != "" {
		setupDir := memberSetupDir(group, members, ws.SetupCwd)
		fmt.Printf("running setup: %s (in %s)\n", ws.Setup, setupDir)
		if err := git.RunSetup(ws.Setup, setupDir); err != nil {
			return "", err
		}
	}

	for _, m := range members {
		if m.IsPrimary {
			return filepath.Join(group, m.Short), nil
		}
	}
	return filepath.Join(group, members[0].Short), nil
}

// resolveWorkspaceGroup maps a user-supplied worktree identifier — a branch
// name, a member worktree path, or the group dir itself — to the branch group
// directory to remove.
func resolveWorkspaceGroup(root string, members []config.ResolvedMember, arg string) (string, error) {
	if !strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "/") && !strings.HasPrefix(arg, ".") {
		// Branch name: resolve via the primary member's worktree list.
		primary := members[0]
		for _, m := range members {
			if m.IsPrimary {
				primary = m
			}
		}
		pr := &git.Repo{Dir: primary.Path}
		if path, found, err := pr.FindWorktreeByBranch(arg); err == nil && found {
			return validateGroup(root, arg, filepath.Dir(path))
		}
		// Group-dir name as shown in `gwt ls` paths (branch with '/' flattened to '-').
		if fi, err := os.Stat(filepath.Join(root, arg)); err == nil && fi.IsDir() {
			return validateGroup(root, arg, groupFromPath(filepath.Join(root, arg), members))
		}
		return "", fmt.Errorf("no workspace worktree found for %q", arg)
	}

	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", arg, err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no workspace worktree found at %q", arg)
	}
	return validateGroup(root, arg, groupFromPath(abs, members))
}

// groupFromPath returns the group dir for a path that may be either a member
// worktree (its parent is the group) or the group dir itself.
func groupFromPath(path string, members []config.ResolvedMember) string {
	base := filepath.Base(path)
	for _, m := range members {
		if base == m.Short {
			return filepath.Dir(path)
		}
	}
	return path
}

// validateGroup rejects group dirs that are not direct children of the
// workspace root, so removal can never touch a real repo checkout (e.g. when
// arg is the main branch, which resolves to a member's actual repo). Returns
// the group with symlinks resolved.
func validateGroup(root, arg, group string) (string, error) {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if g, err := filepath.EvalSymlinks(group); err == nil {
		group = g
	}
	if filepath.Dir(filepath.Clean(group)) != filepath.Clean(root) {
		return "", fmt.Errorf("%s resolves to %s, which is not a workspace worktree under %s", arg, group, root)
	}
	return filepath.Clean(group), nil
}

// runWorkspaceRemove removes every member worktree in the branch group dir
// concurrently, then cleans the empty group dir if every member succeeded.
// Removal is best-effort: every present member is attempted even if others
// fail, and results are aggregated into a summary printed to stdout. Returns
// the primary's real repo path to cd back into, and a non-nil error when any
// member failed (for a non-zero exit).
func runWorkspaceRemove(cfg *config.Config, wsName string, ws config.WorkspaceEntry, group string, keepBranch, force bool) (string, error) {
	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		return "", err
	}

	type memberResult struct {
		name    string
		present bool
		mr      git.MemberRemoval
	}
	results := make([]memberResult, len(members))

	var wg sync.WaitGroup
	for i, m := range members {
		worktreePath := filepath.Join(group, m.Short)
		if _, statErr := os.Stat(worktreePath); statErr != nil {
			results[i] = memberResult{name: m.Short, present: false}
			continue
		}
		wg.Add(1)
		go func(i int, repoDir, worktreePath, name string) {
			defer wg.Done()
			results[i] = memberResult{
				name:    name,
				present: true,
				mr:      git.RemoveMemberWorktree(repoDir, worktreePath, keepBranch, force),
			}
		}(i, m.Path, worktreePath, m.Short)
	}
	wg.Wait()

	// Aggregate.
	var totalBytes int64
	anyApprox := false
	removed, attempted := 0, 0
	var failures []string                 // "repo: reason"
	keptBranches := map[string][]string{} // branch -> repos
	for _, res := range results {
		if !res.present {
			continue
		}
		attempted++
		if res.mr.Err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", res.name, res.mr.Err))
			continue
		}
		removed++
		totalBytes += res.mr.Freed.Bytes
		if res.mr.Freed.Skipped > 0 {
			anyApprox = true
		}
		if res.mr.BranchKept != "" {
			keptBranches[res.mr.BranchKept] = append(keptBranches[res.mr.BranchKept], res.name)
		}
	}

	// Clean the empty group dir (best-effort, only if everything removed).
	root, rootErr := ws.ResolveWorktreeRoot(wsName)
	if rootErr == nil && len(failures) == 0 {
		_ = os.Remove(group)
		git.CleanEmptyParents(group, root)
	}

	// Report.
	groupName := filepath.Base(group)
	sizeStr := disk.FormatApprox(totalBytes, anyApprox)
	if len(failures) == 0 {
		fmt.Printf("removed workspace group %s (%d repos) — freed %s\n", groupName, removed, sizeStr)
	} else {
		fmt.Printf("removed %d/%d repos — freed %s\n", removed, attempted, sizeStr)
		for _, f := range failures {
			fmt.Printf("  ! %s\n", f)
		}
	}
	for branch, repos := range keptBranches {
		fmt.Printf("note: branch %q kept (not fully merged) in: %s\n", branch, strings.Join(repos, ", "))
		fmt.Printf("      delete with: git -C <repo> branch -D %s\n", branch)
	}

	// Primary path to cd back into.
	primaryPath := members[0].Path
	for _, m := range members {
		if m.IsPrimary {
			primaryPath = m.Path
		}
	}

	if len(failures) > 0 {
		return primaryPath, fmt.Errorf("%d of %d worktrees could not be removed", len(failures), attempted)
	}
	return primaryPath, nil
}

// ensureRegistered adds the repo to the config if not already present.
// Used by add to auto-register non-bare repos with a minimal entry.
func ensureRegistered(repo *git.Repo, name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if _, ok := cfg.Lookup(name); ok {
		return nil
	}
	cfg.Register(name, config.RepoEntry{Path: repo.Dir})
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// isSizeFlag reports whether args is exactly the size flag for `gwt ls`.
func isSizeFlag(args []string) bool {
	return len(args) == 1 && (args[0] == "-s" || args[0] == "--size")
}

func main() {
	initCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	initCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	initCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	initCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing post-checkout hook")
	initCmd.Flags().BoolP("with-hook", "w", false, "Auto-detect managers and generate a post-checkout hook")

	cloneCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	cloneCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	cloneCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	cloneCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
	cloneCmd.Flags().BoolP("with-hook", "w", false, "Auto-detect managers and generate a post-checkout hook")
	rootCmd.Version = resolveVersion()
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(useCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(shellInitCmd)

	// Check for pass-through before cobra runs
	if len(os.Args) > 1 {
		subcmd := os.Args[1]

		// Resolve aliases
		if replacement, ok := aliases[subcmd]; ok {
			os.Args[1] = replacement
			subcmd = replacement
		}

		known := map[string]bool{
			"init": true, "add": true, "clone": true, "remove": true, "rm": true,
			"use": true, "version": true, "shell-init": true,
			"help": true, "completion": true, "__complete": true,
			"--help": true, "-h": true, "--version": true,
		}

		// Valid git worktree subcommands forwarded directly.
		passthroughAllowed := map[string]bool{
			"list": true, "prune": true, "lock": true,
			"unlock": true, "move": true, "repair": true,
		}

		if !known[subcmd] {
			if passthroughAllowed[subcmd] {
				repo, err := git.NewRepo()
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					os.Exit(1)
				}
				// Enhance the bare `gwt list` (and its `ls` alias) by marking
				// the active worktree. `-s`/`--size` adds an on-disk size column.
				// Any other flags fall through to plain git untouched.
				if subcmd == "list" {
					if len(os.Args) == 2 {
						if err := repo.PrintWorktreeList(); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							os.Exit(git.ExitCode(err))
						}
						return
					}
					if isSizeFlag(os.Args[2:]) {
						if err := repo.PrintSizedWorktreeList(); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							os.Exit(git.ExitCode(err))
						}
						return
					}
				}
				if err := repo.Passthrough(os.Args[1:]); err != nil {
					os.Exit(git.ExitCode(err))
				}
				return
			}
			fmt.Fprintf(os.Stderr, "gwt: unknown command %q\n\nRun 'gwt --help' for usage.\n", subcmd)
			os.Exit(1)
		}
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
