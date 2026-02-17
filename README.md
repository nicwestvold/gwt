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

## Shell Integration (optional)

Add to your shell profile (`.zshrc`, `.bashrc`, etc.) for auto-cd into new worktrees after `add` and `clone`:

```bash
eval "$(gwt shell-init)"
```

## Quick Start

```bash
gwt clone git@github.com:you/your-repo.git
cd your-repo                               # auto-cd if shell integration is set up

gwt init                                   # generate hook (copies .env by default)
gwt add main                               # create worktree for main branch
gwt add my-feature                         # post-checkout hook runs automatically
                                           # auto-cd's into worktree with shell integration
```

## Usage

### Clone

```bash
gwt clone <repo>                         # bare-repo setup, no hook
gwt clone <repo> --copy .env -p pnpm     # clone and create hook in one step
gwt clone <repo> -m develop --no-copy    # clone with custom main branch, no file copying
```

Without init flags (`--main`, `--copy`, `--no-copy`, `--version-manager`, `--package-manager`), no post-checkout hook is created. Run `gwt init` afterward to generate one.

### Init

```bash
gwt init                                 # generate hook with default file copy (.env)
gwt init -c .secret -c certs/dev.pem     # custom files to copy
gwt init --no-copy                       # no file copying
gwt init -p pnpm -v mise                 # install deps + build via mise/pnpm
gwt init --force                         # overwrite existing post-checkout hook
gwt init --main develop                  # set main branch name
```

In bare repos, `gwt init` also configures `remote.origin.fetch` so `git fetch` works properly.

### Add

```bash
gwt add my-feature                       # create worktree (hook handles setup)
gwt add fix/login-bug                    # directory derived: fix/login-bug → fix-login-bug
gwt add -b feat/new-feature              # create a new branch
gwt add -b feat/new-feature origin/main  # create a new branch from a start-point
```

With shell integration, your shell auto-cd's into the new worktree.

### Pass-through

```bash
gwt list                                 # pass-through to git worktree
gwt remove my-feature                    # pass-through to git worktree
```

Aliases: `ls` → `list`, `rm` → `remove`. Any unrecognized command is passed directly to `git worktree`.
