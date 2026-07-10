# gwt

A CLI that makes the git worktree workflow painless — clone, branch, and go.

## Why?

Juggling branches means stashing, switching, and re-installing dependencies. Git worktrees fix that — each branch gets its own directory — but the bare-repo setup has sharp edges: `.env` files don't carry over, `git fetch` breaks, and every new worktree needs its dependencies installed by hand.

`gwt` smooths all of it: correct bare-repo clones, a `post-checkout` hook that copies your files and installs dependencies, and straight pass-through to `git worktree` for everything else.

<details>
<summary>New to git worktrees?</summary>

Git worktrees let you check out multiple branches into separate directories at once. Instead of stashing and switching, you just `cd` to another directory. Every worktree shares the same git history — no duplication, just parallel working copies.

A common pattern uses a **bare repo** as the central `.git` store, with each branch checked out as a sibling directory. That's the layout `gwt` creates and manages.

Learn more: [git-worktree documentation](https://git-scm.com/docs/git-worktree)
</details>

## Install

Easiest path, with [mise](https://mise.jdx.dev) — it brings the right Go toolchain and builds `gwt` for you:

```bash
git clone https://github.com/nicwestvold/gwt.git
cd gwt
mise run install        # builds with version info, installs to ~/.local/bin
```

Already have Go? Grab it directly:

```bash
go install github.com/nicwestvold/gwt@latest
```

Update later with `mise run update` (or re-run `go install …@latest`).

## Quick Start

```bash
gwt clone git@github.com:you/your-repo.git
cd your-repo

gwt init -c .env      # generate a hook that copies .env into new worktrees; auto-detects mise/pnpm/etc.
gwt add main          # create a worktree for the main branch
gwt add my-feature    # the hook runs automatically
gwt use main          # jump to the main worktree
gwt rm my-feature     # remove the worktree (and delete its branch)
```

> **Tip:** turn on [shell integration](#shell-integration) so `add`, `clone`, `use`, and `rm` drop you straight into the right directory.

## Shell Integration

Add this to your shell profile (`.zshrc`, `.bashrc`, …):

```bash
eval "$(command gwt shell-init)"
```

Now `gwt` auto-cd's you after `add`, `clone`, and `use` (into the worktree) and after `rm` (back to the repo root), and tab-completion works too.

> **Note:** `command` bypasses shell aliases — needed if oh-my-zsh's git plugin has aliased `gwt` to `git worktree`.

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
gwt clone <repo> --copy .env -p pnpm     # clone and create the hook in one step
gwt clone <repo> -m develop              # clone with a custom main branch
gwt clone <repo> -w                      # clone, then auto-detect managers for the hook
```

Without init flags (`--main`, `--copy`, `-v`, `-p`, `-w`), no hook is created — run `gwt init` afterward to generate one.

### Init

```bash
gwt init                                 # register repo (hints if .env is found)
gwt init -c .env                         # copy .env into new worktrees
gwt init -c .secret -c certs/dev.pem     # copy multiple files
gwt init -p pnpm -v mise                 # install deps + build via mise/pnpm
gwt init -c .env -p pnpm -v mise         # copy files and install deps
gwt init -f                              # overwrite an existing hook
gwt init --main develop                  # set the main branch name
gwt init -w                              # auto-detect managers + generate a hook
```

A hook is generated when `-c`, `-p`, `-v`, or `-w`/`--with-hook` is provided. `-w` auto-detects the version manager (mise/asdf) and package manager (pnpm/npm/yarn) from the repo; if it finds neither and no `-c` files were given, no hook is written. Detection also runs alongside `-c`/`-p`/`-v` to fill in whatever you didn't specify — explicit flags always win. In a bare repo, `gwt init` also configures `remote.origin.fetch` so `git fetch` works properly.

With a package manager, the hook runs `<manager> install` followed by a build (`yarn build`, `pnpm run build`, or `npm run build`). If install fails, the build is skipped.

### Add

```bash
gwt add my-feature                       # create a worktree (hook handles setup)
gwt add fix/login-bug                    # directory derived: fix/login-bug → fix-login-bug
gwt add -b feat/new-feature              # create a new branch
gwt add -b feat/new-feature origin/main  # create a new branch from a start-point
```

If the branch isn't found locally, `gwt` auto-fetches from origin and retries.

### Remove

```bash
gwt remove my-feature                    # remove a worktree by path
gwt rm my-feature                        # rm is an alias for remove
gwt rm                                   # no args = remove the current worktree
gwt rm feature/login                     # accepts branch names too
```

After removing the worktree, `gwt` does a best-effort `git branch -d` to clean up the branch. Use `-k`/`--keep-branch` to keep it.

### Use

```bash
gwt use my-feature                       # cd into the worktree for this branch
```

Finds the worktree checked out on the given branch and switches to it (needs shell integration). If none exists, it suggests `gwt add`.

### Workspaces

For codebases split across mutually-dependent sibling repos (e.g. an `app` + `app-plugins` pair that must sit next to each other so `../app-plugins` resolves), define a **workspace** in `~/.config/gwt/config.toml`. Both repos must already be registered (via `gwt init`/`gwt clone`).

```toml
[workspaces.app]
members       = ["app", "app-plugins"]          # repos, by name; first is primary
primary       = "app"                            # cd target; followers mirror its branch
setup         = "make dev"                       # optional; runs after all worktrees exist
setup_cwd     = "app"                            # member dir setup runs in (default: primary)
worktree_root = "~/Development/app/.worktrees"   # optional; default: gwt data dir
```

Then, from inside any member:

```bash
gwt add -b feat/x   # creates <root>/feat-x/app and <root>/feat-x/app-plugins on
                    # branch feat/x, runs setup, and cd's into the primary
gwt rm              # removes the whole group and cd's back to the primary repo
gwt rm feat/x       # same, by branch name (or a worktree/group path) from any member
```

Followers mirror the branch: an existing branch is checked out, otherwise it's created from the member's main branch. `gwt rm -k`/`--keep-branch` keeps each member's branch.

### Pass-through

These git worktree subcommands are forwarded directly:

```bash
gwt list                                 # list worktrees, marking the active one
gwt prune                                # git worktree prune
gwt lock <worktree>                      # git worktree lock
gwt unlock <worktree>                    # git worktree unlock
gwt move <worktree> <new-path>           # git worktree move
gwt repair                               # git worktree repair
```

`ls` is an alias for `list`. Bare `gwt list`/`gwt ls` marks the active worktree with `*` (green on a TTY); adding any flag (e.g. `--porcelain`) falls through to plain `git worktree list`. Unrecognized commands are rejected — only the above are passed through.

### AI Coding Assistants

`gwt` pairs well with agents like Claude Code or Codex that need isolated workspaces for parallel tasks. Drop this into your project's `CLAUDE.md` (or agent config):

> This project uses `gwt` (a git worktree wrapper). To work in an isolated worktree:
> - `gwt add <branch>` — create a worktree (prints the path)
> - `gwt add -b <new-branch>` — create a new branch in a worktree
> - `gwt ls` — list all worktrees and their paths
> - `gwt rm --keep-branch <branch>` — clean up without deleting the branch
> - `gwt rm <branch>` — clean up and delete the branch
>
> Worktree paths are printed to stdout. In bare-repo layouts, worktrees are sibling directories next to `.bare/`. Agents run commands via exec (not the shell wrapper), so capture the printed path instead of relying on auto-cd.

### Version

```bash
gwt version
```

## Development

Tooling and tasks run through [mise](https://mise.jdx.dev):

```bash
mise run build       # build a snapshot binary (goreleaser)
mise run test        # run the test suite
mise run lint        # run golangci-lint
mise run check       # lint + test + go mod tidy
mise run coverage    # HTML coverage report
mise run release     # interactive tag + publish
```

## Requirements

- **[mise](https://mise.jdx.dev)** (recommended — provides Go and the build tooling), or **Go 1.26+** if installing via `go install`
- **Git**
- **bash or zsh** (for shell integration and hook execution)

## License

[MIT](LICENSE)
