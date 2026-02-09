# gwt

A convenience wrapper around `git worktree` that reduces friction when working with worktrees.

## Why?

`git worktree` is powerful but has rough edges — especially with bare repos where `git fetch` doesn't work out of the box, and there's no built-in way to carry over environment files (`.env`, etc.) when creating new worktrees.

`gwt` fixes this by acting as a transparent pass-through to `git worktree` with two additions:

- **`gwt init`** — configures `git fetch` for bare repos and sets up which files to copy into new worktrees
- **`gwt add`** — creates a worktree (same as `git worktree add`) and automatically copies configured files from your main branch worktree

Everything else (`gwt list`, `gwt remove`, etc.) is passed directly to `git worktree`.

## Getting Started with Worktrees

Worktrees work best with a **bare clone**. Instead of a normal `git clone`, run:

```bash
git clone --bare git@github.com:you/your-repo.git your-repo
cd your-repo
```

This gives you a repo with no checked-out working directory — each worktree you create becomes its own isolated working directory. Then set up `gwt`:

```bash
gwt init --copy .env
gwt add main        # create a worktree for your main branch
gwt add my-feature  # start working on a feature
```

You'll end up with a structure like:

```
your-repo/            # bare repo (git internals)
├── main/             # worktree for main branch
│   └── .env          # copied automatically
└── my-feature/       # worktree for feature branch
    └── .env          # copied automatically
```

Each worktree is a full working directory with its own branch checked out, so you can switch between tasks without stashing or committing in-progress work.

## Install

```bash
go install github.com/nicwestvold/gwt@latest
```

## Usage

```bash
# Initialize (configures fetch for bare repos, sets files to copy)
gwt init --copy .env --copy .env.local

# Create a worktree — .env and .env.local are copied automatically
gwt add my-feature

# All other commands pass through to git worktree
gwt list
gwt ls          # alias for list
gwt remove my-feature
gwt rm my-feature  # alias for remove
```

### Configuration

`gwt init` saves a `.gwt.json` in your repo root when non-default values are set:

```json
{
  "main_branch": "main",
  "copy_files": [".env", ".env.local"]
}
```

| Flag | Description |
|------|-------------|
| `--main, -m` | Main branch name (default `main`) |
| `--copy, -c` | Files to copy into new worktrees (repeatable) |

### Bare repo setup

When run inside a bare repo, `gwt init` automatically configures `remote.origin.fetch` so that `git fetch` works properly:

```
git config remote.origin.fetch "+refs/heads/*:refs/remotes/origin/*"
```
