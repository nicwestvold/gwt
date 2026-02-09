package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MainBranch != "main" {
		t.Errorf("MainBranch = %q, want %q", cfg.MainBranch, "main")
	}
	if cfg.CopyFiles != nil {
		t.Errorf("CopyFiles = %v, want nil", cfg.CopyFiles)
	}
}

func TestIsDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"default config", DefaultConfig(), true},
		{"different MainBranch", Config{MainBranch: "master"}, false},
		{"non-empty CopyFiles", Config{MainBranch: "main", CopyFiles: []string{".env"}}, false},
		{"both differ", Config{MainBranch: "dev", CopyFiles: []string{"a"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsDefault()
			if got != tt.want {
				t.Errorf("IsDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("missing file returns default config", func(t *testing.T) {
		dir := t.TempDir()
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.IsDefault() {
			t.Errorf("expected default config, got %+v", cfg)
		}
	})

	t.Run("valid JSON parsed correctly", func(t *testing.T) {
		dir := t.TempDir()
		data := []byte(`{"main_branch":"develop","copy_files":[".env","config.yaml"]}`)
		if err := os.WriteFile(filepath.Join(dir, ".gwt.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MainBranch != "develop" {
			t.Errorf("MainBranch = %q, want %q", cfg.MainBranch, "develop")
		}
		if len(cfg.CopyFiles) != 2 || cfg.CopyFiles[0] != ".env" || cfg.CopyFiles[1] != "config.yaml" {
			t.Errorf("CopyFiles = %v, want [.env config.yaml]", cfg.CopyFiles)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".gwt.json"), []byte("{bad json"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Load(dir)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestSave(t *testing.T) {
	t.Run("default config does not write file", func(t *testing.T) {
		dir := t.TempDir()
		if err := Save(dir, DefaultConfig()); err != nil {
			t.Fatal(err)
		}

		_, err := os.Stat(filepath.Join(dir, ".gwt.json"))
		if !os.IsNotExist(err) {
			t.Errorf("expected no file, got err=%v", err)
		}
	})

	t.Run("default config removes existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gwt.json")
		if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}

		if err := Save(dir, DefaultConfig()); err != nil {
			t.Fatal(err)
		}

		_, err := os.Stat(path)
		if !os.IsNotExist(err) {
			t.Errorf("expected file to be removed, got err=%v", err)
		}
	})

	t.Run("non-default config writes JSON with trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		cfg := Config{MainBranch: "develop", CopyFiles: []string{".env"}}
		if err := Save(dir, cfg); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(dir, ".gwt.json"))
		if err != nil {
			t.Fatal(err)
		}

		str := string(data)
		if str[len(str)-1] != '\n' {
			t.Error("expected trailing newline")
		}
		// Verify it's indented (contains two-space indent)
		if !containsStr(str, "  ") {
			t.Error("expected indented JSON")
		}
	})

	t.Run("round-trip Save then Load", func(t *testing.T) {
		dir := t.TempDir()
		original := Config{MainBranch: "feature", CopyFiles: []string{"a.txt", "b.txt"}}
		if err := Save(dir, original); err != nil {
			t.Fatal(err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}

		if loaded.MainBranch != original.MainBranch {
			t.Errorf("MainBranch = %q, want %q", loaded.MainBranch, original.MainBranch)
		}
		if len(loaded.CopyFiles) != len(original.CopyFiles) {
			t.Fatalf("CopyFiles length = %d, want %d", len(loaded.CopyFiles), len(original.CopyFiles))
		}
		for i := range original.CopyFiles {
			if loaded.CopyFiles[i] != original.CopyFiles[i] {
				t.Errorf("CopyFiles[%d] = %q, want %q", i, loaded.CopyFiles[i], original.CopyFiles[i])
			}
		}
	})
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
