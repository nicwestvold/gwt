package git

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

type Repo struct {
	Dir string
}

// git config remote.origin.fetch "+refs/heads/*:refs/remotes/origin/*"
// when cloning a bare repo, run this command
// taken from: https://morgan.cugerone.com/blog/workarounds-to-git-worktree-using-bare-repository-and-cannot-fetch-remote-branches/

func NewRepo() *Repo {
	repo_dir := os.Getenv("CAPELLA_REPO")
	if repo_dir == "" {
		log.Fatalln("$CAPELLA_REPO env var not set")
	}
	return &Repo{
		Dir: repo_dir,
	}
}

func InRepo() error {
	var buf bytes.Buffer

	// returns true, given inside of top-level worktree dir
	// git rev-parse --is-inside-git-dir
	cmd := exec.Command("git", "rev-parse", "--is-inside-git-dir")
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}
	insideGitDir := strings.HasPrefix(buf.String(), "true")
	buf.Truncate(0)

	// returns true, given insdie of worktree
	// git rev-parse --is-inside-work-tree
	cmd = exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Stdout = &buf
	err = cmd.Run()
	if err != nil {
		return err
	}

	insideWorktree := strings.HasPrefix(buf.String(), "true")
	buf.Truncate(0)

	if !insideGitDir && !insideWorktree {
		fmt.Println("not in git dir")
		return errors.New("Not currently in a git directory")
	}

	return nil
}

func (r *Repo) List() error {
	var buf bytes.Buffer

	cmd := exec.Command("git", "worktree", "list")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}

	fmt.Println(buf.String())

	return nil
}

func (r *Repo) Add(name string) error {
	var buf bytes.Buffer

	cmd := exec.Command("git", "worktree", "add", name)
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}

	fmt.Println(buf.String())

	return nil
}

func (r *Repo) Remove(name string) error {
	var buf bytes.Buffer

	cmd := exec.Command("git", "worktree", "remove", name)
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}

	fmt.Println(buf.String())

	return nil
}
