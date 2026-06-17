# gwt Workspaces — Design

**Date:** 2026-06-17
**Status:** Approved (pending spec review)

## Problem

Some codebases are split across two (or more) git repositories that are
mutually dependent and must be checked out as **sibling directories**. The
motivating case is `grafana` + `grafana-enterprise`:

- Each repo is independent (`grafana-enterprise` is not even a Go module).
- They integrate by **copying/symlinking enterprise source into the grafana
  working tree** at dev-setup time, gated by a hardcoded relative path
  `../grafana-enterprise`.
  - Enterprise `build.sh`/`install.sh` (run via `make enterprise-dev`, default
    `GRAFANA_DIR=../grafana`) copy `src/pkg/extensions/*` →
    `grafana/pkg/extensions/`, `src/public/*` → `grafana/public/app/extensions/`,
    wire files → `grafana/pkg/server/`, and symlink the enterprise Makefile into
    `grafana/local/Makefile`.
  - grafana keys its enterprise build off the *presence* of those injected files
    (`pkg/extensions/ext.go`, `public/app/extensions/index.ts`).
- **All injected paths are gitignored in grafana** — the integration lives only
  in the working tree, never in git.

This breaks `git worktree` (and therefore `gwt`) in two ways:

1. A fresh worktree only materializes tracked files, so the gitignored
   enterprise integration is absent → the worktree builds as OSS-only until the
   link step is re-run.
2. The `../grafana-enterprise` sibling assumption is false once worktrees are
   created under a different parent.

`gwt` today models **one repo → N worktrees** with a per-repo `post-checkout`
hook. It has no concept of a group of repos that must share a branch and be laid
out as siblings.

## Goal

Add a **workspace** concept so that running `gwt add -b feat/x` inside one member
repo (the primary) also creates sibling worktrees for the associated repos,
places them in a shared per-branch directory so relative paths resolve, and runs
a configurable cross-repo setup command (e.g. `make enterprise-dev`).

## Non-goals

- No auto-rollback of partially-created worktrees in v1.
- No `gwt workspace` management subcommand in v1 (config is hand-edited).
- No change to single-repo behavior when no workspace is configured.

## Decisions (from brainstorming)

| Topic | Decision |
|---|---|
| Scope | Create sibling worktrees **and** run a configurable setup command after all worktrees exist. |
| Follower branch | **Mirror** — followers get the same branch; create from their main, or check out if it already exists. |
| Layout | Per-branch parent dir with members as siblings; location is a configurable `worktree_root` defaulting to gwt's centralized dataDir. |
| Management | Hand-edit `[workspaces.<name>]` in `config.toml`. |
| Removal | Remove the whole group; per-member branch delete respecting `-k`/`--keep-branch`. |

## Design

### 1. Config schema

New top-level `[workspaces.<name>]` table. `[repos]` is unchanged and reused for
member paths / main branches.

```toml
[workspaces.grafana]
members       = ["grafana", "grafana-enterprise"]  # resolved against [repos]; order = creation order
primary       = "grafana"                            # cd target; followers mirror its branch
setup         = "make enterprise-dev"                # optional; runs after all worktrees exist
setup_cwd     = "grafana"                            # optional, default = primary; member whose worktree it runs in
worktree_root = "~/Development/grafana/code/.worktrees"  # optional, default = <dataDir>/worktrees/<workspace>
```

- **Member resolution:** each string matches a registered `[repos]` entry — exact
  canonical name (`grafana/grafana-enterprise`), else a **unique** last-segment
  match (`grafana-enterprise`). Ambiguous or missing → error *before* anything is
  created (e.g. `member "grafana-enterprise" not registered; run gwt init there`).
- **Worktree dir name** = the member's short last segment, so siblings are named
  `grafana` / `grafana-enterprise` and `../grafana-enterprise` resolves.
- `setup` is a single command string in v1; the schema leaves room to later
  accept a list of `{run, cwd}` steps.
- `~` in `worktree_root` is expanded.

Go types (in `config`):

```go
type WorkspaceEntry struct {
    Members      []string `toml:"members"`
    Primary      string   `toml:"primary,omitempty"`
    Setup        string   `toml:"setup,omitempty"`
    SetupCwd     string   `toml:"setup_cwd,omitempty"`
    WorktreeRoot string   `toml:"worktree_root,omitempty"`
}

type Config struct {
    Repos      map[string]RepoEntry      `toml:"repos"`
    Workspaces map[string]WorkspaceEntry `toml:"workspaces,omitempty"`
}
```

Helper: `Config.WorkspaceForRepo(canonicalName) (name string, ws WorkspaceEntry, ok bool)`
finds the workspace that lists the given repo as a member.

### 2. `gwt add` (workspace-aware fan-out)

1. Detect current repo → canonical name → find owning workspace. **None ⇒ existing
   single-repo path, untouched.**
2. Parse branch/flags by reusing `buildAddArgs`. Compute
   `group = <worktree_root>/<branchDir>` where `branchDir` replaces `/` with `-`.
3. Validate every member resolves to a `[repos]` entry. Fail here if any don't.
4. For each member **in order**:
   - **primary:** `git worktree add` with the user's args verbatim
     (`-b feat/x [start-point]`), run with `cmd.Dir =` the member repo dir,
     worktree path `<group>/grafana`.
   - **followers (mirror):** worktree at `<group>/<short>` on `feat/x`:
     - if `feat/x` exists locally or as `origin/feat/x` → `worktree add <path> feat/x`;
     - else → `worktree add -b feat/x <path> origin/<main_branch>`, where
       `main_branch` comes from the member's `[repos]` entry and defaults to
       `main` if unset.
     - Reuse the existing fetch-and-retry fallback on `invalid reference`.
5. **After all** worktrees exist, if `setup` is set: run it via the shell
   (`sh -c`) with `cmd.Dir = <group>/<setup_cwd>`.
6. `WriteCdFile(<group>/<primary>)`.

Each member's own `post-checkout` hook still fires on its checkout (existing
`copy_files` / `.env` behavior is preserved per repo); the workspace `setup`
layers the cross-repo link on top.

### 3. `gwt remove` (whole-group)

1. From the current worktree, confirm the repo is a workspace member;
   `group = filepath.Dir(currentWorktree)`.
2. For each member: remove `<group>/<short>` and delete its branch unless
   `-k`/`--keep-branch` (per-member, reusing current `Remove`).
3. `rmdir` the empty group dir; clean empty parents up to `worktree_root`.
4. `cd` back to the primary's **real** repo (`[repos][primary].path`).

If the current repo is not a workspace member, `remove` behaves exactly as today.

### 4. Error handling

- Validate all members resolve **before** creating anything.
- Fan-out failure: stop, report which worktrees were created, suggest `gwt rm` to
  unwind. No auto-rollback in v1.
- `setup` non-zero exit: report command + cwd, leave worktrees intact (setup is
  re-runnable).

### 5. Documentation

Add a concise (terse) `### Workspaces` section to `README.md` under `## Usage`
(after `### Use`). It must cover, minimally:

- One sentence on what a workspace is and the sibling-repo problem it solves.
- The `[workspaces.<name>]` config block with field meanings (1 line each).
- The grafana/grafana-enterprise example.
- That `gwt add` / `gwt rm` fan out across members automatically.

Keep it to roughly the density of the existing `### Clone` / `### Init` sections.

### 6. Testing

- **Unit:** `[workspaces]` parse; member resolution (canonical, short, ambiguous,
  missing); group/branch-dir path computation; follower ref decision
  (exists→checkout vs missing→create); `worktree_root` defaulting + `~` expansion;
  `WorkspaceForRepo`.
- **Integration:** temp git repos exercising add fan-out + group remove, using a
  trivial `setup` (e.g. `true` / `echo`). Follow existing test patterns in
  `main_test.go` and the `git` package tests.

### 7. Backward compatibility

No `[workspaces]` ⇒ identical behavior. Bare and standard modes both supported
(member worktrees are created in each member repo's own context, then placed in
the group dir). Non-member repos unaffected.
</content>
</invoke>
