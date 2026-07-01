package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &Config{
		Repos: map[string]RepoEntry{
			"acme/app":            {Path: "/code/app", MainBranch: "main"},
			"acme/app-plugins": {Path: "/code/app-plugins"},
		},
		Workspaces: map[string]WorkspaceEntry{
			"app": {
				Members:      []string{"app", "app-plugins"},
				Primary:      "app",
				Setup:        "make dev",
				SetupCwd:     "app",
				WorktreeRoot: "~/code/.worktrees",
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Confirm it persisted under a [workspaces.app] table.
	data, err := os.ReadFile(filepath.Join(tmp, "gwt", "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "[workspaces.app]") {
		t.Fatalf("config missing [workspaces.app]:\n%s", data)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	ws, ok := loaded.Workspaces["app"]
	if !ok {
		t.Fatal("workspace app not loaded")
	}
	if ws.Primary != "app" || ws.Setup != "make dev" || ws.SetupCwd != "app" {
		t.Errorf("unexpected workspace: %+v", ws)
	}
	if len(ws.Members) != 2 || ws.Members[0] != "app" || ws.Members[1] != "app-plugins" {
		t.Errorf("members = %v", ws.Members)
	}
	if ws.WorktreeRoot != "~/code/.worktrees" {
		t.Errorf("WorktreeRoot = %q", ws.WorktreeRoot)
	}
}

func TestWorkspaceForRepo(t *testing.T) {
	cfg := &Config{
		Repos: map[string]RepoEntry{
			"acme/app":            {Path: "/code/app"},
			"acme/app-plugins": {Path: "/code/app-plugins"},
		},
		Workspaces: map[string]WorkspaceEntry{
			"app": {Members: []string{"app", "app-plugins"}, Primary: "app"},
		},
	}
	tests := []struct {
		name      string
		canonical string
		wantName  string
		wantOK    bool
	}{
		{"by canonical", "acme/app", "app", true},
		{"by short segment", "acme/app-plugins", "app", true},
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

func TestResolveMembers(t *testing.T) {
	cfg := &Config{
		Repos: map[string]RepoEntry{
			"acme/app":            {Path: "/code/app", MainBranch: "main"},
			"acme/app-plugins": {Path: "/code/app-plugins"}, // no main_branch -> defaults
		},
	}
	ws := WorkspaceEntry{Members: []string{"app", "app-plugins"}, Primary: "app"}

	members, err := cfg.ResolveMembers(ws)
	if err != nil {
		t.Fatalf("ResolveMembers error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}
	if members[0].Name != "acme/app" || members[0].Short != "app" || !members[0].IsPrimary {
		t.Errorf("member[0] = %+v", members[0])
	}
	if members[0].MainBranch != "main" {
		t.Errorf("member[0].MainBranch = %q, want main", members[0].MainBranch)
	}
	if members[1].Short != "app-plugins" || members[1].IsPrimary {
		t.Errorf("member[1] = %+v", members[1])
	}
	if members[1].MainBranch != "main" {
		t.Errorf("member[1].MainBranch = %q, want defaulted main", members[1].MainBranch)
	}
}

func TestResolveMembersErrors(t *testing.T) {
	cfg := &Config{Repos: map[string]RepoEntry{
		"a/app": {Path: "/a/app"},
		"b/app": {Path: "/b/app"},
		"x/only":    {Path: "/x/only"},
	}}

	if _, err := cfg.ResolveMembers(WorkspaceEntry{Members: []string{"missing"}}); err == nil {
		t.Error("expected error for unregistered member")
	}

	_, ambiguousErr := cfg.ResolveMembers(WorkspaceEntry{Members: []string{"app"}})
	if ambiguousErr == nil {
		t.Error("expected error for ambiguous short match")
	} else {
		// Both matches must appear in sorted order so the message is deterministic.
		msg := ambiguousErr.Error()
		ia := strings.Index(msg, "a/app")
		ib := strings.Index(msg, "b/app")
		if ia < 0 || ib < 0 {
			t.Errorf("ambiguous error should name both matches, got: %v", ambiguousErr)
		}
		if ia > ib {
			t.Errorf("ambiguous error names should appear in sorted order (a/app before b/app), got: %v", ambiguousErr)
		}
	}
}

func TestResolveWorktreeRoot(t *testing.T) {
	t.Run("explicit with tilde", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		ws := WorkspaceEntry{WorktreeRoot: "~/code/.worktrees"}
		got, err := ws.ResolveWorktreeRoot("app")
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
		got, err := ws.ResolveWorktreeRoot("app")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(tmp, "gwt", "worktrees", "app")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
