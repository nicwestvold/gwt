// Package detect infers a repo's version manager and package manager from the
// files it declares, so gwt can generate a post-checkout hook without the user
// restating them.
package detect

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
