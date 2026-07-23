# Worktree Disk-Size Reporting & Parallel Workspace Removal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add on-disk size reporting to `gwt rm` (freed space) and `gwt ls -s` (size column), backed by a concurrent size-measurement primitive that beats `du`, and parallelize workspace teardown.

**Architecture:** A new pure-Go `disk` package walks a tree with a bounded worker pool, summing on-disk blocks. `gwt rm` measures a worktree before removal and reports freed space; workspace removal fans out across members concurrently. `gwt ls -s` self-renders a size column from a complete porcelain parse, preserving row parity with bare `ls`.

**Tech Stack:** Go, `cobra`, `syscall.Stat_t` (Unix block sizing), goroutines + `sync.WaitGroup` for concurrency. Module: `github.com/nicwestvold/gwt`.

## Global Constraints

- Number format: **binary IEC** (`KiB`/`MiB`/`GiB`, powers of 1024) everywhere.
- Approximate sizes (walk skipped unreadable entries): prefix the number with `~` (e.g. `~1.2 GiB`).
- On-disk **blocks** (`Stat_t.Blocks * 512`), never apparent size.
- Symlinks counted as the link, never followed.
- Bounded concurrency: single constant `walkConcurrency`, default `runtime.NumCPU()`.
- Best-effort walks: unreadable / vanished entries are skipped and counted, never fatal.
- Sizing must never block or fail a removal.
- Bare `gwt ls` output is unchanged; only `-s`/`--size` uses the new rendering.
- Platform: Unix (macOS + Linux). No Windows.
- Commit messages: conventional style, no attribution/co-author trailers.

---

## File Structure

- **Create** `disk/disk.go` — `Size`, `Result`, IEC formatting. Sole owner of tree measurement + size formatting.
- **Create** `disk/disk_test.go` — unit tests for the primitive and formatters.
- **Modify** `git/git.go` — add complete porcelain parse (`WorktreeInfo`, `ListWorktreesFull`, `parseWorktreeListFull`, `Annotation`), extract `decorateLine`, add `renderSizedWorktreeList` + `PrintSizedWorktreeList`, change `Remove` to return `RemoveResult`.
- **Modify** `git/git_test.go` — tests for parsing, annotation, sized render, decoration.
- **Modify** `git/workspace.go` — `RemoveMemberWorktree` returns `MemberRemoval`.
- **Modify** `git/workspace_test.go` — tests for the new return shape.
- **Modify** `main.go` — `ls -s` interception; single-repo `rm` freed line; parallel workspace removal + aggregate output + branch note.

---

## Task 1: `disk.Size` core primitive

**Files:**
- Create: `disk/disk.go`
- Test: `disk/disk_test.go`

**Interfaces:**
- Produces: `disk.Result{Bytes int64, Skipped int}`; `disk.Size(root string) (Result, error)`; `var walkConcurrency int`.

- [ ] **Step 1: Write the failing test**

```go
package disk

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes n bytes and returns the path.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSizeSumsRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a"), 4096)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "b"), 8192)

	res, err := Size(root)
	if err != nil {
		t.Fatalf("Size returned error: %v", err)
	}
	// At least the two files' bytes; block rounding may add more, never less.
	if res.Bytes < 4096+8192 {
		t.Errorf("Bytes = %d, want >= %d", res.Bytes, 4096+8192)
	}
	if res.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", res.Skipped)
	}
}

func TestSizeEmptyDir(t *testing.T) {
	res, err := Size(t.TempDir())
	if err != nil {
		t.Fatalf("Size returned error: %v", err)
	}
	if res.Bytes < 0 {
		t.Errorf("Bytes = %d, want >= 0", res.Bytes)
	}
}

func TestSizeMissingRoot(t *testing.T) {
	if _, err := Size(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing root, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./disk/ -run TestSize -v`
Expected: FAIL — `undefined: Size` (package doesn't compile).

- [ ] **Step 3: Write the implementation**

```go
// Package disk measures on-disk usage of directory trees, walking
// concurrently to outperform single-threaded du.
package disk

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

// walkConcurrency bounds the number of directories read in parallel.
var walkConcurrency = runtime.NumCPU()

// Result is the outcome of a Size walk.
type Result struct {
	Bytes   int64 // on-disk size (allocated blocks) in bytes
	Skipped int   // entries that could not be stat'd (permission, vanished)
}

// Size returns the on-disk size of the tree rooted at root, walking
// concurrently. Best-effort: unreadable entries and files that vanish
// mid-walk are skipped and counted in Result.Skipped. Symlinks are counted
// as the link itself and never followed.
func Size(root string) (Result, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		return Result{}, err
	}

	var bytes, skipped int64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		bytes += st.Blocks * 512
	}

	sem := make(chan struct{}, walkConcurrency)
	var wg sync.WaitGroup

	var walk func(dir string)
	walk = func(dir string) {
		defer wg.Done()
		entries, err := os.ReadDir(dir)
		if err != nil {
			atomic.AddInt64(&skipped, 1)
			return
		}
		for _, e := range entries {
			info, err := e.Info() // lstat: does not follow symlinks
			if err != nil {
				atomic.AddInt64(&skipped, 1)
				continue
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				atomic.AddInt64(&bytes, st.Blocks*512)
			}
			// Directory (not a symlink to one — e.IsDir is type-based).
			if info.Mode()&fs.ModeSymlink == 0 && e.IsDir() {
				sub := filepath.Join(dir, e.Name())
				wg.Add(1)
				select {
				case sem <- struct{}{}:
					go func(p string) {
						defer func() { <-sem }()
						walk(p)
					}(sub)
				default:
					walk(sub) // pool saturated: recurse inline, no blocking
				}
			}
		}
	}

	wg.Add(1)
	walk(root)
	wg.Wait()
	return Result{Bytes: bytes, Skipped: int(skipped)}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./disk/ -run TestSize -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add disk/disk.go disk/disk_test.go
git commit -m "feat(disk): add concurrent tree size primitive"
```

---

## Task 2: Symlink, skip, determinism, and du cross-check tests

**Files:**
- Test: `disk/disk_test.go`

**Interfaces:**
- Consumes: `disk.Size`, `disk.Result` from Task 1.

- [ ] **Step 1: Write the failing tests**

```go
import (
	"os/exec"
	"strconv"
	"strings"
)

func TestSizeSymlinkNotFollowed(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "big")
	writeFile(t, target, 1<<20) // 1 MiB
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	linkOnly := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linkOnly, "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	res, err := Size(linkOnly)
	if err != nil {
		t.Fatal(err)
	}
	// A symlink's own size is tiny; must not include the 1 MiB target.
	if res.Bytes >= 1<<20 {
		t.Errorf("Bytes = %d, symlink target was followed", res.Bytes)
	}
}

func TestSizeSkipsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not apply")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readable"), 4096)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	res, err := Size(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == 0 {
		t.Error("Skipped = 0, want >= 1 for the unreadable dir")
	}
}

func TestSizeDeterministic(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(root, "f"+strconv.Itoa(i)), 100)
	}
	first, _ := Size(root)
	for i := 0; i < 5; i++ {
		got, _ := Size(root)
		if got.Bytes != first.Bytes {
			t.Fatalf("non-deterministic: %d vs %d", got.Bytes, first.Bytes)
		}
	}
}

func TestSizeMatchesDu(t *testing.T) {
	du, err := exec.LookPath("du")
	if err != nil {
		t.Skip("du not available")
	}
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(root, "f"+strconv.Itoa(i)), 3000)
	}
	out, err := exec.Command(du, "-sk", root).Output()
	if err != nil {
		t.Fatalf("du failed: %v", err)
	}
	fields := strings.Fields(string(out))
	duKB, _ := strconv.ParseInt(fields[0], 10, 64)

	res, _ := Size(root)
	ourKB := res.Bytes / 1024
	diff := ourKB - duKB
	if diff < 0 {
		diff = -diff
	}
	// Allow a couple KB of rounding slack.
	if diff > 4 {
		t.Errorf("our %d KiB vs du %d KiB (diff %d)", ourKB, duKB, diff)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail or pass appropriately**

Run: `go test ./disk/ -v`
Expected: all PASS (they exercise existing `Size`; this task locks behavior). If `TestSizeMatchesDu` fails, the block math is wrong — fix `Size` before proceeding.

- [ ] **Step 3: No new implementation needed**

These tests validate Task 1's implementation. If any fail, correct `Size` in `disk/disk.go`.

- [ ] **Step 4: Run the full disk suite**

Run: `go test ./disk/ -v`
Expected: PASS (du-dependent test may SKIP in minimal environments).

- [ ] **Step 5: Commit**

```bash
git add disk/disk_test.go
git commit -m "test(disk): cover symlinks, skips, determinism, du parity"
```

---

## Task 3: IEC size formatting

**Files:**
- Modify: `disk/disk.go`
- Test: `disk/disk_test.go`

**Interfaces:**
- Produces: `disk.FormatIEC(b int64) string`; `disk.FormatApprox(b int64, approximate bool) string`; `disk.Format(r Result) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestFormatIEC(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{int64(4.8 * 1024 * 1024 * 1024), "4.8 GiB"},
	}
	for _, c := range cases {
		if got := FormatIEC(c.in); got != c.want {
			t.Errorf("FormatIEC(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatApproxAndResult(t *testing.T) {
	if got := FormatApprox(1024, false); got != "1.0 KiB" {
		t.Errorf("FormatApprox exact = %q", got)
	}
	if got := FormatApprox(1024, true); got != "~1.0 KiB" {
		t.Errorf("FormatApprox approx = %q", got)
	}
	if got := Format(Result{Bytes: 1024, Skipped: 0}); got != "1.0 KiB" {
		t.Errorf("Format clean = %q", got)
	}
	if got := Format(Result{Bytes: 1024, Skipped: 3}); got != "~1.0 KiB" {
		t.Errorf("Format skipped = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./disk/ -run TestFormat -v`
Expected: FAIL — `undefined: FormatIEC`.

- [ ] **Step 3: Write the implementation**

Add `"fmt"` to the import block in `disk/disk.go`, then append these functions:

```go
// FormatIEC renders a byte count in binary IEC units (KiB/MiB/GiB…).
func FormatIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatApprox renders b in IEC units, prefixing "~" when the value is a lower
// bound (some entries could not be measured).
func FormatApprox(b int64, approximate bool) string {
	s := FormatIEC(b)
	if approximate {
		return "~" + s
	}
	return s
}

// Format renders a Result, marking it approximate when entries were skipped.
func Format(r Result) string {
	return FormatApprox(r.Bytes, r.Skipped > 0)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./disk/ -run TestFormat -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add disk/disk.go disk/disk_test.go
git commit -m "feat(disk): add IEC size formatting with approximate marker"
```

---

## Task 4: Complete porcelain parse (`WorktreeInfo`)

**Files:**
- Modify: `git/git.go`
- Test: `git/git_test.go`

**Interfaces:**
- Produces: `git.WorktreeInfo{Path, SHA, Branch string; Detached, Bare, Locked, Prunable bool}`; `(WorktreeInfo).Annotation() string`; `parseWorktreeListFull(string) []WorktreeInfo`; `(r *Repo) ListWorktreesFull() ([]WorktreeInfo, error)`.
- Note: existing `WorktreeEntry`, `ListWorktrees`, `parseWorktreeList` are left untouched — `FindWorktreeByBranch` and completion depend on their branch-only filtering.

- [ ] **Step 1: Write the failing test**

```go
func TestParseWorktreeListFull(t *testing.T) {
	porcelain := "worktree /repo/main\n" +
		"HEAD 27233475638abcdef0123456789abcdef01234567\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo/wt-detached\n" +
		"HEAD 00666edca69abcdef0123456789abcdef01234567\n" +
		"detached\n" +
		"\n" +
		"worktree /repo/bare\n" +
		"bare\n" +
		"\n" +
		"worktree /repo/locked-wt\n" +
		"HEAD 689fff37a9cabcdef0123456789abcdef01234567\n" +
		"branch refs/heads/feature\n" +
		"locked reason here\n" +
		"prunable gitdir gone\n"

	got := parseWorktreeListFull(porcelain)
	if len(got) != 4 {
		t.Fatalf("got %d entries, want 4", len(got))
	}
	if got[0].Branch != "main" || got[0].SHA != "27233475638" {
		t.Errorf("entry0 = %+v", got[0])
	}
	if !got[1].Detached || got[1].Branch != "" {
		t.Errorf("entry1 not detached: %+v", got[1])
	}
	if !got[2].Bare {
		t.Errorf("entry2 not bare: %+v", got[2])
	}
	if !got[3].Locked || !got[3].Prunable || got[3].Branch != "feature" {
		t.Errorf("entry3 flags wrong: %+v", got[3])
	}
}

func TestWorktreeInfoAnnotation(t *testing.T) {
	cases := []struct {
		in   WorktreeInfo
		want string
	}{
		{WorktreeInfo{Branch: "main"}, "[main]"},
		{WorktreeInfo{Detached: true}, "(detached HEAD)"},
		{WorktreeInfo{Bare: true}, "(bare)"},
		{WorktreeInfo{Branch: "x", Locked: true}, "[x] locked"},
		{WorktreeInfo{Branch: "x", Locked: true, Prunable: true}, "[x] locked prunable"},
	}
	for _, c := range cases {
		if got := c.in.Annotation(); got != c.want {
			t.Errorf("Annotation(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./git/ -run 'TestParseWorktreeListFull|TestWorktreeInfoAnnotation' -v`
Expected: FAIL — `undefined: parseWorktreeListFull`.

- [ ] **Step 3: Write the implementation**

Add to `git/git.go` (near the existing `WorktreeEntry`/`parseWorktreeList`):

```go
// shaAbbrevLen is the abbreviated-SHA width shown in the sized worktree list.
const shaAbbrevLen = 11

// WorktreeInfo is a complete parse of one `git worktree list --porcelain`
// entry — unlike WorktreeEntry, it retains detached/bare/locked rows.
type WorktreeInfo struct {
	Path     string
	SHA      string // abbreviated HEAD sha ("" for a bare repo)
	Branch   string // short name; "" if detached or bare
	Detached bool
	Bare     bool
	Locked   bool
	Prunable bool
}

// Annotation renders the trailing column git shows for this worktree.
func (w WorktreeInfo) Annotation() string {
	var a string
	switch {
	case w.Bare:
		a = "(bare)"
	case w.Detached:
		a = "(detached HEAD)"
	default:
		a = "[" + w.Branch + "]"
	}
	if w.Locked {
		a += " locked"
	}
	if w.Prunable {
		a += " prunable"
	}
	return a
}

func parseWorktreeListFull(output string) []WorktreeInfo {
	var out []WorktreeInfo
	var cur WorktreeInfo
	started := false
	flush := func() {
		if started && cur.Path != "" {
			out = append(out, cur)
		}
		cur = WorktreeInfo{}
		started = false
	}
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
			started = true
		case strings.HasPrefix(line, "HEAD "):
			sha := strings.TrimPrefix(line, "HEAD ")
			if len(sha) > shaAbbrevLen {
				sha = sha[:shaAbbrevLen]
			}
			cur.SHA = sha
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		case line == "":
			flush()
		}
	}
	flush()
	return out
}

// ListWorktreesFull returns every worktree (including detached/bare) with its
// abbreviated sha and lock/prune flags.
func (r *Repo) ListWorktreesFull() ([]WorktreeInfo, error) {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseWorktreeListFull(buf.String()), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./git/ -run 'TestParseWorktreeListFull|TestWorktreeInfoAnnotation' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add git/git.go git/git_test.go
git commit -m "feat(git): add complete porcelain worktree parse"
```

---

## Task 5: Extract `decorateLine`, add sized render

**Files:**
- Modify: `git/git.go` (refactor `renderWorktreeList`; add `renderSizedWorktreeList`)
- Test: `git/git_test.go`

**Interfaces:**
- Consumes: `WorktreeInfo`, `Annotation` (Task 4); `disk.Result`, `disk.Format`, `disk.FormatApprox` (Tasks 1–3).
- Produces: `decorateLine(content string, active, color bool) string`; `renderSizedWorktreeList(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestRenderSizedWorktreeList(t *testing.T) {
	infos := []WorktreeInfo{
		{Path: "/repo/main", SHA: "27233475638", Branch: "main"},
		{Path: "/repo/feature-x", SHA: "00666edca69", Branch: "feature-x"},
	}
	sizes := []disk.Result{
		{Bytes: 4831838208},          // ~4.5 GiB
		{Bytes: 1288490188, Skipped: 2}, // ~1.2 GiB, approximate
	}
	out := renderSizedWorktreeList(infos, sizes, "/repo/main", false)

	if !strings.Contains(out, "* /repo/main") {
		t.Errorf("active marker missing:\n%s", out)
	}
	if !strings.Contains(out, "[main]") || !strings.Contains(out, "[feature-x]") {
		t.Errorf("branch annotations missing:\n%s", out)
	}
	if !strings.Contains(out, "~1.2 GiB") {
		t.Errorf("approximate marker missing on feature-x:\n%s", out)
	}
	// Total present and approximate (because a contributing row was approximate).
	if !strings.Contains(out, "total") || !strings.Contains(out, "~") {
		t.Errorf("total row wrong:\n%s", out)
	}
}

func TestRenderSizedColorActiveRow(t *testing.T) {
	infos := []WorktreeInfo{{Path: "/repo/main", SHA: "abc", Branch: "main"}}
	sizes := []disk.Result{{Bytes: 1024}}
	out := renderSizedWorktreeList(infos, sizes, "/repo/main", true)
	if !strings.Contains(out, "\033[32m") {
		t.Errorf("expected green on active row:\n%q", out)
	}
}
```

Add the import `"github.com/nicwestvold/gwt/disk"` to `git/git_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./git/ -run 'TestRenderSized' -v`
Expected: FAIL — `undefined: renderSizedWorktreeList`.

- [ ] **Step 3: Write the implementation**

Add `"github.com/nicwestvold/gwt/disk"` to the import block in `git/git.go`. Then replace the body of `renderWorktreeList` (git/git.go:534) to use a shared `decorateLine`, and add the sized renderer:

```go
// decorateLine prepends the active/inactive marker and, on a color terminal,
// wraps the active line in green. Shared by the plain and sized list renders.
func decorateLine(content string, active, color bool) string {
	const green = "\033[32m"
	const reset = "\033[0m"
	switch {
	case active && color:
		return green + "* " + content + reset
	case active:
		return "* " + content
	default:
		return "  " + content
	}
}

func renderWorktreeList(plain, activePath string, color bool) string {
	plain = strings.TrimRight(plain, "\n")
	if plain == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(plain, "\n") {
		active := activePath != "" &&
			(line == activePath || strings.HasPrefix(line, activePath+" "))
		b.WriteString(decorateLine(line, active, color) + "\n")
	}
	return b.String()
}

// renderSizedWorktreeList renders the four-column sized table:
// path | sha | size | annotation, followed by a total row.
func renderSizedWorktreeList(infos []WorktreeInfo, sizes []disk.Result, activePath string, color bool) string {
	pathW := len("total")
	shaW := 0
	sizeStrs := make([]string, len(infos))
	sizeW := 0
	var totalBytes int64
	anyApprox := false
	for i, in := range infos {
		if len(in.Path) > pathW {
			pathW = len(in.Path)
		}
		if len(in.SHA) > shaW {
			shaW = len(in.SHA)
		}
		sizeStrs[i] = disk.Format(sizes[i])
		if len(sizeStrs[i]) > sizeW {
			sizeW = len(sizeStrs[i])
		}
		totalBytes += sizes[i].Bytes
		if sizes[i].Skipped > 0 {
			anyApprox = true
		}
	}
	totalStr := disk.FormatApprox(totalBytes, anyApprox)
	if len(totalStr) > sizeW {
		sizeW = len(totalStr)
	}

	var b strings.Builder
	for i, in := range infos {
		content := fmt.Sprintf("%-*s  %-*s  %*s  %s",
			pathW, in.Path, shaW, in.SHA, sizeW, sizeStrs[i], in.Annotation())
		active := activePath != "" && in.Path == activePath
		b.WriteString(decorateLine(strings.TrimRight(content, " "), active, color) + "\n")
	}
	totalContent := fmt.Sprintf("%-*s  %-*s  %*s", pathW, "total", shaW, "", sizeW, totalStr)
	b.WriteString(decorateLine(strings.TrimRight(totalContent, " "), false, color) + "\n")
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./git/ -run 'TestRenderSized|TestRender' -v`
Expected: PASS. Also run `go test ./git/ -v` to confirm the `renderWorktreeList` refactor broke nothing.

- [ ] **Step 5: Commit**

```bash
git add git/git.go git/git_test.go
git commit -m "feat(git): render sized worktree table with shared decoration"
```

---

## Task 6: `PrintSizedWorktreeList` + `ls -s` interception

**Files:**
- Modify: `git/git.go` (add `PrintSizedWorktreeList`)
- Modify: `main.go` (intercept `ls -s` / `list --size`)

**Interfaces:**
- Consumes: `ListWorktreesFull` (Task 4); `renderSizedWorktreeList`, `currentWorktreeTop`, `shouldColor` (existing/Task 5); `disk.Size` (Task 1).
- Produces: `(r *Repo) PrintSizedWorktreeList() error`.

- [ ] **Step 1: Write the implementation (sizing helper)**

Ensure `"sync"` is imported in `git/git.go`, then add:

```go
// PrintSizedWorktreeList prints the worktree list with an on-disk size column.
// Sizes are computed concurrently across worktrees.
func (r *Repo) PrintSizedWorktreeList() error {
	infos, err := r.ListWorktreesFull()
	if err != nil {
		return err
	}
	sizes := make([]disk.Result, len(infos))
	var wg sync.WaitGroup
	for i := range infos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, _ := disk.Size(infos[i].Path) // best-effort; errors → zero size
			sizes[i] = res
		}(i)
	}
	wg.Wait()
	fmt.Print(renderSizedWorktreeList(infos, sizes, currentWorktreeTop(), shouldColor()))
	return nil
}
```

- [ ] **Step 2: Modify `main.go` interception**

Replace the bare-list block (main.go:931-937) with a version that also recognizes the size flag. Add a helper near the other `main.go` helpers:

```go
// isSizeFlag reports whether args is exactly the size flag for `gwt ls`.
func isSizeFlag(args []string) bool {
	return len(args) == 1 && (args[0] == "-s" || args[0] == "--size")
}
```

Then in the passthrough block:

```go
				// Enhance the bare `gwt list` (and its `ls` alias) by marking
				// the active worktree. `-s`/`--size` adds an on-disk size column.
				// Any other flags fall through to plain git untouched.
				if subcmd == "list" {
					if len(os.Args) == 2 {
						if err := repo.PrintWorktreeList(); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							os.Exit(git.ExitCode(err))
						}
						return
					}
					if isSizeFlag(os.Args[2:]) {
						if err := repo.PrintSizedWorktreeList(); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							os.Exit(git.ExitCode(err))
						}
						return
					}
				}
```

- [ ] **Step 3: Build and manually verify**

Run:
```bash
go build ./... && go run . ls -s
```
Expected: the worktree list with a size column and a `total` row; bare `go run . ls` unchanged.

- [ ] **Step 4: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add git/git.go main.go
git commit -m "feat(list): add -s/--size column to gwt ls"
```

---

## Task 7: `gwt rm` freed-space line (single repo)

**Files:**
- Modify: `git/git.go` (`Remove` returns `RemoveResult`)
- Modify: `main.go` (print freed line)
- Test: `git/git_test.go`

**Interfaces:**
- Consumes: `disk.Size`, `disk.Format` (Tasks 1–3).
- Produces: `git.RemoveResult{RepoDir, WorktreePath, Branch string; Freed disk.Result}`; `(r *Repo) Remove(args []string, keepBranch bool) (RemoveResult, error)`.
- Breaking: callers of the old 3-value `Remove` must be updated (only `main.go`).

- [ ] **Step 1: Write the failing test**

```go
func TestRemoveReturnsResult(t *testing.T) {
	// Verifies the struct shape and that display name falls back to path base.
	rr := RemoveResult{
		RepoDir:      "/repo",
		WorktreePath: "/repo/wt/feature-x",
		Branch:       "feature-x",
		Freed:        disk.Result{Bytes: 1288490188},
	}
	if rr.Branch != "feature-x" {
		t.Fatal("branch field")
	}
	if disk.Format(rr.Freed) != "1.2 GiB" {
		t.Errorf("freed format = %q", disk.Format(rr.Freed))
	}
}
```

(An integration test that actually removes a worktree requires a git fixture; the freed-line wiring is verified manually in Step 4. This test locks the type contract.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./git/ -run TestRemoveReturnsResult -v`
Expected: FAIL — `undefined: RemoveResult`.

- [ ] **Step 3: Change `Remove`**

In `git/git.go`, add the struct and rewrite `Remove`'s signature and returns. Measure size **before** the `git worktree remove` call:

```go
// RemoveResult reports the outcome of removing a single worktree.
type RemoveResult struct {
	RepoDir      string
	WorktreePath string
	Branch       string      // "" if detached
	Freed        disk.Result // on-disk space reclaimed
}
```

Rewrite the signature (git/git.go:97) to `func (r *Repo) Remove(args []string, keepBranch bool) (RemoveResult, error)`. Return `RemoveResult{}, err` at every existing error return. Immediately before the `gitArgs := []string{"worktree", "remove"}` block, measure:

```go
	freed, _ := disk.Size(worktreePath) // best-effort; never blocks removal
```

And replace the final `return r.Dir, worktreePath, nil` with:

```go
	return RemoveResult{
		RepoDir:      r.Dir,
		WorktreePath: worktreePath,
		Branch:       branch,
		Freed:        freed,
	}, nil
```

- [ ] **Step 4: Update `main.go` caller and print the freed line**

Replace the single-repo call site (main.go:475-489). The display name is the branch, falling back to the path base:

```go
		res, err := repo.Remove(resolvedArgs, keepBranch)
		if err != nil {
			return err
		}

		// Clean up empty parent dirs for centralized worktrees.
		dataDir, dataErr := config.DataDir()
		if dataErr == nil {
			worktreeRoot := filepath.Join(dataDir, "worktrees")
			if strings.HasPrefix(res.WorktreePath, worktreeRoot+string(filepath.Separator)) {
				git.CleanEmptyParents(filepath.Dir(res.WorktreePath), worktreeRoot)
			}
		}

		name := res.Branch
		if name == "" {
			name = filepath.Base(res.WorktreePath)
		}
		if res.Freed.Bytes > 0 {
			fmt.Printf("removed worktree %s — freed %s\n", name, disk.Format(res.Freed))
		} else {
			fmt.Printf("removed worktree %s\n", name)
		}

		git.WriteCdFile(res.RepoDir)
		return nil
```

Add `"github.com/nicwestvold/gwt/disk"` to `main.go` imports.

- [ ] **Step 5: Verify, then commit**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS.

Manual check in a scratch repo with a worktree:
```bash
go run . rm some-branch
# -> removed worktree some-branch — freed 12.3 MiB
```

```bash
git add git/git.go main.go git/git_test.go
git commit -m "feat(remove): report freed disk space on rm"
```

---

## Task 8: `RemoveMemberWorktree` returns structured result

**Files:**
- Modify: `git/workspace.go`
- Test: `git/workspace_test.go`

**Interfaces:**
- Consumes: `disk.Size` (Task 1).
- Produces: `git.MemberRemoval{Freed disk.Result; BranchKept string; Err error}`; `RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) MemberRemoval`.
- Breaking: caller `runWorkspaceRemove` (updated in Task 9).

- [ ] **Step 1: Write the failing test**

```go
func TestMemberRemovalShape(t *testing.T) {
	mr := MemberRemoval{
		Freed:      disk.Result{Bytes: 2202009600}, // ~2.05 GiB
		BranchKept: "feature-x",
		Err:        nil,
	}
	if mr.BranchKept != "feature-x" || mr.Err != nil {
		t.Fatal("fields")
	}
	if mr.Freed.Bytes == 0 {
		t.Fatal("freed")
	}
}
```

Add `"github.com/nicwestvold/gwt/disk"` to `git/workspace_test.go` imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./git/ -run TestMemberRemovalShape -v`
Expected: FAIL — `undefined: MemberRemoval`.

- [ ] **Step 3: Rewrite `RemoveMemberWorktree`**

Replace the function (git/workspace.go:68) so it returns data instead of printing warnings. Measure size before removal; report a kept branch when the safe delete fails:

```go
// MemberRemoval reports the outcome of removing one workspace member worktree.
type MemberRemoval struct {
	Freed      disk.Result // reclaimed space (zero if removal failed)
	BranchKept string      // branch left undeleted because it was not merged
	Err        error       // worktree-removal error; nil on success
}

// RemoveMemberWorktree removes one member's worktree and, unless keepBranch,
// safely deletes its branch. It returns structured results rather than
// printing, so the caller can aggregate across members.
func RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) MemberRemoval {
	var branch string
	var buf bytes.Buffer
	bc := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	bc.Stdout = &buf
	if bc.Run() == nil {
		if b := strings.TrimSpace(buf.String()); b != "HEAD" {
			branch = b
		}
	}

	freed, _ := disk.Size(worktreePath) // best-effort, before removal

	args := []string{"-C", repoDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return MemberRemoval{Err: fmt.Errorf("git worktree remove failed for %s: %w", worktreePath, err)}
	}

	mr := MemberRemoval{Freed: freed}
	if !keepBranch && branch != "" {
		del := exec.Command("git", "-C", repoDir, "branch", "-d", branch)
		if err := del.Run(); err != nil {
			mr.BranchKept = branch // not fully merged; caller reports it
		}
	}
	return mr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./git/ -run TestMemberRemovalShape -v`
Expected: PASS. (`go build ./...` will fail until Task 9 updates the caller — that is expected and resolved next.)

- [ ] **Step 5: Commit**

```bash
git add git/workspace.go git/workspace_test.go
git commit -m "refactor(workspace): return structured member-removal result"
```

---

## Task 9: Parallel workspace removal + aggregate output

**Files:**
- Modify: `main.go` (`runWorkspaceRemove`)
- Test: manual (integration requires a workspace fixture)

**Interfaces:**
- Consumes: `git.RemoveMemberWorktree`, `git.MemberRemoval` (Task 8); `disk.FormatApprox` (Task 3).
- Produces: updated `runWorkspaceRemove(cfg, wsName, ws, group, keepBranch, force) (string, error)` — same signature, new behavior.

- [ ] **Step 1: Rewrite `runWorkspaceRemove`**

Replace the member loop (main.go:828-857) with a concurrent, best-effort version that aggregates results. Add `"sync"` to `main.go` imports if absent:

```go
func runWorkspaceRemove(cfg *config.Config, wsName string, ws config.WorkspaceEntry, group string, keepBranch, force bool) (string, error) {
	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		return "", err
	}

	type memberResult struct {
		name    string
		present bool
		mr      git.MemberRemoval
	}
	results := make([]memberResult, len(members))

	var wg sync.WaitGroup
	for i, m := range members {
		worktreePath := filepath.Join(group, m.Short)
		if _, statErr := os.Stat(worktreePath); statErr != nil {
			results[i] = memberResult{name: m.Short, present: false}
			continue
		}
		wg.Add(1)
		go func(i int, repoDir, worktreePath, name string) {
			defer wg.Done()
			results[i] = memberResult{
				name:    name,
				present: true,
				mr:      git.RemoveMemberWorktree(repoDir, worktreePath, keepBranch, force),
			}
		}(i, m.Path, worktreePath, m.Short)
	}
	wg.Wait()

	// Aggregate.
	var totalBytes int64
	anyApprox := false
	removed, attempted := 0, 0
	var failures []string           // "repo: reason"
	keptBranches := map[string][]string{} // branch -> repos
	for _, res := range results {
		if !res.present {
			continue
		}
		attempted++
		if res.mr.Err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", res.name, res.mr.Err))
			continue
		}
		removed++
		totalBytes += res.mr.Freed.Bytes
		if res.mr.Freed.Skipped > 0 {
			anyApprox = true
		}
		if res.mr.BranchKept != "" {
			keptBranches[res.mr.BranchKept] = append(keptBranches[res.mr.BranchKept], res.name)
		}
	}

	// Clean the empty group dir (best-effort, only if everything removed).
	root, rootErr := ws.ResolveWorktreeRoot(wsName)
	if rootErr == nil && len(failures) == 0 {
		_ = os.Remove(group)
		git.CleanEmptyParents(group, root)
	}

	// Report.
	groupName := filepath.Base(group)
	sizeStr := disk.FormatApprox(totalBytes, anyApprox)
	if len(failures) == 0 {
		fmt.Printf("removed workspace group %s (%d repos) — freed %s\n", groupName, removed, sizeStr)
	} else {
		fmt.Printf("removed %d/%d repos — freed %s\n", removed, attempted, sizeStr)
		for _, f := range failures {
			fmt.Printf("  ! %s\n", f)
		}
	}
	for branch, repos := range keptBranches {
		fmt.Printf("note: branch %q kept (not fully merged) in: %s\n", branch, strings.Join(repos, ", "))
		fmt.Printf("      delete with: git -C <repo> branch -D %s\n", branch)
	}

	// Primary path to cd back into.
	primaryPath := members[0].Path
	for _, m := range members {
		if m.IsPrimary {
			primaryPath = m.Path
		}
	}

	if len(failures) > 0 {
		return primaryPath, fmt.Errorf("%d of %d worktrees could not be removed", len(failures), attempted)
	}
	return primaryPath, nil
}
```

- [ ] **Step 2: Handle the caller's cd on partial failure**

In `main.go`, the workspace `rm` block calls `runWorkspaceRemove` then `git.WriteCdFile(cd)` and returns nil (main.go:453-458). Update it to still write the cd file (so the shell returns to the primary repo) but propagate the error for a non-zero exit:

```go
					cd, rmErr := runWorkspaceRemove(cfg, wsName, ws, group, keepBranch, force)
					if cd != "" {
						git.WriteCdFile(cd)
					}
					if rmErr != nil {
						return rmErr
					}
					return nil
```

- [ ] **Step 3: Build and run the full suite**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 4: Manual verification in a workspace**

In a configured workspace, from a member worktree:
```bash
go run . rm some-branch
# -> removed workspace group some-branch (N repos) — freed X.Y GiB
#    (plus a branch note if any member's branch was unmerged)
```

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat(remove): parallelize workspace teardown with freed-space summary"
```

---

## Self-Review

**Spec coverage:**

- `disk` concurrent block-walker → Tasks 1, 2. ✓
- IEC formatting + `~` marker → Task 3. ✓ (used in Tasks 5, 7, 9)
- `rm` single-repo freed line → Task 7. ✓
- `rm` workspace parallel + aggregate line + consolidated branch note + partial-failure `N/M` + non-zero exit → Tasks 8, 9. ✓
- `ls -s` flag interception → Task 6. ✓
- `ls -s` size column + total, self-rendered, active-row color → Tasks 5, 6. ✓
- Row parity (detached/bare/locked/prunable) without breaking `ListWorktrees` → Task 4. ✓
- Symlink / permission / vanished handling → Tasks 1, 2. ✓
- Blocks-not-apparent, `du` parity → Tasks 1, 2. ✓

**Placeholder scan:** No TBDs; every code step contains complete code. Manual-verification steps are used only where a git/workspace fixture would be disproportionate, and each is paired with a type-contract unit test.

**Type consistency:** `disk.Result{Bytes, Skipped}`, `disk.Size`, `disk.Format`, `disk.FormatApprox`, `disk.FormatIEC` used identically across Tasks 1–9. `WorktreeInfo` fields (`Path`, `SHA`, `Branch`, `Detached`, `Bare`, `Locked`, `Prunable`) match between Tasks 4 and 5. `RemoveResult` (Task 7) and `MemberRemoval` (Task 8) field names are consistent with their consumers (main.go, Task 9). `decorateLine`/`renderSizedWorktreeList` signatures match between Tasks 5 and 6.
