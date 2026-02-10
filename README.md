# gwt

A wrapper around `git worktree` that auto-copies files (`.env`, etc.) into new worktrees and fixes `git fetch` in bare repos.

## tl;dr;

You should probably just [use git hooks when creating worktrees](https://mskelton.dev/bytes/using-git-hooks-when-creating-worktrees).

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
gwt clone git@github.com:you/your-repo.git
cd your-repo

gwt init                           # generate hook (copies .env by default)
gwt add main                       # create worktree for main branch
gwt add my-feature                 # post-checkout hook runs automatically
```

## Usage

### Clone

```bash
gwt clone <repo>                         # bare-repo setup, no hook
gwt clone <repo> --copy .env -p pnpm     # clone and create hook in one step
gwt clone <repo> -m develop --no-copy    # clone with custom main branch, no file copying
```

Without init flags, run `gwt init` afterward to generate the post-checkout hook.

### Init

```bash
gwt init                                 # generate hook with default file copy (.env)
gwt init -c .secret -c certs/dev.pem     # custom files to copy
gwt init --no-copy                       # no file copying
gwt init -p pnpm -v mise                 # install deps + build via mise/pnpm
gwt init --force                         # overwrite existing post-checkout hook
gwt init --main develop                  # set main branch name
```

### Add / Pass-through

```bash
gwt add my-feature                       # create worktree (hook handles setup)

gwt list                                 # pass-through to git worktree
gwt remove my-feature                    # pass-through to git worktree
```

Aliases: `ls` → `list`, `rm` → `remove`. Any unrecognized command is passed directly to `git worktree`.

In bare repos, `gwt init` also configures `remote.origin.fetch` so `git fetch` works properly.
