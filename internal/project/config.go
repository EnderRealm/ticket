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

// Config stores tk project configuration (merged view of local + shared).
type Config struct {
	CentralRoot  string                   `yaml:"central_root,omitempty" json:"central_root,omitempty"`
	GitEmail     string                   `yaml:"git_email,omitempty" json:"git_email,omitempty"`
	GitName      string                   `yaml:"git_name,omitempty" json:"git_name,omitempty"`
	DefaultStore string                   `yaml:"default_store,omitempty" json:"default_store,omitempty"`
	SyncInterval string                   `yaml:"sync_interval,omitempty" json:"sync_interval,omitempty"`
	SpawnCommand string                   `yaml:"spawn_command,omitempty" json:"spawn_command,omitempty"`
	Projects     map[string]ProjectConfig `yaml:"projects"`
}

// ProjectConfig stores per-project settings.
type ProjectConfig struct {
	Path         string `yaml:"path,omitempty" json:"path,omitempty"`
	Store        string `yaml:"store,omitempty" json:"store,omitempty"`
	AutoLink     bool   `yaml:"auto_link" json:"auto_link"`
	AutoClose    bool   `yaml:"auto_close" json:"auto_close"`
	RegisteredAt string `yaml:"registered_at,omitempty" json:"registered_at,omitempty"`
}

// Load reads both local (~/.ticket/config.yaml) and shared (<central_root>/config.yaml)
// configs, merging them into a single Config. Local fields (central_root, git_email,
// git_name, default_store, sync_interval, spawn_command, per-project path) come from local config.
// Shared fields (per-project store, auto_link, auto_close, registered_at) come from
// shared config. Missing files are not errors — returns what's available.
func Load() (Config, error) {
	local, err := loadLocalOnly()
	if err != nil {
		return Config{}, err
	}

	// Resolve central root from local config to find shared config
	centralRoot := centralStoreRootFromLocal(local)
	var shared Config
	if centralRoot != "" {
		sharedPath := filepath.Join(centralRoot, configFileName)
		// Missing shared config is fine, but a corrupt one (e.g. unresolved
		// merge conflict markers committed by a misbehaving sync) must surface
		// loudly — silently dropping it strips every project's `store: central`
		// and leaves callers thinking projects are empty.
		s, err := loadFile(sharedPath)
		if err != nil {
			return Config{}, err
		}
		shared = s
	} else {
		shared = Config{Projects: map[string]ProjectConfig{}}
	}

	return mergeConfigs(local, shared), nil
}

// Save writes the config to both local and shared files, splitting fields
// appropriately. Local gets top-level fields + per-project path. Shared gets
// per-project store, auto_link, auto_close, registered_at.
func Save(cfg Config) error {
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}

	if err := saveLocal(cfg); err != nil {
		return err
	}

	// Only write shared config if central root is configured
	centralRoot := centralStoreRootFromLocal(cfg)
	if centralRoot != "" {
		sharedPath := filepath.Join(centralRoot, configFileName)
		return saveShared(cfg, sharedPath)
	}
	return nil
}

// ConfigPath returns ~/.ticket/config.yaml.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName, configFileName), nil
}

// SharedConfigPath returns <central_root>/config.yaml.
func SharedConfigPath() (string, error) {
	root, err := CentralStoreRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, configFileName), nil
}

// UpsertProject inserts or updates a project entry.
func (cfg *Config) UpsertProject(name string, project ProjectConfig) {
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}
	cfg.Projects[name] = project
}

// CentralRegistered reports whether a project is registered with the central
// store. Presence in the merged config is not enough: `store: central` lives in
// the shared config alone — saveLocal writes only `path` per project once a
// central root is set — so a project whose shared config is missing (never
// cloned, or lost in a sync) merges to an entry with an empty store. Write
// authorization, read resolution, and the unregistered markers all key on this
// one predicate so they describe the same set of projects.
func CentralRegistered(cfg Config, name string) bool {
	p, ok := cfg.Projects[name]
	return ok && p.Store == "central"
}

// IsConfigured returns true if ~/.ticket/config.yaml exists and has central_root set.
func IsConfigured() bool {
	local, err := loadLocalOnly()
	if err != nil {
		return false
	}
	return local.CentralRoot != ""
}

// CentralStoreRoot returns the central ticket store root directory.
// Returns an error if central_root is not configured — run `tk init` first.
func CentralStoreRoot() (string, error) {
	local, err := loadLocalOnly()
	if err != nil {
		return "", fmt.Errorf("tk is not configured: %w", err)
	}
	if local.CentralRoot == "" || !filepath.IsAbs(local.CentralRoot) {
		return "", fmt.Errorf("central_root is not configured. Run `tk init` to get started")
	}
	return local.CentralRoot, nil
}

// CentralProjectDir returns <centralRoot>/tickets/<projectName>.
func CentralProjectDir(projectName string) (string, error) {
	root, err := CentralStoreRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tickets", projectName), nil
}

// ResolveWorkDir returns the project's real repo working directory for a tickets
// directory. With the central store, ticketsDir is <centralRoot>/tickets/<project>
// and its parent is the central tickets dir, NOT the repo — so the project's
// recorded `path` from config is used when available. Falls back to the parent
// of ticketsDir (correct for a local .tickets/ at the repo root) when the project
// has no configured path.
func ResolveWorkDir(ticketsDir string, cfg Config) string {
	abs, err := filepath.Abs(ticketsDir)
	if err != nil {
		abs = ticketsDir
	}

	// Derive the project name the same way the TUI does: the base of the
	// tickets dir, or its parent's base when it's a local ".tickets" dir.
	name := filepath.Base(abs)
	if name == ".tickets" {
		name = filepath.Base(filepath.Dir(abs))
	}

	if p, ok := cfg.Projects[name]; ok && p.Path != "" {
		return p.Path
	}
	return filepath.Dir(abs)
}

// --- Internal helpers ---

// loadLocalOnly reads ~/.ticket/config.yaml without merging shared config.
func loadLocalOnly() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	return loadFile(path)
}

// loadFile reads a single config YAML file. Missing or empty returns empty config.
func loadFile(path string) (Config, error) {
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

// centralStoreRootFromLocal extracts central_root from a config.
// Returns empty string if not configured.
func centralStoreRootFromLocal(cfg Config) string {
	if cfg.CentralRoot != "" && filepath.IsAbs(cfg.CentralRoot) {
		return cfg.CentralRoot
	}
	return ""
}

// mergeConfigs combines local and shared configs. Local provides top-level fields
// and per-project path. Shared provides per-project store, auto_link, auto_close,
// registered_at. Projects present in either source are included.
func mergeConfigs(local, shared Config) Config {
	merged := Config{
		CentralRoot:  local.CentralRoot,
		GitEmail:     local.GitEmail,
		GitName:      local.GitName,
		DefaultStore: local.DefaultStore,
		SyncInterval: local.SyncInterval,
		SpawnCommand: local.SpawnCommand,
		Projects:     map[string]ProjectConfig{},
	}

	// Start with shared projects (store, auto_link, auto_close, registered_at)
	for name, sp := range shared.Projects {
		merged.Projects[name] = ProjectConfig{
			Store:        sp.Store,
			AutoLink:     sp.AutoLink,
			AutoClose:    sp.AutoClose,
			RegisteredAt: sp.RegisteredAt,
		}
	}

	// Overlay local projects (path, and fill in shared fields if not in shared)
	for name, lp := range local.Projects {
		if existing, ok := merged.Projects[name]; ok {
			existing.Path = lp.Path
			merged.Projects[name] = existing
		} else {
			// Local-only project (backward compat)
			merged.Projects[name] = lp
		}
	}

	return merged
}

// saveLocal writes ~/.ticket/config.yaml with local-only fields.
func saveLocal(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	localCfg := Config{
		CentralRoot:  cfg.CentralRoot,
		GitEmail:     cfg.GitEmail,
		GitName:      cfg.GitName,
		DefaultStore: cfg.DefaultStore,
		SyncInterval: cfg.SyncInterval,
		SpawnCommand: cfg.SpawnCommand,
		Projects:     map[string]ProjectConfig{},
	}

	hasShared := cfg.CentralRoot != ""
	for name, p := range cfg.Projects {
		if hasShared {
			// Only store path locally — shared fields go to shared config
			if p.Path != "" {
				localCfg.Projects[name] = ProjectConfig{Path: p.Path}
			}
		} else {
			// No shared config available — store everything locally
			localCfg.Projects[name] = p
		}
	}

	return writeFileAtomic(path, localCfg)
}

// saveShared writes <central_root>/config.yaml with shared-only fields.
func saveShared(cfg Config, path string) error {
	sharedCfg := Config{
		Projects: map[string]ProjectConfig{},
	}

	for name, p := range cfg.Projects {
		if p.Store != "" {
			sharedCfg.Projects[name] = ProjectConfig{
				Store:        p.Store,
				AutoLink:     p.AutoLink,
				AutoClose:    p.AutoClose,
				RegisteredAt: p.RegisteredAt,
			}
		}
	}

	return writeFileAtomic(path, sharedCfg)
}

// writeFileAtomic writes a config to path via temp file + rename.
func writeFileAtomic(path string, cfg Config) error {
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
