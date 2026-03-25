package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
)

// RepoEntry holds the configuration for a single registered repository.
type RepoEntry struct {
	Path           string   `toml:"path"`
	Bare           bool     `toml:"bare,omitempty"`
	PackageManager string   `toml:"package_manager,omitempty"`
	VersionManager string   `toml:"version_manager,omitempty"`
	CopyFiles      []string `toml:"copy_files,omitempty"`
	MainBranch     string   `toml:"main_branch,omitempty"`
}

// Config is the top-level gwt configuration, keyed by canonical repo name.
type Config struct {
	Repos map[string]RepoEntry `toml:"repos"`
}

func ConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gwt"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gwt"), nil
}

func DataDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "gwt"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "gwt"), nil
}

func configPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (*Config, error) {
	cfg := &Config{Repos: make(map[string]RepoEntry)}
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Repos == nil {
		cfg.Repos = make(map[string]RepoEntry)
	}
	return cfg, nil
}

func (c *Config) Save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, p)
}

func (c *Config) Lookup(name string) (RepoEntry, bool) {
	entry, ok := c.Repos[name]
	return entry, ok
}

func (c *Config) Register(name string, entry RepoEntry) {
	c.Repos[name] = entry
}

// Equal reports whether two RepoEntry values are identical.
func (e RepoEntry) Equal(other RepoEntry) bool {
	return e.Path == other.Path &&
		e.Bare == other.Bare &&
		e.PackageManager == other.PackageManager &&
		e.VersionManager == other.VersionManager &&
		e.MainBranch == other.MainBranch &&
		(len(e.CopyFiles) == 0 && len(other.CopyFiles) == 0 || slices.Equal(e.CopyFiles, other.CopyFiles))
}
