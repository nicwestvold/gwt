package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &Config{Repos: map[string]RepoEntry{
		"grafana/metrics-drilldown": {
			Path:           "/home/user/code/metrics-drilldown",
			PackageManager: "pnpm",
			VersionManager: "mise",
			CopyFiles:      []string{".env", ".env.local"},
			MainBranch:     "main",
		},
		"nicwestvold/gwt": {
			Path: "/home/user/dev/gwt",
		},
	}}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(loaded.Repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(loaded.Repos))
	}

	entry, ok := loaded.Lookup("grafana/metrics-drilldown")
	if !ok {
		t.Fatal("Lookup(grafana/metrics-drilldown) not found")
	}
	if entry.Path != "/home/user/code/metrics-drilldown" {
		t.Errorf("Path = %q, want %q", entry.Path, "/home/user/code/metrics-drilldown")
	}
	if entry.PackageManager != "pnpm" {
		t.Errorf("PackageManager = %q, want %q", entry.PackageManager, "pnpm")
	}
	if len(entry.CopyFiles) != 2 {
		t.Errorf("CopyFiles = %v, want 2 items", entry.CopyFiles)
	}
}

func TestLoadMissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("got %d repos, want 0", len(cfg.Repos))
	}
}

func TestRegisterAndLookup(t *testing.T) {
	cfg := &Config{Repos: make(map[string]RepoEntry)}

	_, ok := cfg.Lookup("owner/repo")
	if ok {
		t.Fatal("Lookup should return false for missing entry")
	}

	cfg.Register("owner/repo", RepoEntry{Path: "/some/path"})

	entry, ok := cfg.Lookup("owner/repo")
	if !ok {
		t.Fatal("Lookup should return true after Register")
	}
	if entry.Path != "/some/path" {
		t.Errorf("Path = %q, want %q", entry.Path, "/some/path")
	}
}

func TestDataDir(t *testing.T) {
	t.Run("uses XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/data")
		got := DataDir()
		want := "/custom/data/gwt"
		if got != want {
			t.Errorf("DataDir() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to default", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		home, _ := os.UserHomeDir()
		got := DataDir()
		want := filepath.Join(home, ".local", "share", "gwt")
		if got != want {
			t.Errorf("DataDir() = %q, want %q", got, want)
		}
	})
}
