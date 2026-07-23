# Worktree disk-size reporting & parallel workspace removal

**Date:** 2026-07-23
**Status:** Approved

## Problem

`gwt` has no notion of how much disk a worktree occupies. Two places would
benefit:

1. **`gwt rm`** currently prints nothing on success. When a worktree (or a whole
   workspace branch group) is torn down, the user has no sense of how much space
   was reclaimed.
2. **`gwt ls`** shows worktrees but not their footprint, so there's no way to
   see which worktree is eating disk.

We want to add on-disk size reporting to both, computed fast enough that it
never makes these commands feel sluggish — explicitly **faster than `du`** on
the common case. While in the removal path, we also want to parallelize
workspace teardown, which is currently sequential.

## Goals

- A single, tested primitive that measures a directory tree's on-disk size,
  beating `du` by walking concurrently.
- `gwt rm` reports freed space on success.
- `gwt ls -s` / `--size` shows a size column plus a total, with row accuracy
  identical to bare `gwt ls`.
- Workspace `gwt rm` removes member worktrees concurrently (best-effort).

## Non-goals

- No workspace-wide aggregate view in `ls` (it is per-repo, as today).
- No forced unification of bare-`ls` rendering onto a self-rendered path (that
  is a possible follow-up, not part of this feature).
- No Windows support for block-based sizing (not a current target).

## The `disk` package (core primitive)

New package `disk/` exposing:

```go
package disk

// Size returns the on-disk size (allocated blocks) of the tree rooted at path,
// in bytes, walking concurrently. Best-effort: unreadable entries and files
// that vanish mid-walk are skipped and counted in Result.Skipped.
func Size(root string) (Result, error)

type Result struct {
    Bytes   int64 // sum of Stat_t.Blocks * 512 across regular files
    Skipped int   // entries that could not be stat'd (permission, vanished)
}
```

**Mechanics:**

- A bounded worker pool pulls directory paths from a work queue. Concurrency is
  a single tunable constant, `walkConcurrency`, defaulting to
  `runtime.NumCPU()`.
- Each worker `os.ReadDir`s its directory, sums
  `info.Sys().(*syscall.Stat_t).Blocks * 512` for regular files, and enqueues
  subdirectories.
- **On-disk blocks, not apparent size** — matches `du` and reports true
  reclaimed space (correct for sparse files and block rounding).
- **Symlinks are not followed** — counted as the link itself. No cycle risk, no
  double-counting.
- **Sum-and-discard** — file entries are never retained; only pending directory
  paths accumulate, so memory does not scale with file count.

**Resource profile:** goroutines, not subprocesses — no `fork`, nothing that can
fork-bomb the machine. Peak footprint is a handful of worker stacks (KB each)
plus a small pending-directory queue: single-digit MB in normal use. Disk I/O is
the limiter; the bounded pool prevents saturating it.

**Platform note:** `Stat_t.Blocks` is Unix (macOS + Linux, the tool's targets).
If Windows ever becomes a target, a fallback to apparent size goes behind build
tags.

## `rm` / `remove` integration

### Single-repo `gwt rm`

- Measure `disk.Size(worktreePath)` **before** removal (files are gone after).
- On successful `git worktree remove`, print:

  ```
  removed worktree feature-x — freed 4.8 GiB
  ```

- If sizing returns a hard error, removal still proceeds and the line omits the
  freed clause (`removed worktree feature-x`). Sizing never blocks a removal.

### Workspace `gwt rm` (parallelized)

Each workspace member is a separate repo with its own `.git`, so member
removals share no git state and are safe to run concurrently.

- For each member, run **concurrently** (bounded): measure its worktree size,
  then remove it. Measurement thus parallelizes across members *and* within each
  tree.
- **Best-effort**: every member attempts removal even if another fails. Collect
  per-member results — freed bytes, removal error (if any), and whether the
  branch was left unmerged.
- Print one aggregate line plus consolidated notes:

  ```
  removed workspace group feature-x (3 repos) — freed 4.8 GiB
  note: branch "feature-x" kept (not fully merged) in: api, web
        delete with: git -C <repo> branch -D feature-x
  ```

- On partial failure:

  ```
  removed 2/3 repos — freed 4.5 GiB
    ! web: worktree contains modified files (use -f)
  ```

  and exit non-zero.

- The consolidated unmerged-branch note **replaces** the current scattered
  per-member `warning:` lines. `RemoveMemberWorktree` (git/workspace.go:68)
  changes shape: instead of printing warnings itself, it returns structured info
  (freed bytes, branch-kept status, error) to the caller, which aggregates.

**Force semantics (unchanged, for reference):** `-f` maps to
`git worktree remove --force` (removes a dirty worktree); branch deletion is
always the safe `git branch -d`, which keeps an unmerged branch and is the
source of the "branch kept" note.

## `list` / `ls -s` integration

### Flag interception

`gwt list` is handled specially in `main.go` (intercepted at the bare
`len(os.Args) == 2` case, main.go:931), not as a cobra subcommand. Extend that
interception to also recognize `gwt ls -s`, `gwt list -s`, `--size`.

- Bare `gwt ls` (no `-s`) is **unchanged** — git text + our line decoration.
- Any other flags (e.g. `--porcelain`) still fall through to plain git untouched,
  preserving scripting behavior.

### Rendering

The `-s` variant is **self-rendered from structured data**, because inserting an
aligned column into git's pre-formatted text is fragile.

- Columns: `path | size | HEAD-sha | [branch]` — size sits **before the sha**
  (3rd-to-last column), right-aligned. Append a `total` row aligned under the
  size column.

  ```
    /repo/.../build-migrate-to-pnpm/grafana   1.2 GiB   00666edca69   [build/migrate-to-pnpm]
  * /repo/.../governance-.../grafana          4.8 GiB   3249c5ff7a0   [governance/...]
    total                                     6.9 GiB
  ```

- Size for each worktree computed via `disk.Size`, run **concurrently** across
  worktrees (same bounded primitive).
- Active-row `*` marker and green (on a TTY, `NO_COLOR` unset) preserved,
  identical to bare `ls`. The per-line decoration (marker + optional green) is
  factored out of `renderWorktreeList` (git/git.go:534) into a shared helper so
  both paths decorate rows identically; the active row is fully green including
  the new size cell, as today.

### Row accuracy (parity with bare `ls`)

`ls -s` **must show the same set of rows and annotations** as bare `ls` — every
worktree plus `(detached HEAD)`, `(bare)`, and `locked`/`prunable` flags — with
only the size column added.

The current `ListWorktrees` / `parseWorktreeList` (git/git.go:494, 506) is lossy:
it keeps only entries with a branch (`if current.Branch != ""`), dropping
detached/bare rows and lock/prune flags. The `-s` renderer therefore needs a
**complete** parse of `git worktree list --porcelain`, capturing Path,
abbreviated HEAD sha, branch **or** detached/bare state, and lock/prune
annotations.

This must be done **without changing** the existing `ListWorktrees` behavior
that `FindWorktreeByBranch` (git/git.go:602) and shell completion depend on
(branch-only filtering). Implement as a new richer function (or additive fields
with existing callers retaining their filter) — the non-breaking route, covered
by tests.

Size for a `(bare)` or detached row is just the on-disk size of that path; no
special-casing.

## Number format

- **Binary IEC** throughout: `KiB` / `MiB` / `GiB` (powers of 1024). Matches the
  block math and is unambiguous.
- Shared formatting helper used by both `rm` and `ls -s`.

## Approximate-size marker

When a size is a lower bound (the walk skipped unreadable entries,
`Result.Skipped > 0`), prefix the number with `~` ("approximately"):

- `ls -s`: `~1.2 GiB` on the affected row; the `total` row gets a leading `~` if
  **any** contributing row was approximate.
- `rm`: `freed ~1.2 GiB` when the pre-removal walk skipped anything.

## Error handling & edge cases

**Walk-level (`disk.Size`):**

- Unreadable dir/file (permission denied): skip, increment `Skipped`, continue.
- File vanishes mid-walk (`ENOENT` on stat): skip, not an error.
- Symlinks: counted as the link, never followed.
- Cross-filesystem / mount points: counted normally (no `du -x` semantics); a
  worktree is expected to be one tree. Documented, not special-cased.

**`rm`:** sizing error → removal proceeds, freed clause omitted. Partial
workspace failure → best-effort, `N/M` summary, per-failure `!` lines, non-zero
exit.

**`ls -s`:** a worktree with a partial walk still shows its best-effort size
(marked `~`); the listing never aborts.

## Testing

**`disk` package (heaviest coverage):**

- Temp trees with known sizes; assert `Result.Bytes` matches expected block sums.
- Nested/empty dirs, many small files (exercises the concurrent queue).
- Symlink counted as link, not followed; symlink cycle terminates.
- Unreadable dir (chmod `000`) → skipped, `Skipped` incremented, walk completes
  (guarded/skipped when running as root).
- Cross-check against `du -sk` within block rounding (skip if `du` absent).
- Determinism: same tree → same total regardless of concurrency.

**Rendering:**

- Table renders aligned `path | sha | size | [branch]`; `total` aligns under
  size; active-row `*`/green preserved (color on and off).
- Detached/bare/locked/prunable rows appear in `-s` output (parity with git),
  table-driven against porcelain fixtures.
- `~` prefixes an under-counted row and propagates to `total`.

**`rm` integration:**

- Single-repo: freed line correct; sizing error → removal succeeds, clause
  omitted.
- Workspace: aggregate line + combined total; consolidated unmerged-branch note
  replaces per-member warnings; partial failure → `N/M`, non-zero exit.
- Parallelism correctness: N members removed, results aggregated regardless of
  completion order.

**main.go flag interception:** `gwt ls -s`, `gwt list --size` trigger the sized
path; bare `ls` and `ls --porcelain` unchanged.
