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

## How it works

1. **`gwt clone`** clones a repository into a bare-repo structure
2. **`gwt init`** generates a `post-checkout` hook (and fixes `git fetch` in bare repos)
3. **`gwt add`** creates a worktree — git runs the hook automatically
4. **The hook** copies files (`.env`, etc.), installs dependencies, and runs builds

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
