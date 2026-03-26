package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/nicwestvold/gwt/config"
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

If init flags (--main, --copy, --version-manager, --package-manager)
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

		if regErr := registerRepo(repo, opts); regErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", regErr)
		}

		initFlags := []string{"main", "copy", "version-manager", "package-manager"}
		wantHook := false
		for _, f := range initFlags {
			if cmd.Flags().Changed(f) {
				wantHook = true
				break
			}
		}

		if wantHook {
			if err := setupHook(repo, opts); err != nil {
				fmt.Fprintf(os.Stderr, "Clone succeeded, but hook setup failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "You can retry with: cd %s && gwt init\n", absDir)
				return err
			}
		}

		git.WriteCdFile(absDir)

		fmt.Printf("Cloned into %s\n", absDir)
		fmt.Println("Next steps:")
		fmt.Println("  cd", absDir)
		if !wantHook {
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

var removeCmd = &cobra.Command{
	Use:     "remove [flags] [<worktree>]",
	Aliases: []string{"rm"},
	Short:   "Remove a worktree",
	Long: `Remove a worktree. If no path is given, removes the current worktree.

Accepts a branch name or a path. If the argument matches a worktree branch,
it resolves to that worktree's path.

After removal, the shell wrapper (from 'gwt shell-init') will cd back
to the repository root.

Supports all git worktree remove flags (e.g., --force).`,
	DisableFlagParsing:    true,
	ValidArgsFunction:     completeWorktreeBranches,
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

		repoDir, worktreePath, err := repo.Remove(resolvedArgs)
		if err != nil {
			return err
		}

		// Clean up empty parent dirs for centralized worktrees.
		dataDir, dataErr := config.DataDir()
		if dataErr == nil {
			worktreeRoot := filepath.Join(dataDir, "worktrees")
			if strings.HasPrefix(worktreePath, worktreeRoot+string(filepath.Separator)) {
				git.CleanEmptyParents(filepath.Dir(worktreePath), worktreeRoot)
			}
		}

		git.WriteCdFile(repoDir)
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

		if err := registerRepo(repo, opts); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", err)
		}

		wantHook := cmd.Flags().Changed("copy") || cmd.Flags().Changed("version-manager") || cmd.Flags().Changed("package-manager")
		if !wantHook {
			basePath := repoBasePath(repo, mainBranch)
			if _, err := os.Stat(filepath.Join(basePath, ".env")); err == nil {
				fmt.Println("hint: .env file found; to copy it to new worktrees, run:")
				fmt.Println("  gwt init -c .env")
			}
			return nil
		}

		return setupHook(repo, opts)
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

func main() {
	initCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	initCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	initCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	initCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing post-checkout hook")

	cloneCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	cloneCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	cloneCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	cloneCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
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
			"help": true, "completion": true,
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
