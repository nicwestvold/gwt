# gwt Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `gwt add`/`gwt remove` fan out across a configured group of sibling repos (a "workspace"), so coupled codebases like `grafana` + `grafana-enterprise` get matched worktrees laid out as siblings plus a cross-repo setup step.

**Architecture:** A new `[workspaces.<name>]` config table lists member repos (resolved against the existing `[repos]` registry). `gwt add` detects whether the current repo is a workspace member; if so it creates one worktree per member under a shared per-branch directory, mirrors the branch to followers, then runs a configurable `setup` command. `gwt remove` tears the whole group down. No workspace configured ⇒ existing single-repo behavior is untouched.

**Tech Stack:** Go, `cobra` (CLI), `github.com/BurntSushi/toml` (config), stdlib `testing` + `os/exec` for git integration tests.

## Global Constraints

- Go module `github.com/nicwestvold/gwt`; packages: `config`, `git`, `hook`, `main`.
- No new third-party dependencies.
- Tests use stdlib `testing`, `t.TempDir()`, `t.Setenv`. Git integration tests in package `git` use the existing `testRunGit`/`testGitEnv` helpers (`git/testhelper_test.go`).
- Backward compatibility: with no `[workspaces]` table, all existing behavior is byte-for-byte unchanged.
- Member worktree directory name = the member repo's canonical-name **last segment** (e.g. `grafana-enterprise`), so relative paths like `../grafana-enterprise` resolve.
- Follower branch policy = **mirror**: same branch name; check out if it exists (local or `origin/`), else create from the member's main branch (`main_branch`, default `"main"`).
- Spec: `docs/superpowers/specs/2026-06-17-gwt-workspaces-design.md`.
- Per repo owner's rule: do NOT run `git commit` automatically. The "Commit" step in each task means *stage the changes and tell the user the suggested commit message* for them to commit.

---

## File Structure

- `config/workspace.go` (new) — `WorkspaceEntry` type, `Workspaces` map field on `Config`, member resolution, path helpers.
- `config/config.go` (modify) — add `Workspaces` field to `Config`.
- `config/workspace_test.go` (new) — unit tests for the above.
- `git/git.go` (modify) — refactor add-arg parsing into reusable `ParseAddArgs`/`AddArgs.Build`; export `BranchToDir`.
- `git/workspace.go` (new) — `BranchExists`, `MainBranchRef`, `AddWorktreeAt`, `RemoveMemberWorktree`, `RunSetup`.
- `git/workspace_test.go` (new) — integration tests with temp repos.
- `git/git_test.go` (modify) — tests for `ParseAddArgs`/`Build` if not already covered.
- `main.go` (modify) — `runWorkspaceAdd`/`runWorkspaceRemove`, wire into `addCmd`/`removeCmd`.
- `main_test.go` (modify) — integration tests for the orchestration functions.
- `README.md` (modify) — add a terse `### Workspaces` section.

---

## Task 1: Config — workspace schema + `WorkspaceForRepo`

**Files:**
- Modify: `config/config.go` (add `Workspaces` field to `Config`)
- Create: `config/workspace.go`
- Create: `config/workspace_test.go`

**Interfaces:**
- Produces:
  - `type WorkspaceEntry struct { Members []string; Primary string; Setup string; SetupCwd string; WorktreeRoot string }`
  - `Config.Workspaces map[string]WorkspaceEntry` (toml key `workspaces`, omitempty)
  - `func (c *Config) WorkspaceForRepo(canonical string) (string, WorkspaceEntry, bool)`
  - `func lastSegment(canonical string) string`

- [ ] **Step 1: Write the failing test**

Create `config/workspace_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &Config{
		Repos: map[string]RepoEntry{
			"grafana/grafana":            {Path: "/code/grafana", MainBranch: "main"},
			"grafana/grafana-enterprise": {Path: "/code/grafana-enterprise"},
		},
		Workspaces: map[string]WorkspaceEntry{
			"grafana": {
				Members:      []string{"grafana", "grafana-enterprise"},
				Primary:      "grafana",
				Setup:        "make enterprise-dev",
				SetupCwd:     "grafana",
				WorktreeRoot: "~/code/.worktrees",
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Confirm it persisted under a [workspaces.grafana] table.
	data, err := os.ReadFile(filepath.Join(tmp, "gwt", "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !contains(string(data), "[workspaces.grafana]") {
		t.Fatalf("config missing [workspaces.grafana]:\n%s", data)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	ws, ok := loaded.Workspaces["grafana"]
	if !ok {
		t.Fatal("workspace grafana not loaded")
	}
	if ws.Primary != "grafana" || ws.Setup != "make enterprise-dev" || ws.SetupCwd != "grafana" {
		t.Errorf("unexpected workspace: %+v", ws)
	}
	if len(ws.Members) != 2 || ws.Members[0] != "grafana" || ws.Members[1] != "grafana-enterprise" {
		t.Errorf("members = %v", ws.Members)
	}
	if ws.WorktreeRoot != "~/code/.worktrees" {
		t.Errorf("WorktreeRoot = %q", ws.WorktreeRoot)
	}
}

func TestWorkspaceForRepo(t *testing.T) {
	cfg := &Config{
		Repos: map[string]RepoEntry{
			"grafana/grafana":            {Path: "/code/grafana"},
			"grafana/grafana-enterprise": {Path: "/code/grafana-enterprise"},
		},
		Workspaces: map[string]WorkspaceEntry{
			"grafana": {Members: []string{"grafana", "grafana-enterprise"}, Primary: "grafana"},
		},
	}
	tests := []struct {
		name      string
		canonical string
		wantName  string
		wantOK    bool
	}{
		{"by canonical", "grafana/grafana", "grafana", true},
		{"by short segment", "grafana/grafana-enterprise", "grafana", true},
		{"non-member", "nicwestvold/gwt", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, _, ok := cfg.WorkspaceForRepo(tt.canonical)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("WorkspaceForRepo(%q) = (%q,%v), want (%q,%v)", tt.canonical, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

// contains is a tiny helper to avoid importing strings in the test for one call.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./config/ -run 'Workspace' -v`
Expected: FAIL — `WorkspaceEntry`/`Workspaces`/`WorkspaceForRepo` undefined (compile error).

- [ ] **Step 3: Add the `Workspaces` field to `Config`**

In `config/config.go`, change the `Config` struct:

```go
// Config is the top-level gwt configuration, keyed by canonical repo name.
type Config struct {
	Repos      map[string]RepoEntry      `toml:"repos"`
	Workspaces map[string]WorkspaceEntry `toml:"workspaces,omitempty"`
}
```

- [ ] **Step 4: Create `config/workspace.go`**

```go
package config

import "strings"

// WorkspaceEntry groups several registered repos that must be checked out as
// sibling worktrees sharing a branch.
type WorkspaceEntry struct {
	// Members lists repos by canonical name ("owner/repo") or unique short
	// last segment ("repo"). The first member is the default primary.
	Members []string `toml:"members"`
	// Primary is the member you cd into after add; followers mirror its branch.
	Primary string `toml:"primary,omitempty"`
	// Setup is a shell command run after all worktrees exist (optional).
	Setup string `toml:"setup,omitempty"`
	// SetupCwd is the member whose worktree Setup runs in (defaults to Primary).
	SetupCwd string `toml:"setup_cwd,omitempty"`
	// WorktreeRoot overrides where per-branch group dirs are created.
	WorktreeRoot string `toml:"worktree_root,omitempty"`
}

// lastSegment returns the final path segment of a canonical name,
// e.g. "grafana/grafana-enterprise" -> "grafana-enterprise".
func lastSegment(canonical string) string {
	if i := strings.LastIndex(canonical, "/"); i >= 0 {
		return canonical[i+1:]
	}
	return canonical
}

// WorkspaceForRepo returns the workspace that lists the given canonical repo
// name as a member (matched by canonical name or unique short segment).
func (c *Config) WorkspaceForRepo(canonical string) (string, WorkspaceEntry, bool) {
	short := lastSegment(canonical)
	for name, ws := range c.Workspaces {
		for _, m := range ws.Members {
			if m == canonical || m == short {
				return name, ws, true
			}
		}
	}
	return "", WorkspaceEntry{}, false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./config/ -run 'Workspace' -v`
Expected: PASS (both tests).

- [ ] **Step 6: Stage + report commit message**

```bash
git add config/config.go config/workspace.go config/workspace_test.go
```
Suggested message (ask the user to commit): `feat(config): add workspace schema and WorkspaceForRepo lookup`

---

## Task 2: Config — member resolution + path helpers

**Files:**
- Modify: `config/workspace.go`
- Modify: `config/workspace_test.go`

**Interfaces:**
- Consumes: `lastSegment`, `Config.Repos`, `DataDir()` (from Task 1 / existing `config.go`).
- Produces:
  - `type ResolvedMember struct { Name, Short, Path, MainBranch string; IsPrimary bool }`
  - `func (c *Config) ResolveMembers(ws WorkspaceEntry) ([]ResolvedMember, error)`
  - `func (ws WorkspaceEntry) ResolveWorktreeRoot(name string) (string, error)`
  - `func expandTilde(p string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `config/workspace_test.go`:

```go
func TestResolveMembers(t *testing.T) {
	cfg := &Config{
		Repos: map[string]RepoEntry{
			"grafana/grafana":            {Path: "/code/grafana", MainBranch: "main"},
			"grafana/grafana-enterprise": {Path: "/code/grafana-enterprise"}, // no main_branch -> defaults
		},
	}
	ws := WorkspaceEntry{Members: []string{"grafana", "grafana-enterprise"}, Primary: "grafana"}

	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		t.Fatalf("ResolveMembers error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}
	if members[0].Name != "grafana/grafana" || members[0].Short != "grafana" || !members[0].IsPrimary {
		t.Errorf("member[0] = %+v", members[0])
	}
	if members[0].MainBranch != "main" {
		t.Errorf("member[0].MainBranch = %q, want main", members[0].MainBranch)
	}
	if members[1].Short != "grafana-enterprise" || members[1].IsPrimary {
		t.Errorf("member[1] = %+v", members[1])
	}
	if members[1].MainBranch != "main" {
		t.Errorf("member[1].MainBranch = %q, want defaulted main", members[1].MainBranch)
	}
}

func TestResolveMembersErrors(t *testing.T) {
	cfg := &Config{Repos: map[string]RepoEntry{
		"a/grafana": {Path: "/a/grafana"},
		"b/grafana": {Path: "/b/grafana"},
		"x/only":    {Path: "/x/only"},
	}}

	if _, err := cfg.ResolveMembers(WorkspaceEntry{Members: []string{"missing"}}); err == nil {
		t.Error("expected error for unregistered member")
	}
	if _, err := cfg.ResolveMembers(WorkspaceEntry{Members: []string{"grafana"}}); err == nil {
		t.Error("expected error for ambiguous short match")
	}
}

func TestResolveWorktreeRoot(t *testing.T) {
	t.Run("explicit with tilde", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		ws := WorkspaceEntry{WorktreeRoot: "~/code/.worktrees"}
		got, err := ws.ResolveWorktreeRoot("grafana")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, "code", ".worktrees")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("default to dataDir", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)
		ws := WorkspaceEntry{}
		got, err := ws.ResolveWorktreeRoot("grafana")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(tmp, "gwt", "worktrees", "grafana")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run 'ResolveMembers|ResolveWorktreeRoot' -v`
Expected: FAIL — `ResolveMembers`/`ResolvedMember`/`ResolveWorktreeRoot` undefined.

- [ ] **Step 3: Implement in `config/workspace.go`**

Add imports `fmt`, `os`, `path/filepath` to the existing `import` block, then append:

```go
// ResolvedMember is a workspace member resolved to a concrete repo on disk.
type ResolvedMember struct {
	Name       string // canonical name as registered, e.g. "grafana/grafana"
	Short      string // last segment, used as the sibling directory name
	Path       string // repo path on disk
	MainBranch string // defaults to "main"
	IsPrimary  bool
}

// resolveMember maps a member reference (canonical or unique short segment)
// to its registered repo entry.
func (c *Config) resolveMember(ref string) (string, RepoEntry, error) {
	if e, ok := c.Repos[ref]; ok {
		return ref, e, nil
	}
	var matches []string
	for name := range c.Repos {
		if lastSegment(name) == ref {
			matches = append(matches, name)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], c.Repos[matches[0]], nil
	case 0:
		return "", RepoEntry{}, fmt.Errorf("member %q not registered in [repos]; run gwt init there", ref)
	default:
		return "", RepoEntry{}, fmt.Errorf("member %q is ambiguous; matches %v — use the full canonical name", ref, matches)
	}
}

// ResolveMembers resolves every member of a workspace, marking the primary.
// If Primary is empty, the first member is primary.
func (c *Config) ResolveMembers(ws WorkspaceEntry) ([]ResolvedMember, error) {
	if len(ws.Members) == 0 {
		return nil, fmt.Errorf("workspace has no members")
	}
	primaryCanon := ""
	if ws.Primary != "" {
		pc, _, err := c.resolveMember(ws.Primary)
		if err != nil {
			return nil, fmt.Errorf("primary: %w", err)
		}
		primaryCanon = pc
	}
	out := make([]ResolvedMember, 0, len(ws.Members))
	for i, ref := range ws.Members {
		canon, entry, err := c.resolveMember(ref)
		if err != nil {
			return nil, err
		}
		mb := entry.MainBranch
		if mb == "" {
			mb = "main"
		}
		out = append(out, ResolvedMember{
			Name:       canon,
			Short:      lastSegment(canon),
			Path:       entry.Path,
			MainBranch: mb,
			IsPrimary:  (primaryCanon == "" && i == 0) || canon == primaryCanon,
		})
	}
	return out, nil
}

// expandTilde expands a leading "~" to the user's home directory.
func expandTilde(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// ResolveWorktreeRoot returns the directory under which per-branch group dirs
// are created. Defaults to <dataDir>/worktrees/<workspace-name>.
func (ws WorkspaceEntry) ResolveWorktreeRoot(name string) (string, error) {
	if ws.WorktreeRoot != "" {
		return expandTilde(ws.WorktreeRoot)
	}
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "worktrees", name), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./config/ -v`
Expected: PASS (all config tests, including the new ones).

- [ ] **Step 5: Stage + report commit message**

```bash
git add config/workspace.go config/workspace_test.go
```
Suggested message: `feat(config): resolve workspace members and worktree root`

---

## Task 3: git — reusable add-arg parsing

**Files:**
- Modify: `git/git.go`
- Modify: `git/git_test.go`

**Interfaces:**
- Produces:
  - `type AddArgs struct { Flags []string; BranchFlag string; Branch string; Extra []string }`
  - `func ParseAddArgs(args []string) (AddArgs, error)`
  - `func (a AddArgs) Build(worktreePath string) []string`
  - `func BranchToDir(branch string) string`
- Consumes: nothing new.

This refactors the existing private `buildAddArgs` to reuse a single parser so the workspace code and the single-repo `Add` share branch parsing. Existing `Add` behavior must not change.

- [ ] **Step 1: Write the failing tests**

Append to `git/git_test.go`:

```go
func TestParseAddArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantBranch string
		wantFlag   string
		wantExtra  []string
		wantErr    bool
	}{
		{"existing branch", []string{"fix/login"}, "fix/login", "", nil, false},
		{"new branch", []string{"-b", "feat/x"}, "feat/x", "-b", nil, false},
		{"new branch with start-point", []string{"-b", "feat/x", "origin/main"}, "feat/x", "-b", []string{"origin/main"}, false},
		{"flag equals form", []string{"-b=feat/y"}, "feat/y", "-b", nil, false},
		{"no args", []string{}, "", "", nil, true},
		{"too many positional", []string{"a", "b"}, "", "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Branch != tt.wantBranch || got.BranchFlag != tt.wantFlag {
				t.Errorf("branch=%q flag=%q, want %q / %q", got.Branch, got.BranchFlag, tt.wantBranch, tt.wantFlag)
			}
			if len(got.Extra) != len(tt.wantExtra) {
				t.Errorf("extra=%v, want %v", got.Extra, tt.wantExtra)
			}
		})
	}
}

func TestAddArgsBuild(t *testing.T) {
	// New branch: flags + worktreePath + extra
	a, _ := ParseAddArgs([]string{"-b", "feat/x", "origin/main"})
	got := a.Build("/wt/grafana")
	want := []string{"-b", "feat/x", "/wt/grafana", "origin/main"}
	if !equalStrings(got, want) {
		t.Errorf("Build new = %v, want %v", got, want)
	}

	// Existing branch: flags + worktreePath + branch
	b, _ := ParseAddArgs([]string{"fix/login"})
	got = b.Build("/wt/grafana")
	want = []string{"/wt/grafana", "fix/login"}
	if !equalStrings(got, want) {
		t.Errorf("Build existing = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBranchToDir(t *testing.T) {
	if got := BranchToDir("fix/login-bug"); got != "fix-login-bug" {
		t.Errorf("BranchToDir = %q, want fix-login-bug", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./git/ -run 'ParseAddArgs|AddArgsBuild|BranchToDir' -v`
Expected: FAIL — `ParseAddArgs`/`AddArgs`/`BranchToDir` undefined.

- [ ] **Step 3: Refactor `git/git.go`**

Replace the existing `branchToDir` and `buildAddArgs` functions with:

```go
// BranchToDir converts a branch name into a filesystem-safe directory name.
func BranchToDir(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// AddArgs is the parsed form of `git worktree add` arguments.
type AddArgs struct {
	Flags      []string // all flags, including any branch flag + its value
	BranchFlag string   // "-b", "-B", "--orphan", or "" when checking out an existing branch
	Branch     string   // the branch name
	Extra      []string // trailing positionals (e.g. a start-point) when BranchFlag is set
}

// ParseAddArgs parses user-supplied `gwt add` arguments into AddArgs.
func ParseAddArgs(args []string) (AddArgs, error) {
	if len(args) == 0 {
		return AddArgs{}, fmt.Errorf("requires a branch name")
	}

	valueFlags := map[string]bool{"-b": true, "-B": true, "--orphan": true, "--reason": true}
	branchFlags := map[string]bool{"-b": true, "-B": true, "--orphan": true}

	var flags []string
	var positional []string
	var branchFlag, branchValue string
	pastFlags := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if pastFlags {
			positional = append(positional, arg)
			continue
		}
		if arg == "--" {
			pastFlags = true
			continue
		}
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if branchFlags[parts[0]] {
				branchFlag, branchValue = parts[0], parts[1]
			}
			flags = append(flags, arg)
			continue
		}
		// Support "-b=value" shorthand.
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if branchFlags[parts[0]] {
				branchFlag, branchValue = parts[0], parts[1]
				flags = append(flags, arg)
				continue
			}
		}
		if valueFlags[arg] {
			if i+1 >= len(args) {
				return AddArgs{}, fmt.Errorf("%s requires a value", arg)
			}
			if branchFlags[arg] {
				branchFlag, branchValue = arg, args[i+1]
			}
			flags = append(flags, arg, args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			continue
		}
		positional = append(positional, arg)
	}

	a := AddArgs{Flags: flags, BranchFlag: branchFlag}
	if branchFlag != "" {
		a.Branch = branchValue
		if len(positional) > 1 {
			return AddArgs{}, fmt.Errorf("too many positional arguments")
		}
		a.Extra = positional
	} else {
		if len(positional) == 0 {
			return AddArgs{}, fmt.Errorf("requires a branch name")
		}
		if len(positional) > 1 {
			return AddArgs{}, fmt.Errorf("too many positional arguments")
		}
		a.Branch = positional[0]
	}
	return a, nil
}

// Build produces the `git worktree add` argument list for a given worktree path.
func (a AddArgs) Build(worktreePath string) []string {
	out := append([]string{}, a.Flags...)
	if a.BranchFlag != "" {
		out = append(out, worktreePath)
		out = append(out, a.Extra...)
	} else {
		out = append(out, worktreePath, a.Branch)
	}
	return out
}

// buildAddArgs parses user args and returns transformed args for git worktree add
// plus the derived worktree path (worktree dir derived from the branch name).
func buildAddArgs(args []string, baseDir string) (gitArgs []string, worktreePath string, err error) {
	a, err := ParseAddArgs(args)
	if err != nil {
		return nil, "", err
	}
	worktreePath = filepath.Join(baseDir, BranchToDir(a.Branch))
	return a.Build(worktreePath), worktreePath, nil
}
```

Note: any remaining internal call to `branchToDir(...)` must be updated to `BranchToDir(...)`. Search with `grep -rn 'branchToDir' .` and fix.

- [ ] **Step 4: Run the full git package test suite**

Run: `go test ./git/ -v`
Expected: PASS — new parser tests pass AND all pre-existing `Add`/`buildAddArgs` tests still pass (behavior preserved).

- [ ] **Step 5: Stage + report commit message**

```bash
git add git/git.go git/git_test.go
```
Suggested message: `refactor(git): extract reusable ParseAddArgs and BranchToDir`

---

## Task 4: git — workspace worktree operations

**Files:**
- Create: `git/workspace.go`
- Create: `git/workspace_test.go`

**Interfaces:**
- Produces:
  - `func BranchExists(repoDir, branch string) bool`
  - `func MainBranchRef(repoDir, mainBranch string) string`
  - `func AddWorktreeAt(repoDir string, gitArgs []string) error`
  - `func RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) error`
  - `func RunSetup(command, dir string) error`
- Consumes: nothing new (uses `os/exec`, `bytes`).

- [ ] **Step 1: Write the failing tests**

Create `git/workspace_test.go`:

```go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepoWithMain creates a git repo at dir with one commit on "main".
func initRepoWithMain(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = testGitEnv()
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}

func TestBranchExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoWithMain(t, dir)

	if !BranchExists(dir, "main") {
		t.Error("BranchExists(main) = false, want true")
	}
	if BranchExists(dir, "nope") {
		t.Error("BranchExists(nope) = true, want false")
	}
}

func TestMainBranchRef(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	initRepoWithMain(t, dir)
	// No origin remote -> falls back to the local branch name.
	if got := MainBranchRef(dir, "main"); got != "main" {
		t.Errorf("MainBranchRef = %q, want main", got)
	}
}

func TestAddWorktreeAt(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)

	wt := filepath.Join(root, "wt", "feat")
	args := []string{"-b", "feat/x", wt, "main"}
	if err := AddWorktreeAt(repo, args); err != nil {
		t.Fatalf("AddWorktreeAt error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README")); err != nil {
		t.Errorf("worktree not created: %v", err)
	}
}

func TestRemoveMemberWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)
	wt := filepath.Join(root, "wt", "feat")
	if err := AddWorktreeAt(repo, []string{"-b", "feat/x", wt, "main"}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveMemberWorktree(repo, wt, false, false); err != nil {
		t.Fatalf("RemoveMemberWorktree error: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("worktree dir still present")
	}
	// Branch should be deleted (keepBranch=false).
	if BranchExists(repo, "feat/x") {
		t.Error("branch feat/x still exists, want deleted")
	}
}

func TestRemoveMemberWorktreeKeepBranch(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	initRepoWithMain(t, repo)
	wt := filepath.Join(root, "wt", "feat")
	if err := AddWorktreeAt(repo, []string{"-b", "feat/x", wt, "main"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMemberWorktree(repo, wt, true, false); err != nil {
		t.Fatal(err)
	}
	if !BranchExists(repo, "feat/x") {
		t.Error("branch feat/x deleted, want kept")
	}
}

func TestRunSetup(t *testing.T) {
	dir := t.TempDir()
	if err := RunSetup("touch marker", dir); err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Errorf("setup did not run in dir: %v", err)
	}
	if err := RunSetup("exit 3", dir); err == nil {
		t.Error("RunSetup should return error on non-zero exit")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./git/ -run 'BranchExists|MainBranchRef|AddWorktreeAt|RemoveMemberWorktree|RunSetup' -v`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Create `git/workspace.go`**

```go
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BranchExists reports whether branch exists locally or as an origin
// remote-tracking ref in the repo at repoDir.
func BranchExists(repoDir, branch string) bool {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", ref)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

// MainBranchRef returns "origin/<mainBranch>" when that remote-tracking ref
// exists, otherwise the local "<mainBranch>". Used as the base for new
// follower branches.
func MainBranchRef(repoDir, mainBranch string) string {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+mainBranch)
	if cmd.Run() == nil {
		return "origin/" + mainBranch
	}
	return mainBranch
}

// AddWorktreeAt runs `git -C repoDir worktree add <gitArgs>`, retrying once
// after `git fetch origin` if the ref was not found.
func AddWorktreeAt(repoDir string, gitArgs []string) error {
	base := []string{"-C", repoDir, "worktree", "add"}
	var stderr bytes.Buffer
	cmd := exec.Command("git", append(append([]string{}, base...), gitArgs...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "invalid reference:") {
			fetch := exec.Command("git", "-C", repoDir, "fetch", "origin")
			fetch.Stdout = os.Stdout
			fetch.Stderr = os.Stderr
			if ferr := fetch.Run(); ferr != nil {
				return fmt.Errorf("git fetch failed: %w", ferr)
			}
			retry := exec.Command("git", append(append([]string{}, base...), gitArgs...)...)
			retry.Stdout = os.Stdout
			retry.Stderr = os.Stderr
			retry.Stdin = os.Stdin
			if rerr := retry.Run(); rerr != nil {
				return fmt.Errorf("git worktree add failed: %w", rerr)
			}
			return nil
		}
		_, _ = os.Stderr.Write(stderr.Bytes())
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// RemoveMemberWorktree removes the worktree at worktreePath belonging to the
// repo at repoDir, then deletes its branch unless keepBranch is set.
func RemoveMemberWorktree(repoDir, worktreePath string, keepBranch, force bool) error {
	var branch string
	var buf bytes.Buffer
	bc := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	bc.Stdout = &buf
	if bc.Run() == nil {
		if b := strings.TrimSpace(buf.String()); b != "HEAD" {
			branch = b
		}
	}

	args := []string{"-C", repoDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree remove failed for %s: %w", worktreePath, err)
	}

	if !keepBranch && branch != "" {
		del := exec.Command("git", "-C", repoDir, "branch", "-d", branch)
		del.Stdout = os.Stdout
		del.Stderr = os.Stderr
		if err := del.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not delete branch %q in %s: %v\n", branch, repoDir, err)
		}
	}
	return nil
}

// RunSetup runs a shell command in dir, streaming stdio.
func RunSetup(command, dir string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup command %q (in %s) failed: %w", command, dir, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./git/ -v`
Expected: PASS (all git tests).

- [ ] **Step 5: Stage + report commit message**

```bash
git add git/workspace.go git/workspace_test.go
```
Suggested message: `feat(git): add per-member worktree add/remove and setup helpers`

---

## Task 5: main — `gwt add` fan-out

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: `config.WorkspaceEntry`, `config.ResolvedMember`, `config.ResolveMembers`, `config.WorkspaceEntry.ResolveWorktreeRoot`, `git.ParseAddArgs`, `git.AddArgs.Build`, `git.BranchToDir`, `git.BranchExists`, `git.MainBranchRef`, `git.AddWorktreeAt`, `git.RunSetup`, `git.WriteCdFile`.
- Produces:
  - `func runWorkspaceAdd(cfg *config.Config, wsName string, ws config.WorkspaceEntry, args []string) (cdPath string, err error)`

- [ ] **Step 1: Write the failing test**

Append to `main_test.go` (ensure imports include `os`, `os/exec`, `path/filepath`, `testing`, and `github.com/nicwestvold/gwt/config`):

```go
func mainTestInitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}

func TestRunWorkspaceAdd(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "grafana")
	follower := filepath.Join(root, "grafana-enterprise")
	mainTestInitRepo(t, primary)
	mainTestInitRepo(t, follower)

	wtRoot := filepath.Join(root, "worktrees")
	cfg := &config.Config{
		Repos: map[string]config.RepoEntry{
			"grafana/grafana":            {Path: primary, MainBranch: "main"},
			"grafana/grafana-enterprise": {Path: follower, MainBranch: "main"},
		},
	}
	ws := config.WorkspaceEntry{
		Members:      []string{"grafana", "grafana-enterprise"},
		Primary:      "grafana",
		Setup:        "touch setup-ran",
		SetupCwd:     "grafana",
		WorktreeRoot: wtRoot,
	}

	cd, err := runWorkspaceAdd(cfg, "grafana", ws, []string{"-b", "feat/x"})
	if err != nil {
		t.Fatalf("runWorkspaceAdd error: %v", err)
	}

	group := filepath.Join(wtRoot, "feat-x")
	if cd != filepath.Join(group, "grafana") {
		t.Errorf("cd = %q, want %q", cd, filepath.Join(group, "grafana"))
	}
	for _, short := range []string{"grafana", "grafana-enterprise"} {
		if _, err := os.Stat(filepath.Join(group, short, "README")); err != nil {
			t.Errorf("missing worktree for %s: %v", short, err)
		}
	}
	// Setup ran in the primary worktree.
	if _, err := os.Stat(filepath.Join(group, "grafana", "setup-ran")); err != nil {
		t.Errorf("setup did not run in primary: %v", err)
	}
	// Follower is on the mirrored branch.
	cmd := exec.Command("git", "-C", filepath.Join(group, "grafana-enterprise"), "rev-parse", "--abbrev-ref", "HEAD")
	out, _ := cmd.Output()
	if got := string(out); got != "feat/x\n" {
		t.Errorf("follower branch = %q, want feat/x", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRunWorkspaceAdd -v`
Expected: FAIL — `runWorkspaceAdd` undefined.

- [ ] **Step 3: Implement `runWorkspaceAdd` in `main.go`**

Add this function (and ensure `main.go` imports include `os`, `path/filepath`, `fmt`, the `config` and `git` packages — all already imported):

```go
// memberSetupDir returns the absolute path of the worktree whose short name
// matches ref (the setup_cwd), defaulting to the primary's worktree.
func memberSetupDir(group string, members []config.ResolvedMember, ref string) string {
	for _, m := range members {
		if ref != "" && (m.Short == ref || m.Name == ref) {
			return filepath.Join(group, m.Short)
		}
	}
	for _, m := range members {
		if m.IsPrimary {
			return filepath.Join(group, m.Short)
		}
	}
	return filepath.Join(group, members[0].Short)
}

// runWorkspaceAdd creates a worktree for every workspace member under a shared
// per-branch group directory, mirroring the branch to followers, then runs the
// workspace setup command. Returns the primary worktree path to cd into.
func runWorkspaceAdd(cfg *config.Config, wsName string, ws config.WorkspaceEntry, args []string) (string, error) {
	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		return "", err
	}
	parsed, err := git.ParseAddArgs(args)
	if err != nil {
		return "", err
	}
	root, err := ws.ResolveWorktreeRoot(wsName)
	if err != nil {
		return "", err
	}
	group := filepath.Join(root, git.BranchToDir(parsed.Branch))
	if err := os.MkdirAll(group, 0o755); err != nil {
		return "", fmt.Errorf("failed to create group dir: %w", err)
	}

	var created []string
	for _, m := range members {
		worktreePath := filepath.Join(group, m.Short)
		var gitArgs []string
		if m.IsPrimary {
			gitArgs = parsed.Build(worktreePath)
		} else if git.BranchExists(m.Path, parsed.Branch) {
			gitArgs = []string{worktreePath, parsed.Branch}
		} else {
			gitArgs = []string{"-b", parsed.Branch, worktreePath, git.MainBranchRef(m.Path, m.MainBranch)}
		}
		if err := git.AddWorktreeAt(m.Path, gitArgs); err != nil {
			return "", fmt.Errorf("creating worktree for %s failed: %w\ncreated so far: %v\nrun `gwt rm` from one of them to unwind", m.Name, err, created)
		}
		created = append(created, worktreePath)
		fmt.Printf("worktree: %s @ %s\n", worktreePath, parsed.Branch)
	}

	if ws.Setup != "" {
		setupDir := memberSetupDir(group, members, ws.SetupCwd)
		fmt.Printf("running setup: %s (in %s)\n", ws.Setup, setupDir)
		if err := git.RunSetup(ws.Setup, setupDir); err != nil {
			return "", err
		}
	}

	for _, m := range members {
		if m.IsPrimary {
			return filepath.Join(group, m.Short), nil
		}
	}
	return filepath.Join(group, members[0].Short), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestRunWorkspaceAdd -v`
Expected: PASS.

- [ ] **Step 5: Wire `runWorkspaceAdd` into `addCmd`**

In `main.go`, in `addCmd.RunE`, after `repo, err := git.NewRepo()` succeeds and before the `worktreeBaseDir` call, insert the workspace dispatch:

```go
		// Workspace fan-out: if this repo is a workspace member, create
		// worktrees for all members instead of the single-repo flow.
		if canonical, nameErr := repo.CanonicalName(); nameErr == nil {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if wsName, ws, ok := cfg.WorkspaceForRepo(canonical); ok {
					cd, addErr := runWorkspaceAdd(cfg, wsName, ws, args)
					if addErr != nil {
						return addErr
					}
					git.WriteCdFile(cd)
					return nil
				}
			}
		}
```

The existing single-repo code below this block is unchanged.

- [ ] **Step 6: Run the full suite + build**

Run: `go test ./... && go build ./...`
Expected: PASS / clean build.

- [ ] **Step 7: Stage + report commit message**

```bash
git add main.go main_test.go
```
Suggested message: `feat: fan out gwt add across workspace members`

---

## Task 6: main — `gwt remove` whole-group teardown

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: `config.ResolveMembers`, `git.RemoveMemberWorktree`, `git.CleanEmptyParents`, `config.WorkspaceEntry.ResolveWorktreeRoot`.
- Produces:
  - `func runWorkspaceRemove(cfg *config.Config, wsName string, ws config.WorkspaceEntry, currentWorktree string, keepBranch, force bool) (cdPath string, err error)`

- [ ] **Step 1: Write the failing test**

Append to `main_test.go`:

```go
func TestRunWorkspaceRemove(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "grafana")
	follower := filepath.Join(root, "grafana-enterprise")
	mainTestInitRepo(t, primary)
	mainTestInitRepo(t, follower)

	wtRoot := filepath.Join(root, "worktrees")
	cfg := &config.Config{
		Repos: map[string]config.RepoEntry{
			"grafana/grafana":            {Path: primary, MainBranch: "main"},
			"grafana/grafana-enterprise": {Path: follower, MainBranch: "main"},
		},
	}
	ws := config.WorkspaceEntry{
		Members:      []string{"grafana", "grafana-enterprise"},
		Primary:      "grafana",
		WorktreeRoot: wtRoot,
	}

	if _, err := runWorkspaceAdd(cfg, "grafana", ws, []string{"-b", "feat/x"}); err != nil {
		t.Fatalf("setup add failed: %v", err)
	}

	group := filepath.Join(wtRoot, "feat-x")
	cd, err := runWorkspaceRemove(cfg, "grafana", ws, filepath.Join(group, "grafana"), false, false)
	if err != nil {
		t.Fatalf("runWorkspaceRemove error: %v", err)
	}
	if cd != primary {
		t.Errorf("cd = %q, want %q (primary real repo)", cd, primary)
	}
	if _, err := os.Stat(group); !os.IsNotExist(err) {
		t.Error("group dir still present after remove")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRunWorkspaceRemove -v`
Expected: FAIL — `runWorkspaceRemove` undefined.

- [ ] **Step 3: Implement `runWorkspaceRemove` in `main.go`**

```go
// runWorkspaceRemove removes every member worktree in the branch group that
// currentWorktree belongs to, then cleans the empty group dir. Returns the
// primary's real repo path to cd back into.
func runWorkspaceRemove(cfg *config.Config, wsName string, ws config.WorkspaceEntry, currentWorktree string, keepBranch, force bool) (string, error) {
	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		return "", err
	}
	group := filepath.Dir(currentWorktree)

	for _, m := range members {
		worktreePath := filepath.Join(group, m.Short)
		if _, statErr := os.Stat(worktreePath); statErr != nil {
			continue // member worktree absent; skip
		}
		if err := git.RemoveMemberWorktree(m.Path, worktreePath, keepBranch, force); err != nil {
			return "", err
		}
	}

	root, err := ws.ResolveWorktreeRoot(wsName)
	if err == nil {
		_ = os.Remove(group)
		git.CleanEmptyParents(group, root)
	}

	primaryPath := members[0].Path
	for _, m := range members {
		if m.IsPrimary {
			primaryPath = m.Path
		}
	}
	return primaryPath, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestRunWorkspaceRemove -v`
Expected: PASS.

- [ ] **Step 5: Wire `runWorkspaceRemove` into `removeCmd`**

In `main.go`, in `removeCmd.RunE`, after `args, keepBranch := stripKeepBranch(args)` and after `repo, err := git.NewRepo()` succeeds, insert the workspace dispatch before the existing branch-resolution loop:

```go
		// Workspace teardown: if this repo is a workspace member, remove the
		// whole branch group.
		if canonical, nameErr := repo.CanonicalName(); nameErr == nil {
			if cfg, cfgErr := config.Load(); cfgErr == nil {
				if wsName, ws, ok := cfg.WorkspaceForRepo(canonical); ok {
					force := false
					for _, a := range args {
						if a == "--force" || a == "-f" {
							force = true
						}
					}
					var buf bytes.Buffer
					tl := exec.Command("git", "rev-parse", "--show-toplevel")
					tl.Stdout = &buf
					if tl.Run() != nil {
						return fmt.Errorf("not inside a worktree")
					}
					current := strings.TrimSpace(buf.String())
					cd, rmErr := runWorkspaceRemove(cfg, wsName, ws, current, keepBranch, force)
					if rmErr != nil {
						return rmErr
					}
					git.WriteCdFile(cd)
					return nil
				}
			}
		}
```

Ensure `main.go` imports include `bytes`, `os/exec`, and `strings` (add any missing). The existing single-repo removal code below is unchanged.

- [ ] **Step 6: Run the full suite + build + vet**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: PASS / clean.

- [ ] **Step 7: Stage + report commit message**

```bash
git add main.go main_test.go
```
Suggested message: `feat: tear down whole workspace group on gwt remove`

---

## Task 7: Documentation — README `### Workspaces`

**Files:**
- Modify: `README.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: Add the section**

Insert a new `### Workspaces` subsection under `## Usage`, after the `### Use` section and before `### Pass-through`. Keep it terse, matching the density of `### Clone`/`### Init`:

````markdown
### Workspaces

For codebases split across mutually-dependent sibling repos (e.g.
`grafana` + `grafana-enterprise`, which must sit next to each other so
`../grafana-enterprise` resolves), define a **workspace** in
`~/.config/gwt/config.toml`. Both repos must already be registered (via
`gwt init`/`gwt clone`).

```toml
[workspaces.grafana]
members       = ["grafana", "grafana-enterprise"]  # repos, by name; first is primary
primary       = "grafana"                            # cd target; followers mirror its branch
setup         = "make enterprise-dev"                # optional; runs after all worktrees exist
setup_cwd     = "grafana"                            # member dir the setup runs in (default: primary)
worktree_root = "~/Development/grafana/code/.worktrees"  # optional; default: gwt data dir
```

Then, from inside any member:

```bash
gwt add -b feat/x   # creates <root>/feat-x/grafana and <root>/feat-x/grafana-enterprise
                    # on branch feat/x, then runs setup; cd's into the primary
gwt rm              # removes the whole group's worktrees and cd's back to the primary repo
```

Followers mirror the branch: an existing branch is checked out, otherwise it is
created from the member's main branch. `gwt rm -k`/`--keep-branch` keeps each
member's branch.
````

- [ ] **Step 2: Verify**

Run: `grep -n '### Workspaces' README.md`
Expected: prints the new heading line.

- [ ] **Step 3: Stage + report commit message**

```bash
git add README.md
```
Suggested message: `docs: document workspaces in README`

---

## Self-Review

**Spec coverage:**
- Config schema (§1) → Task 1 + Task 2.
- `gwt add` fan-out incl. mirror branch + setup (§2) → Task 5 (uses Task 3/4 helpers).
- `gwt remove` whole-group (§3) → Task 6.
- Error handling: validate members before creating (Task 5 `ResolveMembers` first), partial-failure message (Task 5 Step 3), setup non-zero leaves worktrees (Task 4 `RunSetup` returns error after worktrees exist; Task 5 returns it without removing) → covered.
- Documentation (§5) → Task 7.
- Testing (§6) → tests in every task.
- Backward compatibility (§7) → Task 5/6 dispatch only when `WorkspaceForRepo` matches; otherwise existing flow.

**Placeholder scan:** none — every code/test step contains complete code and exact commands.

**Type consistency:** `WorkspaceEntry`, `ResolvedMember`, `AddArgs`, and the function signatures used in Tasks 5–6 match those defined in Tasks 1–4 (`ResolveMembers`, `ResolveWorktreeRoot`, `ParseAddArgs`, `AddArgs.Build`, `BranchToDir`, `BranchExists`, `MainBranchRef`, `AddWorktreeAt`, `RemoveMemberWorktree`, `RunSetup`, `CleanEmptyParents`). The follower base uses `MainBranchRef` (defined Task 4), keeping tests origin-free while resolving `origin/main` in production.
</content>
