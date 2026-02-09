# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`gwt` is a Go CLI tool that wraps `git worktree` commands. It acts as a pass-through to `git worktree` — any unrecognized command is forwarded directly. Two commands have enhanced behavior: `init` and `add`.

## Build & Run

```bash
go build -o gwt .       # Build the binary
go run .                 # Run without building
```

There are no tests in this project currently.

## Architecture

- **`main.go`** — CLI entry point using `cobra`. Registers `init` and `add` as subcommands; all other arguments are passed through to `git worktree` before cobra runs. Aliases: `ls` → `list`, `rm` → `remove`.
- **`git/git.go`** — Core logic. `Repo` struct holds the repo directory and bare-repo flag (auto-detected via `git rev-parse`). Provides `Passthrough` for forwarding args to `git worktree`, `Add` for worktree creation with path extraction, and helpers for fetch config, worktree lookup, and file copying.
- **`config/config.go`** — Configuration loading/saving. Manages `.gwt.json` in the repo root. Config is only written to disk when non-default values are set; removed if reset to defaults.

Key commands:
- `gwt init --main/-m <branch> --copy/-c <file>` — configure fetch (bare repos only) and save settings
- `gwt add [git worktree add flags] <path>` — create worktree and copy configured files
- `gwt <anything else>` — passed directly to `git worktree`

## File Copy Behavior

When `gwt add` creates a worktree, it loads `.gwt.json` config. If `copy_files` is configured, it finds the main branch worktree and copies those files into the new worktree. Missing files produce warnings but don't cause errors. If no config exists or `copy_files` is empty, no file copying occurs.
