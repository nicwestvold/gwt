# Auto-detect version & package managers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `gwt init` and `gwt clone` auto-detect the version manager (mise/asdf) and package manager (pnpm/npm/yarn) so a hook can be generated without the user restating what the repo already declares.

**Architecture:** A new pure `detect` package reads detection signals through a `FileSource` interface, with a `DirSource` (working tree) and a `GitSource` (main branch tree, for the bare-after-clone case where no worktree exists yet). `main.go` builds the right source, calls `detect.Detect`, fills only the manager dimensions the user did not pass explicitly, and generates a hook only when there is real work to do.

**Tech Stack:** Go 1.26, cobra, `os/exec` (git), stdlib `encoding/json`.

## Global Constraints

- **Go 1.26** — module target; use only stdlib + existing deps (cobra).
- **Commits: do NOT run `git commit`.** Per repo/user policy, at every commit step stage the files, show the suggested message, and ask the user to commit. Do not push or open PRs.
- **Explicit flags always win.** Detection fills only a dimension whose flag was not `Changed`.
- **Hook only when there is work.** Generate a hook only if there is at least one copy file, a version manager, or a package manager (explicit or detected).
- **Message strings (verbatim):**
  - per hit: `auto-detected <mgr>, adding to hook`
  - after install when anything was detected: `If auto-detection got it wrong, re-run with explicit flags, e.g. gwt init -f -v asdf -p yarn`
  - nothing to do: `no version or package manager detected — no hook generated (use -c to copy files, or -v/-p to set them manually)`
- **Supported values:** version managers `mise`, `asdf`; package managers `pnpm`, `npm`, `yarn`.

---

### Task 1: `detect` package — pure detection logic

**Files:**
- Create: `detect/detect.go`
- Test: `detect/detect_test.go`

**Interfaces:**
- Consumes: nothing (new package).
- Produces:
  - `type FileSource interface { Exists(path string) bool; Read(path string) ([]byte, error) }`
  - `type Result struct { VersionManager string; PackageManager string }`
  - `func Detect(src FileSource, lookPath func(string) (string, error)) Result`
  - unexported `detectVersionManager(src FileSource, lookPath func(string) (string, error)) string`
  - unexported `detectPackageManager(src FileSource) string`

- [ ] **Step 1: Write the failing tests**

Create `detect/detect_test.go`:

```go
package detect

import (
	"os"
	"os/exec"
	"testing"
)

type fakeSource struct {
	files map[string]string
}

func (f fakeSource) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f fakeSource) Read(path string) ([]byte, error) {
	if v, ok := f.files[path]; ok {
		return []byte(v), nil
	}
	return nil, os.ErrNotExist
}

// lookPathWith returns a fake exec.LookPath where only the named tools resolve.
func lookPathWith(available ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, a := range available {
		set[a] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestDetectVersionManager(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		available []string
		want      string
	}{
		{"mise.toml", map[string]string{"mise.toml": ""}, nil, "mise"},
		{"dot mise.toml", map[string]string{".mise.toml": ""}, nil, "mise"},
		{"config mise", map[string]string{".config/mise/config.toml": ""}, nil, "mise"},
		{"tool-versions with mise on PATH", map[string]string{".tool-versions": ""}, []string{"mise"}, "mise"},
		{"tool-versions with only asdf on PATH", map[string]string{".tool-versions": ""}, []string{"asdf"}, "asdf"},
		{"tool-versions with mise and asdf prefers mise", map[string]string{".tool-versions": ""}, []string{"mise", "asdf"}, "mise"},
		{"tool-versions with neither installed", map[string]string{".tool-versions": ""}, nil, ""},
		{"nothing", map[string]string{}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectVersionManager(fakeSource{tt.files}, lookPathWith(tt.available...))
			if got != tt.want {
				t.Errorf("detectVersionManager() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"packageManager field pnpm", map[string]string{"package.json": `{"packageManager":"pnpm@8.15.0"}`}, "pnpm"},
		{"packageManager field yarn", map[string]string{"package.json": `{"packageManager":"yarn@4.1.0"}`}, "yarn"},
		{"packageManager field npm", map[string]string{"package.json": `{"packageManager":"npm@10.0.0"}`}, "npm"},
		{"unsupported field falls through to lockfile", map[string]string{"package.json": `{"packageManager":"bun@1.0.0"}`, "yarn.lock": ""}, "yarn"},
		{"pnpm lockfile", map[string]string{"pnpm-lock.yaml": ""}, "pnpm"},
		{"yarn lockfile", map[string]string{"yarn.lock": ""}, "yarn"},
		{"npm lockfile", map[string]string{"package-lock.json": ""}, "npm"},
		{"multiple lockfiles prefer pnpm", map[string]string{"pnpm-lock.yaml": "", "yarn.lock": "", "package-lock.json": ""}, "pnpm"},
		{"package.json without field, no lockfile", map[string]string{"package.json": `{"name":"x"}`}, ""},
		{"nothing", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPackageManager(fakeSource{tt.files})
			if got != tt.want {
				t.Errorf("detectPackageManager() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	src := fakeSource{map[string]string{
		"mise.toml":      "",
		"pnpm-lock.yaml": "",
	}}
	got := Detect(src, lookPathWith())
	if got.VersionManager != "mise" || got.PackageManager != "pnpm" {
		t.Errorf("Detect() = %+v, want {mise pnpm}", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./detect/ -v`
Expected: FAIL — build error, `detect.go` does not exist (undefined: `detectVersionManager`, `detectPackageManager`, `Detect`).

- [ ] **Step 3: Write the implementation**

Create `detect/detect.go`:

```go
// Package detect infers a repo's version manager and package manager from the
// files it declares, so gwt can generate a post-checkout hook without the user
// restating them.
package detect

import (
	"encoding/json"
	"strings"
)

// FileSource abstracts reading detection signals so detection works against a
// working directory or a git tree (before any worktree is checked out).
type FileSource interface {
	Exists(path string) bool
	Read(path string) ([]byte, error)
}

// Result holds the detected managers. Empty string means "not detected".
type Result struct {
	VersionManager string
	PackageManager string
}

var validPackageManagers = map[string]bool{"pnpm": true, "npm": true, "yarn": true}

// Detect infers the version and package managers from src. lookPath is used to
// disambiguate a bare .tool-versions file (mise and asdf share it); pass
// exec.LookPath in production.
func Detect(src FileSource, lookPath func(string) (string, error)) Result {
	return Result{
		VersionManager: detectVersionManager(src, lookPath),
		PackageManager: detectPackageManager(src),
	}
}

func detectVersionManager(src FileSource, lookPath func(string) (string, error)) string {
	for _, f := range []string{"mise.toml", ".mise.toml", ".config/mise/config.toml"} {
		if src.Exists(f) {
			return "mise"
		}
	}
	if src.Exists(".tool-versions") {
		if _, err := lookPath("mise"); err == nil {
			return "mise"
		}
		if _, err := lookPath("asdf"); err == nil {
			return "asdf"
		}
	}
	return ""
}

func detectPackageManager(src FileSource) string {
	if src.Exists("package.json") {
		if data, err := src.Read("package.json"); err == nil {
			var pj struct {
				PackageManager string `json:"packageManager"`
			}
			if json.Unmarshal(data, &pj) == nil && pj.PackageManager != "" {
				name := pj.PackageManager
				if i := strings.IndexByte(name, '@'); i >= 0 {
					name = name[:i]
				}
				if validPackageManagers[name] {
					return name
				}
			}
		}
	}
	for _, lf := range []struct{ file, pm string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	} {
		if src.Exists(lf.file) {
			return lf.pm
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./detect/ -v`
Expected: PASS — all `TestDetectVersionManager`, `TestDetectPackageManager`, `TestDetect` subtests pass.

- [ ] **Step 5: Commit**

Stage `detect/detect.go` and `detect/detect_test.go`, then ask the user to commit with:

```
feat(detect): add version/package manager detection over a FileSource
```

---

### Task 2: source adapters (`DirSource`, `GitSource`)

**Files:**
- Modify: `detect/detect.go` (append adapters + imports)
- Test: `detect/detect_test.go` (append `DirSource` test)

**Interfaces:**
- Consumes: `FileSource` from Task 1.
- Produces:
  - `type DirSource struct { Root string }` implementing `FileSource`
  - `type GitSource struct { RepoDir string; Ref string }` implementing `FileSource`

- [ ] **Step 1: Write the failing test**

Append to `detect/detect_test.go`:

```go
import (
	"path/filepath"
)

func TestDirSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"pnpm@8"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	src := DirSource{Root: dir}

	if !src.Exists("package.json") {
		t.Error("Exists(package.json) = false, want true")
	}
	if src.Exists("missing.txt") {
		t.Error("Exists(missing.txt) = true, want false")
	}
	data, err := src.Read("package.json")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if string(data) != `{"packageManager":"pnpm@8"}` {
		t.Errorf("Read() = %q", string(data))
	}
}
```

(Merge the `path/filepath` import into the existing import block rather than adding a second block.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./detect/ -run TestDirSource -v`
Expected: FAIL — `undefined: DirSource`.

- [ ] **Step 3: Write the implementation**

Update the import block in `detect/detect.go` to:

```go
import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)
```

Append the adapters to `detect/detect.go`:

```go
// DirSource reads detection signals from a directory on disk. Used when the
// repo's working tree is checked out.
type DirSource struct {
	Root string
}

func (d DirSource) Exists(path string) bool {
	_, err := os.Stat(filepath.Join(d.Root, path))
	return err == nil
}

func (d DirSource) Read(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.Root, path))
}

// GitSource reads detection signals from a branch's git tree without a
// checkout. Used for a bare repo right after clone, before any worktree exists.
type GitSource struct {
	RepoDir string
	Ref     string
}

func (g GitSource) Exists(path string) bool {
	cmd := exec.Command("git", "-C", g.RepoDir, "cat-file", "-e", g.Ref+":"+path)
	return cmd.Run() == nil
}

func (g GitSource) Read(path string) ([]byte, error) {
	cmd := exec.Command("git", "-C", g.RepoDir, "show", g.Ref+":"+path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./detect/ -v`
Expected: PASS — including `TestDirSource`. Run `go vet ./detect/` and expect no output.

- [ ] **Step 5: Commit**

Stage `detect/detect.go` and `detect/detect_test.go`, then ask the user to commit with:

```
feat(detect): add DirSource and GitSource FileSource adapters
```

---

### Task 3: wire detection into `gwt init`

**Files:**
- Modify: `main.go` (add flag in `main()`; add helpers + consts; rewrite `initCmd.RunE`)
- Test: `main_test.go` (create)

**Interfaces:**
- Consumes: `detect.Detect`, `detect.DirSource`, `detect.GitSource`, `detect.Result` (Tasks 1–2); existing `repoBasePath`, `git.MainBranchRef`, `hookOptions`, `registerRepo`, `setupHook`.
- Produces (in `package main`, used by Task 4):
  - `func fileSourceFor(repo *git.Repo, mainBranch string) detect.FileSource`
  - `func mergeDetected(opts hookOptions, res detect.Result, vmSet, pmSet bool) (hookOptions, []string, bool)`
  - `func detectAndMerge(repo *git.Repo, opts hookOptions, vmSet, pmSet bool) (hookOptions, bool)`
  - `func hookHasWork(opts hookOptions) bool`
  - `const noHookMsg`, `const fixItMsg`

- [ ] **Step 1: Write the failing tests**

Create `main_test.go`:

```go
package main

import (
	"testing"

	"github.com/nicwestvold/gwt/detect"
)

func TestHookHasWork(t *testing.T) {
	tests := []struct {
		name string
		opts hookOptions
		want bool
	}{
		{"empty", hookOptions{}, false},
		{"copy files", hookOptions{copyFiles: []string{".env"}}, true},
		{"version manager", hookOptions{versionManager: "mise"}, true},
		{"package manager", hookOptions{packageManager: "pnpm"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hookHasWork(tt.opts); got != tt.want {
				t.Errorf("hookHasWork() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeDetected(t *testing.T) {
	t.Run("fills both unset dimensions", func(t *testing.T) {
		opts, msgs, detected := mergeDetected(hookOptions{}, detect.Result{VersionManager: "mise", PackageManager: "pnpm"}, false, false)
		if opts.versionManager != "mise" || opts.packageManager != "pnpm" {
			t.Errorf("opts = %+v", opts)
		}
		if !detected {
			t.Error("detected = false, want true")
		}
		if len(msgs) != 2 {
			t.Errorf("msgs = %v, want 2 messages", msgs)
		}
	})

	t.Run("explicit version manager is not overwritten", func(t *testing.T) {
		opts, msgs, detected := mergeDetected(hookOptions{versionManager: "asdf"}, detect.Result{VersionManager: "mise", PackageManager: "pnpm"}, true, false)
		if opts.versionManager != "asdf" {
			t.Errorf("versionManager = %q, want asdf", opts.versionManager)
		}
		if opts.packageManager != "pnpm" {
			t.Errorf("packageManager = %q, want pnpm", opts.packageManager)
		}
		if !detected {
			t.Error("detected = false, want true (pnpm was detected)")
		}
		if len(msgs) != 1 {
			t.Errorf("msgs = %v, want 1 message (pnpm only)", msgs)
		}
	})

	t.Run("nothing detected", func(t *testing.T) {
		opts, msgs, detected := mergeDetected(hookOptions{}, detect.Result{}, false, false)
		if opts.versionManager != "" || opts.packageManager != "" {
			t.Errorf("opts = %+v, want empty", opts)
		}
		if detected {
			t.Error("detected = true, want false")
		}
		if len(msgs) != 0 {
			t.Errorf("msgs = %v, want none", msgs)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'TestHookHasWork|TestMergeDetected' -v`
Expected: FAIL — build error (`undefined: hookHasWork`, `undefined: mergeDetected`).

- [ ] **Step 3: Add helpers, consts, and the flag; rewrite `initCmd.RunE`**

In `main.go`, add `"github.com/nicwestvold/gwt/detect"` to the import block.

Add these helpers and consts near `setupHook` (anywhere at package scope):

```go
const noHookMsg = "no version or package manager detected — no hook generated (use -c to copy files, or -v/-p to set them manually)"
const fixItMsg = "If auto-detection got it wrong, re-run with explicit flags, e.g. gwt init -f -v asdf -p yarn"

// fileSourceFor returns a detection source for the repo's main branch content:
// the working directory when it exists, otherwise the main branch git tree
// (for a bare repo whose main worktree is not checked out yet).
func fileSourceFor(repo *git.Repo, mainBranch string) detect.FileSource {
	basePath := repoBasePath(repo, mainBranch)
	if fi, err := os.Stat(basePath); err == nil && fi.IsDir() {
		return detect.DirSource{Root: basePath}
	}
	return detect.GitSource{RepoDir: repo.Dir, Ref: git.MainBranchRef(repo.Dir, mainBranch)}
}

// mergeDetected fills the version/package manager dimensions of opts that were
// not set explicitly (vmSet/pmSet) from res, returning the updated opts, the
// user-facing messages to print, and whether anything was auto-detected.
func mergeDetected(opts hookOptions, res detect.Result, vmSet, pmSet bool) (hookOptions, []string, bool) {
	var msgs []string
	detected := false
	if !vmSet && res.VersionManager != "" {
		opts.versionManager = res.VersionManager
		msgs = append(msgs, fmt.Sprintf("auto-detected %s, adding to hook", res.VersionManager))
		detected = true
	}
	if !pmSet && res.PackageManager != "" {
		opts.packageManager = res.PackageManager
		msgs = append(msgs, fmt.Sprintf("auto-detected %s, adding to hook", res.PackageManager))
		detected = true
	}
	return opts, msgs, detected
}

// detectAndMerge runs detection for the repo and merges the result into opts,
// printing the auto-detected messages. Returns the updated opts and whether
// anything was auto-detected.
func detectAndMerge(repo *git.Repo, opts hookOptions, vmSet, pmSet bool) (hookOptions, bool) {
	res := detect.Detect(fileSourceFor(repo, opts.mainBranch), exec.LookPath)
	opts, msgs, detected := mergeDetected(opts, res, vmSet, pmSet)
	for _, m := range msgs {
		fmt.Println(m)
	}
	return opts, detected
}

// hookHasWork reports whether a generated hook would do anything.
func hookHasWork(opts hookOptions) bool {
	return len(opts.copyFiles) > 0 || opts.versionManager != "" || opts.packageManager != ""
}
```

Note: `main.go` already imports `os/exec` (used elsewhere) and `os`/`fmt`; no new imports beyond `detect`.

Register the flag in `main()`, next to the other `initCmd.Flags()` calls:

```go
	initCmd.Flags().BoolP("with-hook", "w", false, "Auto-detect managers and generate a post-checkout hook")
```

Replace the body of `initCmd.RunE` (currently the block starting at `mainBranch, _ := cmd.Flags().GetString("main")` through the final `return setupHook(repo, opts)`) with:

```go
			mainBranch, _ := cmd.Flags().GetString("main")
			copyFiles, _ := cmd.Flags().GetStringSlice("copy")
			versionManager, _ := cmd.Flags().GetString("version-manager")
			packageManager, _ := cmd.Flags().GetString("package-manager")
			force, _ := cmd.Flags().GetBool("force")

			if versionManager != "" && !validVersionManagers[versionManager] {
				return fmt.Errorf("invalid version manager %q: must be one of: asdf, mise", versionManager)
			}
			if packageManager != "" && !validPackageManagers[packageManager] {
				return fmt.Errorf("invalid package manager %q: must be one of: pnpm, npm, yarn", packageManager)
			}

			opts := hookOptions{
				mainBranch:     mainBranch,
				copyFiles:      copyFiles,
				versionManager: versionManager,
				packageManager: packageManager,
				force:          force,
			}

			wantHook := cmd.Flags().Changed("copy") || cmd.Flags().Changed("version-manager") ||
				cmd.Flags().Changed("package-manager") || cmd.Flags().Changed("with-hook")

			detected := false
			if wantHook {
				opts, detected = detectAndMerge(repo, opts, cmd.Flags().Changed("version-manager"), cmd.Flags().Changed("package-manager"))
			}

			if err := registerRepo(repo, opts); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", err)
			}

			if !wantHook {
				basePath := repoBasePath(repo, mainBranch)
				if _, err := os.Stat(filepath.Join(basePath, ".env")); err == nil {
					fmt.Println("hint: .env file found; to copy it to new worktrees, run:")
					fmt.Println("  gwt init -c .env")
				}
				return nil
			}

			if !hookHasWork(opts) {
				fmt.Println(noHookMsg)
				return nil
			}

			if err := setupHook(repo, opts); err != nil {
				return err
			}
			if detected {
				fmt.Println(fixItMsg)
			}
			return nil
```

- [ ] **Step 4: Run tests and build to verify they pass**

Run: `go test . -run 'TestHookHasWork|TestMergeDetected' -v`
Expected: PASS.

Run: `go build ./... && go vet ./...`
Expected: no output (clean build and vet).

- [ ] **Step 5: Manual end-to-end check**

Build the binary (`mise run install`), then create a scratch non-bare repo that declares managers and run `gwt init -w` in it:

```bash
tmp=$(mktemp -d)/scratch
git init -q -b main "$tmp"
cd "$tmp"
printf '{"packageManager":"pnpm@8"}\n' > package.json
touch mise.toml
git add -A && git commit -q -m init
gwt init -w
```

Expected output includes `auto-detected mise, adding to hook`, `auto-detected pnpm, adding to hook`, a `post-checkout hook installed:` line, and the `If auto-detection got it wrong…` note.

Then confirm the empty case writes no hook:

```bash
tmp2=$(mktemp -d)/empty
git init -q -b main "$tmp2"
cd "$tmp2"
git commit -q --allow-empty -m init
gwt init -w
```

Expected: prints `no version or package manager detected — no hook generated (use -c to copy files, or -v/-p to set them manually)` and no `.git/hooks/post-checkout` is created.

- [ ] **Step 6: Commit**

Stage `main.go` and `main_test.go`, then ask the user to commit with:

```
feat(init): auto-detect version/package managers with --with-hook
```

---

### Task 4: wire detection into `gwt clone`

**Files:**
- Modify: `main.go` (add flag in `main()`; update `cloneCmd.RunE`)

**Interfaces:**
- Consumes: `detectAndMerge`, `hookHasWork`, `noHookMsg`, `fixItMsg` (Task 3); existing `registerRepo`, `setupHook`, `git.Clone`, `git.WriteCdFile`.
- Produces: nothing new.

- [ ] **Step 1: Add the flag**

In `main()`, next to the other `cloneCmd.Flags()` calls, add:

```go
	cloneCmd.Flags().BoolP("with-hook", "w", false, "Auto-detect managers and generate a post-checkout hook")
```

- [ ] **Step 2: Update `cloneCmd.RunE`**

In `cloneCmd.RunE`, add `"with-hook"` to the `initFlags` slice and replace the detection/hook block. The current tail (from `repo := &git.Repo{Dir: absDir, IsBare: true}` through the final `return nil`) becomes:

```go
			repo := &git.Repo{Dir: absDir, IsBare: true}
			opts := hookOptions{
				mainBranch:     mainBranch,
				copyFiles:      copyFiles,
				versionManager: versionManager,
				packageManager: packageManager,
			}

			initFlags := []string{"main", "copy", "version-manager", "package-manager", "with-hook"}
			wantHook := false
			for _, f := range initFlags {
				if cmd.Flags().Changed(f) {
					wantHook = true
					break
				}
			}

			detected := false
			if wantHook {
				opts, detected = detectAndMerge(repo, opts, cmd.Flags().Changed("version-manager"), cmd.Flags().Changed("package-manager"))
			}

			if regErr := registerRepo(repo, opts); regErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to register repo in config: %v\n", regErr)
			}

			hookCreated := false
			if wantHook {
				if hookHasWork(opts) {
					if err := setupHook(repo, opts); err != nil {
						fmt.Fprintf(os.Stderr, "Clone succeeded, but hook setup failed: %v\n", err)
						fmt.Fprintf(os.Stderr, "You can retry with: cd %s && gwt init\n", absDir)
						return err
					}
					hookCreated = true
					if detected {
						fmt.Println(fixItMsg)
					}
				} else {
					fmt.Println(noHookMsg)
				}
			}

			git.WriteCdFile(absDir)

			fmt.Printf("Cloned into %s\n", absDir)
			fmt.Println("Next steps:")
			fmt.Println("  cd", absDir)
			if !hookCreated {
				fmt.Println("  gwt init       # generate post-checkout hook")
			}
			fmt.Println("  gwt add <branch>")
			return nil
```

Note: the explicit-flag validation for `version-manager`/`package-manager` earlier in `cloneCmd.RunE` is unchanged and still runs before this block.

- [ ] **Step 3: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: no output.

Run: `go test ./...`
Expected: PASS (existing suite + `detect` + `main` tests).

- [ ] **Step 4: Manual end-to-end check**

Create a scratch upstream repo with a `pnpm-lock.yaml` (or `package.json` `packageManager` field) and a `mise.toml` on its default branch, then:

```bash
# build gwt first, e.g. mise run install, then from a scratch parent dir:
gwt clone <path-or-url-to-scratch-repo> -w
```

Expected: output includes `auto-detected mise, adding to hook`, `auto-detected pnpm, adding to hook`, `post-checkout hook installed:`, the `If auto-detection got it wrong…` note, and the `gwt init` next-step hint is **absent** (a hook was created). Repeat with a repo that declares neither and confirm the `no version or package manager detected` message and that the `gwt init` hint is present.

- [ ] **Step 5: Commit**

Stage `main.go`, then ask the user to commit with:

```
feat(clone): support --with-hook to auto-detect managers on clone
```

---

### Task 5: update docs

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Update the README Init section**

In `README.md`, in the `### Init` example block, add a `-w` line and update the surrounding prose to describe auto-detection. Replace the sentence "A hook is generated only when `-c`, `-p`, or `-v` is provided." with:

```
A hook is generated when `-c`, `-p`, `-v`, or `-w`/`--with-hook` is provided. `-w`
auto-detects the version manager (mise/asdf) and package manager (pnpm/npm/yarn)
from the repo; if it finds neither and no `-c` files were given, no hook is
written. Detection also runs alongside `-c`/`-p`/`-v` to fill in whatever you
didn't specify — explicit flags always win. In a bare repo, `gwt init` also
configures `remote.origin.fetch` so `git fetch` works properly.
```

Add to the `### Init` fenced example, after the `gwt init -c .env` line:

```
gwt init -w                              # auto-detect managers + generate a hook
```

- [ ] **Step 2: Update the README Clone section**

In `### Clone`, add to the fenced example:

```
gwt clone <repo> -w                      # clone, then auto-detect managers for the hook
```

And update the trailing sentence to note that `-w` also triggers a hook: change "Without init flags (`--main`, `--copy`, `--version-manager`, `--package-manager`), no hook is created" to "Without init flags (`--main`, `--copy`, `-v`, `-p`, `-w`), no hook is created".

- [ ] **Step 3: Update the Quick Start (optional detection mention)**

In `## Quick Start`, change the init line to show detection is automatic:

```
gwt init -c .env      # copy .env into new worktrees; auto-detects mise/pnpm/etc.
```

- [ ] **Step 4: Update CLAUDE.md command summaries**

In `CLAUDE.md`, update the `gwt init` and `gwt clone` bullet lines under "Key commands" to mention `-w`/`--with-hook` and auto-detection, e.g. append to the init bullet: "; `-w`/`--with-hook` auto-detects the version and package managers and generates a hook when one is found" and to the clone bullet: "; also accepts `-w` to auto-detect and install the hook".

- [ ] **Step 5: Verify docs render and commit**

Run: `go test ./...`
Expected: PASS (sanity — no code changed, but confirms tree is green).

Stage `README.md` and `CLAUDE.md`, then ask the user to commit with:

```
docs: document --with-hook auto-detection for init and clone
```
```
```

## Self-Review

**Spec coverage:**
- New `-w`/`--with-hook` flag on init and clone → Tasks 3, 4.
- Detection triggers (runs when `-c`/`-p`/`-v`/`-w`) → Task 3 (`wantHook`), Task 4 (`initFlags`).
- Explicit flags win → `mergeDetected` + `Changed(...)` args (Task 3, tested).
- "Something to do" rule → `hookHasWork` (Task 3, tested; used in Task 4).
- Config gets detected values → detection runs before `registerRepo` (Tasks 3, 4).
- `FileSource` + `DirSource`/`GitSource`, source selection → Tasks 1, 2, `fileSourceFor` (Task 3).
- VM rules (mise files, `.tool-versions` + PATH) → Task 1 (tested).
- PM rules (`packageManager` field, lockfile priority) → Task 1 (tested).
- Messaging (three strings) → Global Constraints + Tasks 3, 4.
- Out of scope (bun/deno, corepack-for-npm, template) → untouched.

**Placeholder scan:** none — every code step has complete code; commands have expected output.

**Type consistency:** `FileSource`, `Result`, `Detect(src, lookPath)`, `DirSource{Root}`, `GitSource{RepoDir, Ref}`, `mergeDetected`, `detectAndMerge`, `hookHasWork`, `fileSourceFor`, `noHookMsg`, `fixItMsg` are used identically across tasks.
