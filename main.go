package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/nicwestvold/gwt/config"
	"github.com/nicwestvold/gwt/git"
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
  init  Initialize gwt configuration (fetch config, file copy settings)

Enhanced commands:
  add   Create a worktree and copy configured files`,
}

var addCmd = &cobra.Command{
	Use:   "add [git worktree add flags] <path> [<commit-ish>]",
	Short: "Create a worktree and copy configured files",
	Long: `Wraps 'git worktree add' and copies configured files into the new worktree.

By default, .env is copied. Additional files can be configured via 'gwt init --copy'.

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

		worktreePath, err := repo.Add(args)
		if err != nil {
			return err
		}

		if worktreePath == "" {
			return nil
		}

		cfg, err := config.Load(repo.Dir)
		if err != nil {
			return err
		}

		if len(cfg.CopyFiles) == 0 {
			return nil
		}

		mainPath, err := repo.WorktreePathForBranch(cfg.MainBranch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v; skipping file copy\n", err)
			return nil
		}

		for _, f := range cfg.CopyFiles {
			if err := git.CopyFileToWorktree(mainPath, worktreePath, f); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not copy %s: %v\n", f, err)
			}
		}

		return nil
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize gwt configuration for this repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := git.NewRepo()
		if err != nil {
			return err
		}

		if repo.IsBare {
			if err := repo.ConfigureFetch(); err != nil {
				return fmt.Errorf("failed to configure fetch: %w", err)
			}
		}

		mainBranch, _ := cmd.Flags().GetString("main")
		copyFiles, _ := cmd.Flags().GetStringSlice("copy")

		cfg := config.Config{
			MainBranch: mainBranch,
			CopyFiles:  copyFiles,
		}

		if err := config.Save(repo.Dir, cfg); err != nil {
			return err
		}

		return nil
	},
}

func main() {
	initCmd.Flags().StringP("main", "m", "main", "Set the main branch name")
	initCmd.Flags().StringSliceP("copy", "c", nil, "Files to copy to new worktrees (repeatable)")

	rootCmd.Version = resolveVersion()
	rootCmd.AddCommand(addCmd)
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
			"init": true, "add": true, "version": true,
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
