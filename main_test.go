package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicwestvold/gwt/config"
	"github.com/nicwestvold/gwt/detect"
	"github.com/nicwestvold/gwt/git"
)

func TestWorktreeBaseDir(t *testing.T) {
	t.Run("bare repo returns repo dir", func(t *testing.T) {
		repo := &git.Repo{Dir: "/some/bare/repo", IsBare: true}
		baseDir, name, err := worktreeBaseDir(repo)
		if err != nil {
			t.Fatalf("worktreeBaseDir() error: %v", err)
		}
		if baseDir != "/some/bare/repo" {
			t.Errorf("baseDir = %q, want %q", baseDir, "/some/bare/repo")
		}
		// Name is derived from dir basename when no origin remote.
		if name != "repo" {
			t.Errorf("name = %q, want %q", name, "repo")
		}
	})

	t.Run("non-bare repo returns XDG data path", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)

		// Uses a non-existent dir — exercises CanonicalName's filepath.Base fallback.
		repo := &git.Repo{Dir: "/some/normal/repo", IsBare: false}
		baseDir, name, err := worktreeBaseDir(repo)
		if err != nil {
			t.Fatalf("worktreeBaseDir() error: %v", err)
		}
		want := filepath.Join(tmp, "gwt", "worktrees", "repo")
		if baseDir != want {
			t.Errorf("baseDir = %q, want %q", baseDir, want)
		}
		if name != "repo" {
			t.Errorf("name = %q, want %q", name, "repo")
		}
	})
}

func TestStripKeepBranchFlag(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantArgs       []string
		wantKeepBranch bool
	}{
		{
			name:           "no flag",
			args:           []string{"my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: false,
		},
		{
			name:           "long flag",
			args:           []string{"--keep-branch", "my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "short flag",
			args:           []string{"-k", "my-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "flag after branch",
			args:           []string{"my-branch", "--keep-branch"},
			wantArgs:       []string{"my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "mixed with git flags",
			args:           []string{"--force", "-k", "my-branch"},
			wantArgs:       []string{"--force", "my-branch"},
			wantKeepBranch: true,
		},
		{
			name:           "no args just flag",
			args:           []string{"--keep-branch"},
			wantArgs:       []string{},
			wantKeepBranch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotKeep := stripKeepBranch(tt.args)
			if gotKeep != tt.wantKeepBranch {
				t.Errorf("keepBranch = %v, want %v", gotKeep, tt.wantKeepBranch)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("args = %v (len %d), want %v (len %d)", gotArgs, len(gotArgs), tt.wantArgs, len(tt.wantArgs))
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestShellWrapperContainsCommands(t *testing.T) {
	for _, cmd := range []string{"add", "clone", "rm", "remove", "use"} {
		if !strings.Contains(shellWrapper, `"`+cmd+`"`) {
			t.Errorf("shellWrapper missing command %q", cmd)
		}
	}
}

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
	primary := filepath.Join(root, "app")
	follower := filepath.Join(root, "app-plugins")
	mainTestInitRepo(t, primary)
	mainTestInitRepo(t, follower)

	wtRoot := filepath.Join(root, "worktrees")
	cfg := &config.Config{
		Repos: map[string]config.RepoEntry{
			"acme/app":         {Path: primary, MainBranch: "main"},
			"acme/app-plugins": {Path: follower, MainBranch: "main"},
		},
	}
	ws := config.WorkspaceEntry{
		Members:      []string{"app", "app-plugins"},
		Primary:      "app",
		Setup:        "touch setup-ran",
		SetupCwd:     "app",
		WorktreeRoot: wtRoot,
	}

	cd, err := runWorkspaceAdd(cfg, "app", ws, []string{"-b", "feat/x"})
	if err != nil {
		t.Fatalf("runWorkspaceAdd error: %v", err)
	}

	group := filepath.Join(wtRoot, "feat-x")
	if cd != filepath.Join(group, "app") {
		t.Errorf("cd = %q, want %q", cd, filepath.Join(group, "app"))
	}
	for _, short := range []string{"app", "app-plugins"} {
		if _, err := os.Stat(filepath.Join(group, short, "README")); err != nil {
			t.Errorf("missing worktree for %s: %v", short, err)
		}
	}
	// Setup ran in the primary worktree.
	if _, err := os.Stat(filepath.Join(group, "app", "setup-ran")); err != nil {
		t.Errorf("setup did not run in primary: %v", err)
	}
	// Follower is on the mirrored branch.
	cmd := exec.Command("git", "-C", filepath.Join(group, "app-plugins"), "rev-parse", "--abbrev-ref", "HEAD")
	out, _ := cmd.Output()
	if got := string(out); got != "feat/x\n" {
		t.Errorf("follower branch = %q, want feat/x", got)
	}
}

func TestPartitionRemoveArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantForce      bool
		wantPositional []string
	}{
		{
			name:           "no args",
			args:           []string{},
			wantForce:      false,
			wantPositional: nil,
		},
		{
			name:           "force long flag only",
			args:           []string{"--force"},
			wantForce:      true,
			wantPositional: nil,
		},
		{
			name:           "force short flag only",
			args:           []string{"-f"},
			wantForce:      true,
			wantPositional: nil,
		},
		{
			name:           "force with value",
			args:           []string{"--force=true"},
			wantForce:      true,
			wantPositional: nil,
		},
		{
			name:           "positional branch",
			args:           []string{"feat/x"},
			wantForce:      false,
			wantPositional: []string{"feat/x"},
		},
		{
			name:           "force and positional",
			args:           []string{"--force", "feat/x"},
			wantForce:      true,
			wantPositional: []string{"feat/x"},
		},
		{
			name:           "double dash separator",
			args:           []string{"--force", "--", "feat/x"},
			wantForce:      true,
			wantPositional: []string{"feat/x"},
		},
		{
			name:           "args after double dash treated as positionals",
			args:           []string{"--", "--force"},
			wantForce:      false,
			wantPositional: []string{"--force"},
		},
		{
			name:           "other flag not force",
			args:           []string{"--keep-branch"},
			wantForce:      false,
			wantPositional: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotForce, gotPos := partitionRemoveArgs(tt.args)
			if gotForce != tt.wantForce {
				t.Errorf("force = %v, want %v", gotForce, tt.wantForce)
			}
			if len(gotPos) != len(tt.wantPositional) {
				t.Fatalf("positionals = %v (len %d), want %v (len %d)",
					gotPos, len(gotPos), tt.wantPositional, len(tt.wantPositional))
			}
			for i := range gotPos {
				if gotPos[i] != tt.wantPositional[i] {
					t.Errorf("positionals[%d] = %q, want %q", i, gotPos[i], tt.wantPositional[i])
				}
			}
		})
	}
}

func TestRunWorkspaceRemove(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "app")
	follower := filepath.Join(root, "app-plugins")
	mainTestInitRepo(t, primary)
	mainTestInitRepo(t, follower)

	wtRoot := filepath.Join(root, "worktrees")
	cfg := &config.Config{
		Repos: map[string]config.RepoEntry{
			"acme/app":         {Path: primary, MainBranch: "main"},
			"acme/app-plugins": {Path: follower, MainBranch: "main"},
		},
	}
	ws := config.WorkspaceEntry{
		Members:      []string{"app", "app-plugins"},
		Primary:      "app",
		WorktreeRoot: wtRoot,
	}

	if _, err := runWorkspaceAdd(cfg, "app", ws, []string{"-b", "feat/x"}); err != nil {
		t.Fatalf("setup add failed: %v", err)
	}

	group := filepath.Join(wtRoot, "feat-x")
	cd, err := runWorkspaceRemove(cfg, "app", ws, group, false, false)
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

func TestResolveWorkspaceGroup(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "app")
	follower := filepath.Join(root, "app-plugins")
	mainTestInitRepo(t, primary)
	mainTestInitRepo(t, follower)

	wtRoot := filepath.Join(root, "worktrees")
	cfg := &config.Config{
		Repos: map[string]config.RepoEntry{
			"acme/app":         {Path: primary, MainBranch: "main"},
			"acme/app-plugins": {Path: follower, MainBranch: "main"},
		},
	}
	ws := config.WorkspaceEntry{
		Members:      []string{"app", "app-plugins"},
		Primary:      "app",
		WorktreeRoot: wtRoot,
	}

	if _, err := runWorkspaceAdd(cfg, "app", ws, []string{"-b", "feat/x"}); err != nil {
		t.Fatalf("setup add failed: %v", err)
	}

	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		t.Fatalf("ResolveMembers error: %v", err)
	}
	wantGroup, err := filepath.EvalSymlinks(filepath.Join(wtRoot, "feat-x"))
	if err != nil {
		t.Fatalf("EvalSymlinks error: %v", err)
	}

	resolves := []struct {
		name string
		arg  string
	}{
		{"branch name", "feat/x"},
		{"flattened group-dir name", "feat-x"},
		{"member worktree path", filepath.Join(wtRoot, "feat-x", "app")},
		{"follower worktree path", filepath.Join(wtRoot, "feat-x", "app-plugins")},
		{"group dir path", filepath.Join(wtRoot, "feat-x")},
	}
	for _, tt := range resolves {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWorkspaceGroup(wtRoot, members, tt.arg)
			if err != nil {
				t.Fatalf("resolveWorkspaceGroup(%q) error: %v", tt.arg, err)
			}
			if got != wantGroup {
				t.Errorf("group = %q, want %q", got, wantGroup)
			}
		})
	}

	rejects := []struct {
		name string
		arg  string
	}{
		{"unknown branch", "feat/nope"},
		{"nonexistent path", filepath.Join(wtRoot, "no-such-dir")},
		{"main branch resolves to real repo", "main"},
		{"real repo path", primary},
		{"path outside worktree root", root},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveWorkspaceGroup(wtRoot, members, tt.arg); err == nil {
				t.Errorf("resolveWorkspaceGroup(%q) succeeded, want error", tt.arg)
			}
		})
	}
}

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

func TestIsSizeFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"short", []string{"-s"}, true},
		{"long", []string{"--size"}, true},
		{"none", nil, false},
		{"empty", []string{}, false},
		{"other flag", []string{"--porcelain"}, false},
		{"size plus other", []string{"-s", "--porcelain"}, false},
		{"two size flags", []string{"-s", "--size"}, false},
		{"positional", []string{"main"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSizeFlag(c.args); got != c.want {
				t.Errorf("isSizeFlag(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}
