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
  clone Clone a repo into a bare-repo worktree structure
  init  Generate a post-checkout hook for worktree setup

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

The resulting directory is ready for 'gwt init' and 'gwt add'.`,
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

		repo := &git.Repo{Dir: absDir, IsBare: true}
		if err := setupHook(repo, defaultHookOptions()); err != nil {
			return err
		}

		fmt.Printf("Cloned into %s\n", absDir)
		fmt.Println("Next steps:")
		fmt.Println("  cd", absDir)
		fmt.Println("  gwt add <branch>")
		return nil
	},
}

var addCmd = &cobra.Command{
	Use:   "add [git worktree add flags] <path> [<commit-ish>]",
	Short: "Create a worktree",
	Long: `Wraps 'git worktree add' to create a new worktree.

File copying and project setup are handled by the post-checkout hook
installed via 'gwt init'.

All flags and arguments are passed directly to 'git worktree add'.
Run 'git worktree add --help' for available options.`,
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

		_, err = repo.Add(args)
		return err
	},
}

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

	rootCmd.Version = resolveVersion()
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)

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
