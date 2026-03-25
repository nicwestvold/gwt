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
			Bare:           true,
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
	if !entry.Bare {
		t.Error("Bare = false, want true")
	}
	if entry.PackageManager != "pnpm" {
		t.Errorf("PackageManager = %q, want %q", entry.PackageManager, "pnpm")
	}
	if entry.VersionManager != "mise" {
		t.Errorf("VersionManager = %q, want %q", entry.VersionManager, "mise")
	}
	if entry.MainBranch != "main" {
		t.Errorf("MainBranch = %q, want %q", entry.MainBranch, "main")
	}
	if len(entry.CopyFiles) != 2 || entry.CopyFiles[0] != ".env" || entry.CopyFiles[1] != ".env.local" {
		t.Errorf("CopyFiles = %v, want [.env .env.local]", entry.CopyFiles)
	}

	// Minimal entry should survive round-trip with zero-value fields.
	minimal, ok := loaded.Lookup("nicwestvold/gwt")
	if !ok {
		t.Fatal("Lookup(nicwestvold/gwt) not found")
	}
	if minimal.Bare {
		t.Error("minimal entry: Bare = true, want false")
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

func TestLoadCorruptFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "gwt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("not valid {{{{ toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for corrupt TOML")
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

func TestConfigDir(t *testing.T) {
	t.Run("uses XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		got, err := ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir() error: %v", err)
		}
		want := "/custom/config/gwt"
		if got != want {
			t.Errorf("ConfigDir() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to default", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		home, _ := os.UserHomeDir()
		got, err := ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir() error: %v", err)
		}
		want := filepath.Join(home, ".config", "gwt")
		if got != want {
			t.Errorf("ConfigDir() = %q, want %q", got, want)
		}
	})
}

func TestDataDir(t *testing.T) {
	t.Run("uses XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/data")
		got, err := DataDir()
		if err != nil {
			t.Fatalf("DataDir() error: %v", err)
		}
		want := "/custom/data/gwt"
		if got != want {
			t.Errorf("DataDir() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to default", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		home, _ := os.UserHomeDir()
		got, err := DataDir()
		if err != nil {
			t.Fatalf("DataDir() error: %v", err)
		}
		want := filepath.Join(home, ".local", "share", "gwt")
		if got != want {
			t.Errorf("DataDir() = %q, want %q", got, want)
		}
	})
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}
	want := "/custom/config/gwt/config.toml"
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestSaveAtomicity(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Save initial config.
	cfg := &Config{Repos: map[string]RepoEntry{
		"owner/repo": {Path: "/first"},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("first Save() error: %v", err)
	}

	// Overwrite with a second config — should not leave temp files.
	cfg2 := &Config{Repos: map[string]RepoEntry{
		"owner/repo": {Path: "/second"},
	}}
	if err := cfg2.Save(); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}

	// Verify the saved content.
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	entry, ok := loaded.Lookup("owner/repo")
	if !ok {
		t.Fatal("entry not found after second save")
	}
	if entry.Path != "/second" {
		t.Errorf("Path = %q, want %q", entry.Path, "/second")
	}

	// Verify no temp files left behind.
	dir := filepath.Join(tmp, "gwt")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file in config dir: %s", e.Name())
		}
	}
}
