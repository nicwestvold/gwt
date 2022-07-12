package main

import (
	"errors"
	"log"

	"github.com/nicwestvold/gwt/git"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gwt",
	Short: "Use git worktrees with ease",
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List current worktrees",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := git.InRepo()
		if err != nil {
			return err
		}

		repo := git.NewRepo()
		repo.List()
		return nil
	},
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new worktree",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("requires a worktree name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		err := git.InRepo()
		if err != nil {
			return err
		}

		name := args[0]

		repo := git.NewRepo()
		err = repo.Add(name)
		if err != nil {
			return err
		}
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm"},
	Short:   "Remove a new worktree",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("requires a worktree name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		err := git.InRepo()
		if err != nil {
			return err
		}

		name := args[0]

		repo := git.NewRepo()
		err = repo.Remove(name)
		if err != nil {
			return err
		}
		return nil
	},
}

func main() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
