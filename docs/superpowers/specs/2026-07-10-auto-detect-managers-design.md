# Auto-detect version & package managers for `gwt init` / `gwt clone`

**Date:** 2026-07-10
**Status:** Approved

## Problem

Generating a working post-checkout hook requires the user to know and pass the
version manager (`-v mise`/`asdf`) and package manager (`-p pnpm`/`npm`/`yarn`).
Almost every repo already declares these through files on disk (`mise.toml`,
`pnpm-lock.yaml`, a `package.json` `packageManager` field, …). Requiring the
flags is friction: the user has to restate what the repo already says.

Goal: `gwt init -c .env` should detect the managers automatically and fold them
into the hook, and a new `-w`/`--with-hook` flag should generate a hook purely
from what it detects.

## Behavior

### New flag

`-w` / `--with-hook` on both `init` and `clone`: run detection and generate a
hook. If detection finds neither a version manager nor a package manager **and**
no `-c` files were given, no hook is generated (see the "something to do" rule).

### Detection triggers

Detection runs whenever a hook is going to be generated — i.e. when any of
`-c`/`--copy`, `-p`/`--package-manager`, `-v`/`--version-manager`, or
`-w`/`--with-hook` is present. So:

- `gwt init -c .env` copies `.env` **and** auto-fills VM + PM.
- `gwt init -w` generates a hook from detection alone.
- `gwt clone <repo> -w` clones, then detects from the fetched main branch tree.

**Explicit flags always win.** Detection only fills a dimension the user did not
specify: `gwt init -c .env -v asdf` detects the package manager but keeps
`asdf` for the version manager (no VM detection message).

### "Something to do" rule

A hook is generated only if it has real work: at least one copy file, or a
version manager, or a package manager (explicit or detected). This makes
`-w` with nothing detected a clean no-op, and it also stops
`gwt clone <repo> -m develop` (alone) from writing today's empty no-op hook.
This is the only behavioral change beyond the new flag.

### Config

Detected values are written into the registered config entry (via
`registerRepo`), so the stored `RepoEntry` matches the generated hook. Detection
therefore runs *before* `registerRepo` on the hook path.

## Detection

### Source abstraction

Detection must work before any worktree is checked out. After `gwt clone` a bare
repo has fetched branches but no working tree, so scanning `repoBasePath` finds
nothing. A new `detect` package abstracts the lookup behind a small interface:

```go
type FileSource interface {
    Exists(path string) bool
    Read(path string) ([]byte, error)
}
```

Two implementations:

- `DirSource{Root string}` — `os.Stat` / `os.ReadFile` under a directory. Used
  when the base path exists (non-bare repos, or a bare repo whose main worktree
  is already checked out).
- `GitSource{RepoDir, Ref string}` — reads the main branch tree without a
  checkout: `Exists` via `git -C RepoDir cat-file -e <Ref>:<path>` (exit 0),
  `Read` via `git -C RepoDir show <Ref>:<path>`. `Ref` comes from
  `git.MainBranchRef(repoDir, mainBranch)` (`origin/<main>` or `<main>`).

The caller (in `main.go`) picks the source:

- if `repoBasePath(repo, mainBranch)` exists on disk → `DirSource` rooted there;
- else → `GitSource` for the main branch ref.

Detection logic is pure over `FileSource` and unit-tested with a fake source.

### API

```go
package detect

type Result struct {
    VersionManager string // "", "mise", "asdf"
    PackageManager string // "", "pnpm", "npm", "yarn"
}

// Detect fills only the dimensions not already set. detectVM also consults the
// host PATH (via a lookPath func, injected for tests) to disambiguate.
func Detect(src FileSource, lookPath func(string) (string, error)) Result
```

`main.go` calls `Detect` with `exec.LookPath`, then applies each result field
only where the corresponding flag was not `Changed`.

### Version-manager rules

1. `mise.toml` OR `.mise.toml` OR `.config/mise/config.toml` exists → `mise`.
2. Else `.tool-versions` exists → consult PATH:
   - `mise` on PATH → `mise`
   - else `asdf` on PATH → `asdf`
   - else `""` (skip; ambiguous config, neither tool installed)
3. Else `""`.

### Package-manager rules

1. `package.json` exists and its top-level `"packageManager"` field parses to a
   name (portion before `@`) in {`pnpm`, `npm`, `yarn`} → that name. An
   unsupported name (e.g. `bun`) is ignored and detection falls through.
2. Else first lockfile found, by priority: `pnpm-lock.yaml` → `yarn.lock` →
   `package-lock.json`.
3. Else `""`.

`package.json` is parsed by unmarshalling just `{"packageManager": "..."}`.

## Messaging

On each auto-detected dimension (only for dimensions the user did not set), to
stdout:

```
auto-detected mise, adding to hook
auto-detected pnpm, adding to hook
```

After a hook is installed when at least one value was auto-detected:

```
If auto-detection got it wrong, re-run with explicit flags, e.g. gwt init -f -v asdf -p yarn
```

(For `clone`, the same note uses the `gwt init -f …` form, since re-running is
done from inside the repo with `init`.)

When `-w` (or a hook-triggering flag with no `-c` files) resolves to nothing to
do:

```
no version or package manager detected — no hook generated (use -c to copy files, or -v/-p to set them manually)
```

This is a normal (non-error) exit.

## Changes

### New: `detect/detect.go`

`FileSource` interface, `DirSource`, `GitSource`, `Result`, `Detect`, and the
pure `detectVersionManager` / `detectPackageManager` helpers.

### `main.go`

- Add `--with-hook`/`-w` bool flag to both `initCmd` and `cloneCmd`.
- Factor a helper that, given a `*git.Repo`, `mainBranch`, and the current
  `hookOptions`, builds the right `FileSource`, runs `detect.Detect`, and fills
  `opts.versionManager` / `opts.packageManager` for unset dimensions — printing
  the `auto-detected …` messages. Returns the possibly-updated opts plus a
  `detectedAny bool`.
- `initCmd`: `wantHook` gains `Changed("with-hook")`. When `wantHook`, run
  detection before `registerRepo`. Replace the direct `setupHook` call with the
  "something to do" guard; if nothing to do, print the no-hook message and
  return nil. After a successful install, print the fix-it note when
  `detectedAny`.
- `cloneCmd`: add `with-hook` to the `initFlags` trigger list; run detection
  after `git.Clone` (base path won't exist yet → `GitSource`); apply the same
  guard, install, and fix-it messaging.

### `git` package

`MainBranchRef` is already exported and reused as-is. No other git changes.

## Testing

### `detect` package (`detect/detect_test.go`)

Table tests against a fake `FileSource` (map of path→contents) and a fake
`lookPath`:

- VM: each mise-specific file → mise; `.tool-versions` + mise on PATH → mise;
  `.tool-versions` + only asdf on PATH → asdf; `.tool-versions` + neither → "";
  none → "".
- PM: `packageManager: "pnpm@8"` → pnpm; unsupported (`bun@1`) falls through to
  lockfile; each lockfile alone → its manager; multiple lockfiles → priority
  order; none → "".
- Combined `Detect` result for a representative repo (mise + pnpm).
- `DirSource` and `GitSource` are thin adapters; cover `DirSource` against a
  temp dir. `GitSource` behavior is covered indirectly (kept minimal).

### `main.go` flow

Where practical without a full git fixture, assert the fill logic: explicit flag
wins over detection; `detectedAny` drives the fix-it note; the "something to do"
guard skips hook creation when everything is empty.

## Out of scope

- Detecting bun/deno or other managers not already supported.
- Changing the existing `corepack enable`-for-npm behavior.
- Any change to how the hook template renders VM/PM (unchanged).
</content>
