package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	configDirName  = ".ticket"
	configFileName = "config.yaml"
)

// Config stores tk project configuration.
type Config struct {
	CentralRoot  string                   `yaml:"central_root,omitempty" json:"central_root,omitempty"`
	GitEmail     string                   `yaml:"git_email,omitempty" json:"git_email,omitempty"`
	GitName      string                   `yaml:"git_name,omitempty" json:"git_name,omitempty"`
	DefaultStore string                   `yaml:"default_store,omitempty" json:"default_store,omitempty"`
	Projects     map[string]ProjectConfig `yaml:"projects"`
}

// ProjectConfig stores per-project settings.
type ProjectConfig struct {
	Path         string `yaml:"path" json:"path"`
	Store        string `yaml:"store" json:"store"`
	AutoLink     bool   `yaml:"auto_link" json:"auto_link"`
	AutoClose    bool   `yaml:"auto_close" json:"auto_close"`
	RegisteredAt string `yaml:"registered_at,omitempty" json:"registered_at,omitempty"`
}

// Load reads ~/.tk/config.yaml. Missing or empty file returns empty config.
func Load() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Projects: map[string]ProjectConfig{}}, nil
		}
		return Config{}, err
	}

	if len(raw) == 0 {
		return Config{Projects: map[string]ProjectConfig{}}, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}
	return cfg, nil
}

// Save writes ~/.tk/config.yaml atomically (write to temp, rename).
func Save(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, path)
}

// ConfigPath returns ~/.tk/config.yaml.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName, configFileName), nil
}

// UpsertProject inserts or updates a project entry.
func (cfg *Config) UpsertProject(name string, project ProjectConfig) {
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}
	cfg.Projects[name] = project
}

// CentralStoreRoot returns the central ticket store root directory.
// Checks ~/.ticket/config.yaml for central_root, falls back to ~/.tickets.
func CentralStoreRoot() (string, error) {
	cfg, err := Load()
	if err == nil && cfg.CentralRoot != "" {
		if filepath.IsAbs(cfg.CentralRoot) {
			return cfg.CentralRoot, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tickets"), nil
}

// CentralProjectDir returns <centralRoot>/<projectName>.
func CentralProjectDir(projectName string) (string, error) {
	root, err := CentralStoreRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, projectName), nil
}
