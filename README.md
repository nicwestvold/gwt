# gwt

A CLI tool that makes the git worktree workflow painless — clone, branch, and go.

## Why?

Working on multiple branches at once usually means stashing, switching, and waiting for dependency installs. Git worktrees solve this by giving each branch its own directory, but the bare-repo workflow has rough edges: environment files don't carry over, `git fetch` breaks, and you still have to manually install dependencies in every new worktree.

`gwt` automates all of that. It sets up bare-repo clones correctly, generates a `post-checkout` hook that copies files and installs dependencies, and gets out of your way for everything else by passing commands straight through to `git worktree`.

<details>
<summary>What are git worktrees?</summary>

Git worktrees let you check out multiple branches of the same repository into separate directories simultaneously. Instead of stashing your work and switching branches, you just `cd` into another directory. Each worktree shares the same git history, so there's no duplication — just parallel working copies.

A common pattern is to use a **bare repo** as the central `.git` store, with each branch checked out as a sibling directory. This is the layout `gwt` creates and manages.

Learn more: [git-worktree documentation](https://git-scm.com/docs/git-worktree)
</details>

## Install

```bash
go install github.com/nicwestvold/gwt@latest
```

## Shell Integration (optional)

Add to your shell profile (`.zshrc`, `.bashrc`, etc.) for auto-cd after `add`, `clone`, `remove`, and `use`:

```bash
eval "$(command gwt shell-init)"
```

> **Note:** `command` bypasses shell aliases. This is required if you use oh-my-zsh's git plugin, which aliases `gwt` to `git worktree`.

## Quick Start

```bash
gwt clone git@github.com:you/your-repo.git
cd your-repo

gwt init -c .env                           # generate hook that copies .env to new worktrees
gwt add main                               # create worktree for main branch
gwt add my-feature                         # post-checkout hook runs automatically
gwt use main                               # jump to the main worktree
gwt rm my-feature                          # remove worktree + delete branch
```

With shell integration, `add`, `clone`, `use`, and `rm` auto-cd you to the right directory.

## How it works

1. **`gwt clone`** clones a repository into a bare-repo structure
2. **`gwt init`** generates a `post-checkout` hook (and fixes `git fetch` in bare repos)
3. **`gwt add`** creates a worktree — git runs the hook automatically
4. **The hook** copies files, installs dependencies, and runs builds

```
your-repo/
├── .bare/            # the bare git repo
├── .git              # file pointing to .bare/
├── main/             # worktree for main branch
├── my-feature/       # worktree for feature branch
└── fix-login-bug/    # worktree (slashes become hyphens)
```

## Usage

### Clone

```bash
gwt clone <repo>                         # bare-repo setup, no hook
gwt clone <repo> --copy .env -p pnpm     # clone and create hook in one step
gwt clone <repo> -m develop              # clone with custom main branch
```

Without init flags (`--main`, `--copy`, `--version-manager`, `--package-manager`), no post-checkout hook is created. Run `gwt init` afterward to generate one.

### Init

```bash
gwt init                                 # register repo (hints if .env found)
gwt init -c .env                         # copy .env to new worktrees
gwt init -c .secret -c certs/dev.pem     # copy multiple files
gwt init -p pnpm -v mise                 # install deps + build via mise/pnpm
gwt init -c .env -p pnpm -v mise         # copy files and install deps
gwt init -f                              # overwrite existing post-checkout hook
gwt init --main develop                  # set main branch name
```

A hook is only generated when `-c`, `-p`, or `-v` flags are provided. When generating a hook in a bare repo, `gwt init` also configures `remote.origin.fetch` so `git fetch` works properly.

When a package manager is specified, the hook runs `<manager> install` followed by a build command (`yarn build`, `pnpm run build`, or `npm run build`). If the install step fails, the build is skipped.

### Add

```bash
gwt add my-feature                       # create worktree (hook handles setup)
gwt add fix/login-bug                    # directory derived: fix/login-bug → fix-login-bug
gwt add -b feat/new-feature              # create a new branch
gwt add -b feat/new-feature origin/main  # create a new branch from a start-point
```

If the branch isn't found locally, `gwt` auto-fetches from origin and retries.

With shell integration, your shell auto-cd's into the new worktree.

### Remove

```bash
gwt remove my-feature                    # remove worktree by path
gwt rm my-feature                        # rm is an alias for remove
gwt rm                                   # no args = remove current worktree
gwt rm feature/login                     # accepts branch names too
```

After removing the worktree, `gwt` does a best-effort `git branch -d` to clean up the branch. With shell integration, your shell cd's back to the repo root.

### Use

```bash
gwt use my-feature                       # cd into the worktree for this branch
```

Finds the worktree checked out on the given branch and switches to it. Requires shell integration for the auto-cd.

### Pass-through

These git worktree subcommands are forwarded directly:

```bash
gwt list                                 # git worktree list
gwt prune                                # git worktree prune
gwt lock <worktree>                      # git worktree lock
gwt unlock <worktree>                    # git worktree unlock
gwt move <worktree> <new-path>           # git worktree move
gwt repair                               # git worktree repair
```

`ls` is an alias for `list`. Unrecognized commands are rejected — only the above are passed through.

### Version

```bash
gwt version
```

## Requirements

- **Go 1.25+** (for `go install`)
- **Git**
- **bash or zsh** (for shell integration and hook execution)

## License

[MIT](LICENSE)
