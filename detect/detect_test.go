package detect

import (
	"os"
	"os/exec"
	"path/filepath"
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
