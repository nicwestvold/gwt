package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type RepoEntry struct {
	Path           string   `toml:"path"`
	Bare           bool     `toml:"bare,omitempty"`
	PackageManager string   `toml:"package_manager,omitempty"`
	VersionManager string   `toml:"version_manager,omitempty"`
	CopyFiles      []string `toml:"copy_files,omitempty"`
	MainBranch     string   `toml:"main_branch,omitempty"`
}

type Config struct {
	Repos map[string]RepoEntry `toml:"repos"`
}

func ConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gwt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gwt")
}

func DataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "gwt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "gwt")
}

func Path() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

func Load() (*Config, error) {
	cfg := &Config{Repos: make(map[string]RepoEntry)}
	data, err := os.ReadFile(Path())
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
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(Path())
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) Lookup(name string) (*RepoEntry, bool) {
	entry, ok := c.Repos[name]
	if !ok {
		return nil, false
	}
	return &entry, true
}

func (c *Config) Register(name string, entry RepoEntry) {
	c.Repos[name] = entry
}
