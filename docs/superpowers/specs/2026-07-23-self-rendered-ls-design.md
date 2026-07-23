# Self-rendered `gwt ls` (own the bare worktree list)

**Date:** 2026-07-23
**Status:** Approved

## Problem

`gwt ls -s` is self-rendered from structured data, but bare `gwt ls` still
shells out to `git worktree list` (plain text) and only post-decorates each
line with the active-worktree marker (`renderWorktreeList`). This leaves two
rendering paths that can drift, and bare `ls` remains coupled to git's exact
text format.

Goal: make bare `gwt ls` "ours" — self-rendered from the same structured model
and the same renderer as `ls -s` — with **functional parity** to the current
output.

## Definitions

**Functional parity** (the chosen bar): every row, SHA, and annotation git
shows in plain `git worktree list` — same rows, same order, same information —
plus `gwt`'s existing `*`/green active-row marker, rendered in our own aligned
table. It is NOT byte-for-byte identical to git: SHAs render at our fixed
11-char width (not git's auto-computed abbreviation) and column spacing is ours.

## Goals

- Bare `gwt ls` self-rendered from `ListWorktreesFull`, sharing one renderer
  with `ls -s`.
- One column-alignment code path so `ls` and `ls -s` cannot visually drift.
- Remove the now-dead git-text path.

## Non-goals

- No byte-identical reproduction of git's output (see Functional parity).
- No home-directory (`~`) path shortening — full absolute paths, as today.
- No change to any non-bare / non-`-s` invocation: `--porcelain`, `-v`/
  `--verbose`, `-z`, `--expire`, and every other flag continue to pass through
  to git untouched. We do not reimplement git's other list modes.

## Scope of interception (unchanged in shape)

`main.go`'s pre-cobra pass-through block keeps its current routing:

- `gwt ls` / `gwt list` (bare) → `PrintWorktreeList` (now self-rendered).
- `gwt ls -s` / `--size` → `PrintSizedWorktreeList`.
- Anything else (`--porcelain`, `-v`, `-z`, `--expire`, …) → `Passthrough` to
  git, verbatim.

Only the *implementation* of `PrintWorktreeList` changes; the interception logic
does not.

## The unified renderer

Generalize the existing `renderSizedWorktreeList` into one function:

```go
// renderWorktreeTable renders the worktree list as an aligned table with the
// active worktree marked. When sizes is nil, the size column and total row are
// omitted (bare `ls`); when non-nil, sizes[i] corresponds to infos[i] and a
// size column + total row are included (`ls -s`).
func renderWorktreeTable(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string
```

- **Columns:** `path | [size] | sha | annotation`. The size column appears only
  when `sizes != nil`, in the 3rd-to-last position (before the sha), matching
  the established `ls -s` order. Bare `ls` → `path | sha | annotation`;
  `ls -s` → `path | size | sha | annotation`.
- Column widths (path, size, sha) computed in one pass so both modes align
  identically.
- Each row decorated via the existing `decorateLine` (the `*`/green active-row
  logic) — shared, unchanged.
- `total` row emitted only in sized mode, aligned under the size column.
- SHA is the fixed 11-char `WorktreeInfo.SHA` from `ListWorktreesFull`. A
  bare-repo row has an empty SHA cell and `(bare)` in the annotation column.

`PrintSizedWorktreeList`'s call becomes
`renderWorktreeTable(infos, sizes, currentWorktreeTop(), shouldColor())`.

## Rewiring bare `ls`

`PrintWorktreeList` becomes:

```go
func (r *Repo) PrintWorktreeList() error {
	infos, err := r.ListWorktreesFull()
	if err != nil {
		return err
	}
	fmt.Print(renderWorktreeTable(infos, nil, currentWorktreeTop(), shouldColor()))
	return nil
}
```

**Deletions** (dead once bare `ls` self-renders):

- `renderWorktreeList` — the git-text line decorator.
- The plain `git worktree list` exec that fed it (inside the old
  `PrintWorktreeList`).

**Unchanged:** `ListWorktrees`/`parseWorktreeList` (still used by
`FindWorktreeByBranch` and shell completion), `decorateLine`, and the `main.go`
interception shape.

## Edge cases & behavior parity

- **Bare-only repo** (fresh `gwt clone`, no worktrees yet): a single `(bare)`
  row, empty SHA cell, no total row. Verified against real git output.
- **Detached HEAD:** `(detached HEAD)` annotation, SHA present.
- **Locked / prunable:** short annotations appended (`[branch] locked`,
  `[branch] prunable`), matching git's non-verbose form. Reasons are not shown
  (those are `-v`-only, which passes through to git).
- **Active-row marking:** `currentWorktreeTop()` unchanged; matching row gets
  `* ` + green (TTY, `NO_COLOR` unset). Outside any worktree, no row marked.
- **Ordering:** `ListWorktreesFull` preserves porcelain order (main first, then
  git's order) — same rows, same order as git.
- **Non-TTY / piped:** no color; `*`/indent still applied — as today.
- **`ListWorktreesFull` error:** propagated; `main.go` prints `error: …` to
  stderr and exits via `git.ExitCode(err)` — same as today's bare-list error
  path.
- **Paths with spaces:** porcelain-sourced, handled correctly — a latent
  improvement over parsing git's space-delimited plain text.

## Testing

- **Renderer, both modes (table-driven):**
  - `sizes == nil`: `path | sha | annotation`, no size column, no total row.
  - `sizes != nil`: size column in 3rd-to-last slot + total row (existing
    `TestRenderSizedWorktreeList` coverage, retargeted to the renamed function).
  - Bare-mode row-type parity: branch, `(detached HEAD)`, `(bare)` (empty SHA
    cell), `locked`, `prunable` all render correct annotations and stay aligned.
  - Active row: `* ` present; green when `color=true`, absent when `false`.
  - Alignment holds with varied path/SHA lengths and the bare row's empty SHA.
  - Approximate `~` marker still works in sized mode.
- **Replace `TestRenderWorktreeList`:** the git-text-fed test is removed; its
  intent (active-row marking, color gating) is re-expressed against
  `renderWorktreeTable` with `WorktreeInfo` inputs.
- **Parsing:** `parseWorktreeListFull`/`ListWorktreesFull` already covered; no
  new parsing tests unless a gap surfaces.
- **Gate:** `go build ./... && go test ./... && go vet ./...` clean; manual
  `gwt ls` and `gwt ls -s` (this repo and a multi-worktree repo) to eyeball
  parity and the active marker.
