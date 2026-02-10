package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		pm   string
		want string
	}{
		{"yarn", "yarn build"},
		{"pnpm", "pnpm run build"},
		{"npm", "npm run build"},
	}

	for _, tt := range tests {
		t.Run(tt.pm, func(t *testing.T) {
			d := HookData{PackageManager: tt.pm}
			got := d.BuildCommand()
			if got != tt.want {
				t.Errorf("BuildCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name     string
		data     HookData
		contains []string
		excludes []string
	}{
		{
			name: "copy files only",
			data: HookData{
				BasePath:  "/repo",
				CopyFiles: []string{".env", "config.json"},
			},
			contains: []string{"cp", ".env", "config.json", "/repo"},
			excludes: []string{"install", "run build"},
		},
		{
			name: "package manager only no version manager",
			data: HookData{
				PackageManager: "pnpm",
			},
			contains: []string{"pnpm install", "pnpm run build"},
			excludes: []string{"cp", "mise", "asdf"},
		},
		{
			name: "with mise",
			data: HookData{
				PackageManager: "pnpm",
				VersionManager: "mise",
			},
			contains: []string{"mise exec --", "pnpm install", "pnpm run build"},
		},
		{
			name: "with asdf",
			data: HookData{
				PackageManager: "npm",
				VersionManager: "asdf",
			},
			contains: []string{"asdf.sh", "npm install", "npm run build"},
		},
		{
			name:     "empty data",
			data:     HookData{},
			contains: []string{"#!/bin/bash", "if [["},
			excludes: []string{"cp", "install"},
		},
		{
			name: "no copy files nil",
			data: HookData{
				CopyFiles: nil,
			},
			excludes: []string{"cp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate(tt.data)
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}

			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("output missing %q\n---\n%s", s, got)
				}
			}

			for _, s := range tt.excludes {
				if strings.Contains(got, s) {
					t.Errorf("output should not contain %q\n---\n%s", s, got)
				}
			}
		})
	}
}

func TestInstall(t *testing.T) {
	data := HookData{
		BasePath:       "/repo",
		CopyFiles:      []string{".env"},
		PackageManager: "npm",
	}

	t.Run("creates hook file", func(t *testing.T) {
		dir := t.TempDir()
		hooksDir := filepath.Join(dir, "hooks")

		if err := Install(hooksDir, data, false); err != nil {
			t.Fatalf("Install() error: %v", err)
		}

		hookPath := filepath.Join(hooksDir, "post-checkout")

		info, err := os.Stat(hookPath)
		if err != nil {
			t.Fatalf("hook file not found: %v", err)
		}

		if info.Mode().Perm() != 0755 {
			t.Errorf("mode = %o, want 0755", info.Mode().Perm())
		}

		content, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatalf("reading hook: %v", err)
		}

		expected, err := Generate(data)
		if err != nil {
			t.Fatalf("Generate() error: %v", err)
		}

		if string(content) != expected {
			t.Error("hook content does not match Generate() output")
		}
	})

	t.Run("creates hooks directory", func(t *testing.T) {
		dir := t.TempDir()
		hooksDir := filepath.Join(dir, "nested", "hooks")

		if err := Install(hooksDir, data, false); err != nil {
			t.Fatalf("Install() error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(hooksDir, "post-checkout")); err != nil {
			t.Fatalf("hook file not found: %v", err)
		}
	})

	t.Run("refuses overwrite without force", func(t *testing.T) {
		dir := t.TempDir()
		hooksDir := filepath.Join(dir, "hooks")

		if err := Install(hooksDir, data, false); err != nil {
			t.Fatalf("first Install() error: %v", err)
		}

		err := Install(hooksDir, data, false)
		if err == nil {
			t.Fatal("expected error on second install, got nil")
		}

		if !strings.Contains(err.Error(), "use --force") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "use --force")
		}
	})

	t.Run("force overwrites", func(t *testing.T) {
		dir := t.TempDir()
		hooksDir := filepath.Join(dir, "hooks")

		if err := Install(hooksDir, data, false); err != nil {
			t.Fatalf("first Install() error: %v", err)
		}

		newData := HookData{PackageManager: "pnpm"}
		if err := Install(hooksDir, newData, true); err != nil {
			t.Fatalf("force Install() error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(hooksDir, "post-checkout"))
		if err != nil {
			t.Fatalf("reading hook: %v", err)
		}

		if strings.Contains(string(content), ".env") {
			t.Error("file still has old content after force overwrite")
		}

		if !strings.Contains(string(content), "pnpm install") {
			t.Error("file missing new content after force overwrite")
		}
	})
}
