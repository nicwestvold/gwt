package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const configFile = ".gwt.json"

type Config struct {
	MainBranch string   `json:"main_branch"`
	CopyFiles  []string `json:"copy_files"`
}

func DefaultConfig() Config {
	return Config{
		MainBranch: "main",
		CopyFiles:  nil,
	}
}

func (c Config) IsDefault() bool {
	return c.MainBranch == "main" && len(c.CopyFiles) == 0
}

func Load(repoDir string) (Config, error) {
	path := filepath.Join(repoDir, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(repoDir string, cfg Config) error {
	path := filepath.Join(repoDir, configFile)

	if cfg.IsDefault() {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
