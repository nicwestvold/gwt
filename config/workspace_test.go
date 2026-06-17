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
	if !strings.Contains(string(data), "[workspaces.grafana]") {
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
