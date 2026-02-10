# gwt

A wrapper around `git worktree` that auto-copies files (`.env`, etc.) into new worktrees and fixes `git fetch` in bare repos.

## tl;dr;

You should probabaly just [use git hooks when creating worktrees](https://mskelton.dev/bytes/using-git-hooks-when-creating-worktrees).

You still need to update your git config so that `git fetch` works properly.

```
git config remote.origin.fetch "+refs/heads/*:refs/remotes/origin/*"
```

## Install

```bash
go install github.com/nicwestvold/gwt@latest
```

## Quick Start

```bash
git clone --bare git@github.com:you/your-repo.git your-repo
cd your-repo

gwt init                    # configure fetch for bare repo
gwt init --copy .env        # also copy .env into new worktrees
gwt add main                # create worktree for main branch
gwt add my-feature          # .env is copied automatically
```

## Usage

```bash
gwt init --copy .env --copy .env.local   # set files to copy (saved to .gwt.json)
gwt init --main develop                  # set main branch name

gwt add my-feature                       # create worktree, copy configured files

gwt list                                 # pass-through to git worktree
gwt remove my-feature                    # pass-through to git worktree
```

Aliases: `ls` → `list`, `rm` → `remove`. Any unrecognized command is passed directly to `git worktree`.

In bare repos, `gwt init` also configures `remote.origin.fetch` so `git fetch` works properly.
