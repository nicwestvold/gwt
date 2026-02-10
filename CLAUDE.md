# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`gwt` is a Go CLI tool that wraps `git worktree` commands. It acts as a pass-through to `git worktree` — any unrecognized command is forwarded directly. Three commands have custom behavior: `clone`, `init`, and `add`.

## Build & Test

```bash
go build -o gwt .       # Build the binary
go run .                 # Run without building
go test ./...            # Run all tests
go vet ./...             # Run static analysis
```

## Architecture

- **`main.go`** — CLI entry point using `cobra`. Registers `clone`, `init`, and `add` as subcommands; all other arguments are passed through to `git worktree` before cobra runs. Aliases: `ls` → `list`, `rm` → `remove`.
- **`git/git.go`** — Core logic. `Repo` struct holds the repo directory and bare-repo flag (auto-detected via `git rev-parse`). Provides `Passthrough` for forwarding args to `git worktree`, `Add` for worktree creation with path extraction, `Clone` for bare-repo setup, and helpers for fetch config and hooks directory lookup.
- **`hook/`** — Hook generation. `HookData` struct + Go template produce a `post-checkout` shell script. Installed via `hook.Install()`.

Key commands:
- `gwt clone <repo> [<dir>]` — clone into a bare-repo worktree structure and configure fetch. Only installs the post-checkout hook if init flags (`--main`, `--copy`, `--no-copy`, `--version-manager`, `--package-manager`) are explicitly provided; otherwise run `gwt init` afterward
- `gwt init --main/-m <branch> --copy/-c <file> --no-copy` — generate a post-checkout hook (configures fetch in bare repos)
- `gwt add [git worktree add flags] <path>` — create worktree (setup handled by post-checkout hook)
- `gwt <anything else>` — passed directly to `git worktree`
