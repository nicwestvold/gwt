package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/nicwestvold/gwt/git"
	"github.com/nicwestvold/gwt/hook"
	"github.com/spf13/cobra"
)

var version = "dev"

var aliases = map[string]string{
	"ls": "list",
	"rm": "remove",
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

Any command not listed below is passed directly to git worktree.
For example, 'gwt list' runs 'git worktree list'.

Run 'git worktree --help' for the full git worktree documentation.

Pass-through commands:
  list    List worktrees
  lock    Lock a worktree
  move    Move a worktree
  prune   Prune stale worktree information
  remove  Remove a worktree
  repair  Repair worktree administrative files
  unlock  Unlock a worktree

Aliases:
  ls    list
  rm    remove

Additional commands:
  clone      Clone a repo into a bare-repo worktree structure
  init       Generate a post-checkout hook for worktree setup
  shell-init Print shell integration for auto-cd

Enhanced commands:
  add   Create a worktree (setup handled by post-checkout hook)`,
}

type hookOptions struct {
	mainBranch     string
	copyFiles      []string
	versionManager string
	packageManager string
	force          bool
}

func defaultHookOptions() hookOptions {
	return hookOptions{
		mainBranch: "main",
		copyFiles:  []string{".env"},
	}
}

func setupHook(repo *git.Repo, opts hookOptions) error {
	if repo.IsBare {
		if err := repo.ConfigureFetch(); err != nil {
			return fmt.Errorf("failed to configure fetch: %w", err)
		}
	}

	var basePath string
	if repo.IsBare {
		basePath = filepath.Join(repo.Dir, opts.mainBranch)
	} else {
		basePath = repo.Dir
	}

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

var cloneCmd = &cobra.Command{
	Use:   "clone <repository> [<directory>]",
	Short: "Clone a repo into a bare-repo worktree structure",
	Long: `Clones a repository as a bare repo inside a .bare/ directory,
creates a .git file pointing to it, configures fetch, and fetches all branches.

If init flags (--main, --copy, --no-copy, --version-manager, --package-manager)
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
		noCopy, _ := cmd.Flags().GetBool("no-copy")
		versionManager, _ := cmd.Flags().GetString("version-manager")
		packageManager, _ := cmd.Flags().GetString("package-manager")

		if noCopy {
			copyFiles = nil
		}

		if versionManager != "" && !validVersionManagers[versionManager] {
			return fmt.Errorf("invalid version manager %q: must be one of: asdf, mise", versionManager)
		}
		if packageManager != "" && !validPackageManagers[packageManager] {
			return fmt.Errorf("invalid package manager %q: must be one of: pnpm, npm, yarn", packageManager)
		}

		initFlags := []string{"main", "copy", "no-copy", "version-manager", "package-manager"}
		wantHook := false
		for _, f := range initFlags {
			if cmd.Flags().Changed(f) {
				wantHook = true
				break
			}
		}

		if wantHook {
			repo := &git.Repo{Dir: absDir, IsBare: true}
			if err := setupHook(repo, hookOptions{
				mainBranch:     mainBranch,
				copyFiles:      copyFiles,
				versionManager: versionManager,
				packageManager: packageManager,
			}); err != nil {
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

		path, err := repo.Add(args)
		if err == nil && path != "" {
			git.WriteCdFile(path)
		}
		return err
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
    if [ "${1}" = "add" ] || [ "${1}" = "clone" ]; then
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
		noCopy, _ := cmd.Flags().GetBool("no-copy")
		versionManager, _ := cmd.Flags().GetString("version-manager")
		packageManager, _ := cmd.Flags().GetString("package-manager")
		force, _ := cmd.Flags().GetBool("force")

		if noCopy {
			copyFiles = nil
		} else if !cmd.Flags().Changed("copy") {
			copyFiles = []string{".env"}
		}

		if versionManager != "" && !validVersionManagers[versionManager] {
			return fmt.Errorf("invalid version manager %q: must be one of: asdf, mise", versionManager)
		}
		if packageManager != "" && !validPackageManagers[packageManager] {
			return fmt.Errorf("invalid package manager %q: must be one of: pnpm, npm, yarn", packageManager)
		}

		return setupHook(repo, hookOptions{
			mainBranch:     mainBranch,
			copyFiles:      copyFiles,
			versionManager: versionManager,
			packageManager: packageManager,
			force:          force,
		})
	},
}

func main() {
	initCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	initCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	initCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	initCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
	initCmd.Flags().Bool("no-copy", false, "Suppress default file copying")
	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing post-checkout hook")

	cloneCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	cloneCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")
	cloneCmd.Flags().StringP("version-manager", "v", "", "Version manager (asdf or mise)")
	cloneCmd.Flags().StringP("package-manager", "p", "", "Package manager (pnpm, npm, or yarn)")
	cloneCmd.Flags().Bool("no-copy", false, "Suppress default file copying")

	rootCmd.Version = resolveVersion()
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(initCmd)
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
			"init": true, "add": true, "clone": true, "version": true,
			"shell-init": true,
			"help": true, "completion": true,
			"--help": true, "-h": true, "--version": true,
		}

		if !known[subcmd] {
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
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
