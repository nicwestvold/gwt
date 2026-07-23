# Self-rendered `gwt ls` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make bare `gwt ls` self-rendered from the same structured model and renderer as `gwt ls -s`, at functional parity with the current output.

**Architecture:** Generalize the existing `renderSizedWorktreeList` into one `renderWorktreeTable` that omits the size column and total row when given `nil` sizes, then rewire `PrintWorktreeList` to build from `ListWorktreesFull` and call it — deleting the now-dead git-text decorator and its `git worktree list` exec.

**Tech Stack:** Go. Module `github.com/nicwestvold/gwt`. Touches only `git/git.go` and `git/git_test.go`.

## Global Constraints

- Functional parity, not byte-identical: same rows, same order, same annotations git shows, plus `gwt`'s `*`/green marker.
- SHA is the fixed 11-char `WorktreeInfo.SHA` (from `ListWorktreesFull`); no git auto-abbreviation.
- Full absolute paths (no `~` shortening).
- Columns: `path | [size] | sha | annotation`; the size column (sized mode only) sits 3rd-to-last, before the sha.
- Only bare `gwt ls`/`gwt list` and `gwt ls -s`/`--size` are self-rendered; every other flag passes through to git unchanged (no `main.go` interception change in this plan).
- Sized mode keeps binary IEC units and the `~` approximate marker.
- `ListWorktrees`/`parseWorktreeList` (used by `FindWorktreeByBranch` + completion) and `decorateLine` are unchanged.
- Commit messages: conventional, one line, NO attribution/co-author trailers.

---

## File Structure

- **Modify** `git/git.go` — replace `renderSizedWorktreeList` with generalized `renderWorktreeTable`; update `PrintSizedWorktreeList` and `PrintWorktreeList` to call it; delete `renderWorktreeList` and the plain `git worktree list` exec.
- **Modify** `git/git_test.go` — retarget the two sized-render tests to `renderWorktreeTable`, add bare-mode coverage, delete `TestRenderWorktreeList`.

---

## Task 1: Generalize the renderer to `renderWorktreeTable`

**Files:**
- Modify: `git/git.go` (replace `renderSizedWorktreeList` at git/git.go:586; update `PrintSizedWorktreeList` call at git/git.go:661)
- Test: `git/git_test.go` (retarget git/git_test.go:1108 and git/git_test.go:1134; add a bare-mode test)

**Interfaces:**
- Consumes: `WorktreeInfo` + `Annotation()`, `disk.Result`, `disk.Format`, `disk.FormatApprox`, `decorateLine` (all existing).
- Produces: `renderWorktreeTable(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string` — when `sizes == nil`, renders `path | sha | annotation` with no total row; when non-nil, renders `path | size | sha | annotation` + a `total` row. Replaces `renderSizedWorktreeList`.

- [ ] **Step 1: Write the failing test**

Replace `TestRenderSizedWorktreeList` (git/git_test.go:1108) and `TestRenderSizedColorActiveRow` (git/git_test.go:1134) with versions calling `renderWorktreeTable`, and add a bare-mode test:

```go
func TestRenderWorktreeTableSized(t *testing.T) {
	infos := []WorktreeInfo{
		{Path: "/repo/main", SHA: "27233475638", Branch: "main"},
		{Path: "/repo/feature-x", SHA: "00666edca69", Branch: "feature-x"},
	}
	sizes := []disk.Result{
		{Bytes: 4831838208},             // ~4.5 GiB
		{Bytes: 1288490188, Skipped: 2}, // ~1.2 GiB, approximate
	}
	out := renderWorktreeTable(infos, sizes, "/repo/main", false)

	if !strings.Contains(out, "* /repo/main") {
		t.Errorf("active marker missing:\n%s", out)
	}
	if !strings.Contains(out, "[main]") || !strings.Contains(out, "[feature-x]") {
		t.Errorf("branch annotations missing:\n%s", out)
	}
	if !strings.Contains(out, "~1.2 GiB") {
		t.Errorf("approximate marker missing on feature-x:\n%s", out)
	}
	if !strings.Contains(out, "total") || !strings.Contains(out, "~") {
		t.Errorf("total row wrong:\n%s", out)
	}
}

func TestRenderWorktreeTableSizedColorActiveRow(t *testing.T) {
	infos := []WorktreeInfo{{Path: "/repo/main", SHA: "abc", Branch: "main"}}
	sizes := []disk.Result{{Bytes: 1024}}
	out := renderWorktreeTable(infos, sizes, "/repo/main", true)
	if !strings.Contains(out, "\033[32m") {
		t.Errorf("expected green on active row:\n%q", out)
	}
}

func TestRenderWorktreeTableBare(t *testing.T) {
	const green = "\033[32m"
	const reset = "\033[0m"
	infos := []WorktreeInfo{
		{Path: "/repo/main", SHA: "27233475638", Branch: "main"},
		{Path: "/repo/wt-detached", SHA: "00666edca69", Detached: true},
		{Path: "/repo/bare", Bare: true},
		{Path: "/repo/locked-wt", SHA: "689fff37a9c", Branch: "feature", Locked: true},
	}

	// Bare mode: no size column, no total row.
	out := renderWorktreeTable(infos, nil, "/repo/main", false)
	if strings.Contains(out, "total") {
		t.Errorf("bare mode should have no total row:\n%s", out)
	}
	for _, want := range []string{"[main]", "(detached HEAD)", "(bare)", "[feature] locked"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing annotation %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "* /repo/main") {
		t.Errorf("active row not marked first:\n%s", out)
	}
	// Non-active rows are indented, not starred.
	if !strings.Contains(out, "  /repo/bare") {
		t.Errorf("bare row not indented:\n%s", out)
	}

	// Color gating on the active row.
	colored := renderWorktreeTable(infos, nil, "/repo/main", true)
	if !strings.Contains(colored, green+"* /repo/main") || !strings.Contains(colored, reset) {
		t.Errorf("active row not green:\n%q", colored)
	}

	// Empty input yields empty string (parity with old renderWorktreeList).
	if got := renderWorktreeTable(nil, nil, "", false); got != "" {
		t.Errorf("empty infos = %q, want \"\"", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./git/ -run 'TestRenderWorktreeTable' -v`
Expected: FAIL — `undefined: renderWorktreeTable`.

- [ ] **Step 3: Replace `renderSizedWorktreeList` with `renderWorktreeTable`**

In `git/git.go`, replace the whole `renderSizedWorktreeList` function (git/git.go:584-624) with:

```go
// renderWorktreeTable renders the worktree list as an aligned table with the
// active worktree marked. Columns are path | [size] | sha | annotation. When
// sizes is nil the size column and total row are omitted (bare `ls`);
// otherwise sizes[i] corresponds to infos[i] and a size column plus a total
// row are included (`ls -s`).
func renderWorktreeTable(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string {
	withSize := sizes != nil

	pathW := 0
	if withSize {
		pathW = len("total")
	}
	shaW := 0
	sizeW := 0
	sizeStrs := make([]string, len(infos))
	var totalBytes int64
	anyApprox := false
	for i, in := range infos {
		if len(in.Path) > pathW {
			pathW = len(in.Path)
		}
		if len(in.SHA) > shaW {
			shaW = len(in.SHA)
		}
		if withSize {
			sizeStrs[i] = disk.Format(sizes[i])
			if len(sizeStrs[i]) > sizeW {
				sizeW = len(sizeStrs[i])
			}
			totalBytes += sizes[i].Bytes
			if sizes[i].Skipped > 0 {
				anyApprox = true
			}
		}
	}

	totalStr := ""
	if withSize {
		totalStr = disk.FormatApprox(totalBytes, anyApprox)
		if len(totalStr) > sizeW {
			sizeW = len(totalStr)
		}
	}

	var b strings.Builder
	for i, in := range infos {
		var content string
		if withSize {
			content = fmt.Sprintf("%-*s  %*s  %-*s  %s",
				pathW, in.Path, sizeW, sizeStrs[i], shaW, in.SHA, in.Annotation())
		} else {
			content = fmt.Sprintf("%-*s  %-*s  %s",
				pathW, in.Path, shaW, in.SHA, in.Annotation())
		}
		active := activePath != "" && in.Path == activePath
		b.WriteString(decorateLine(strings.TrimRight(content, " "), active, color) + "\n")
	}
	if withSize {
		totalContent := fmt.Sprintf("%-*s  %*s", pathW, "total", sizeW, totalStr)
		b.WriteString(decorateLine(strings.TrimRight(totalContent, " "), false, color) + "\n")
	}
	return b.String()
}
```

Then update `PrintSizedWorktreeList` (git/git.go:661) to call the renamed function:

```go
	fmt.Print(renderWorktreeTable(infos, sizes, currentWorktreeTop(), shouldColor()))
```

Leave `renderWorktreeList` and `PrintWorktreeList` untouched in this task (still compiling and used).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./git/ -run 'TestRenderWorktreeTable' -v`
Expected: PASS (all three).
Then: `go test ./git/ -v` — the whole git package still passes (`TestRenderWorktreeList` still present and green, since `renderWorktreeList` is untouched).

- [ ] **Step 5: Commit**

```bash
git add git/git.go git/git_test.go
git commit -m "refactor(git): generalize worktree renderer for bare and sized modes"
```

---

## Task 2: Self-render bare `gwt ls`; delete the git-text path

**Files:**
- Modify: `git/git.go` (rewrite `PrintWorktreeList` at git/git.go:629; delete `renderWorktreeList` at git/git.go:563-582)
- Test: `git/git_test.go` (delete `TestRenderWorktreeList` at git/git_test.go:990)

**Interfaces:**
- Consumes: `ListWorktreesFull`, `renderWorktreeTable` (Task 1), `currentWorktreeTop`, `shouldColor` (existing).
- Produces: `PrintWorktreeList` now self-renders (same signature `() error`).

- [ ] **Step 1: Rewrite `PrintWorktreeList`**

Replace `PrintWorktreeList` (git/git.go:626-641, including its doc comment) with:

```go
// PrintWorktreeList prints the worktree list with the worktree containing the
// caller's current directory marked. It is self-rendered from
// `git worktree list --porcelain` (via ListWorktreesFull) at functional parity
// with git's plain output. Color is enabled only on a terminal and when
// NO_COLOR is unset.
func (r *Repo) PrintWorktreeList() error {
	infos, err := r.ListWorktreesFull()
	if err != nil {
		return err
	}
	fmt.Print(renderWorktreeTable(infos, nil, currentWorktreeTop(), shouldColor()))
	return nil
}
```

- [ ] **Step 2: Delete the dead git-text decorator**

Delete the entire `renderWorktreeList` function together with its doc comment (git/git.go:563-582). It has no remaining callers after Step 1.

- [ ] **Step 3: Delete the obsolete test**

Delete `TestRenderWorktreeList` (git/git_test.go:990-1052) — it feeds git-style plain text to the now-deleted `renderWorktreeList`. Its intent (active-row marking + color gating) is already covered by `TestRenderWorktreeTableBare` from Task 1.

- [ ] **Step 4: Build, test, vet**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: all PASS, output pristine. (If the compiler flags an unused `bytes`/`exec` import in `git.go`, remove only the now-unused one — but both remain used by other functions such as `ListWorktreesFull`, so no import change is expected.)

Manual check in this repo:
```bash
go build -o /tmp/gwt-dev . && /tmp/gwt-dev ls && echo "---" && /tmp/gwt-dev ls -s
```
Expected: `ls` shows `path  sha  [branch]` with the active row marked (green on a TTY); `ls -s` shows the same with a size column + total row. Both use identical column alignment.

- [ ] **Step 5: Commit**

```bash
git add git/git.go git/git_test.go
git commit -m "feat(list): self-render bare gwt ls from structured data"
```

---

## Self-Review

**Spec coverage:**
- Unified renderer with nil-sizes bare mode → Task 1. ✓
- Size column 3rd-to-last, total row in sized mode only → Task 1 (`renderWorktreeTable`). ✓
- Bare `ls` self-rendered from `ListWorktreesFull` → Task 2. ✓
- Delete `renderWorktreeList` + plain `git worktree list` exec → Task 2. ✓
- Row-type parity (branch/detached/bare/locked/prunable), active marker, color gating, empty input → Task 1 tests. ✓
- Fixed 11-char SHA, full paths → inherited from `WorktreeInfo.SHA` + `renderWorktreeTable` (no truncation/shortening added). ✓
- No `main.go` interception change → neither task touches `main.go`. ✓
- `ListWorktrees`/`parseWorktreeList`/`decorateLine` untouched → confirmed, no task modifies them. ✓

**Placeholder scan:** No TBDs; every code step contains complete code.

**Type consistency:** `renderWorktreeTable(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string` is defined in Task 1 and consumed identically by `PrintSizedWorktreeList` (Task 1) and `PrintWorktreeList` (Task 2). `disk.Result`/`disk.Format`/`disk.FormatApprox` and `WorktreeInfo.Annotation()` match their existing definitions.
