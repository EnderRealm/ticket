package project

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

const (
	configDirName  = ".ticket"
	configFileName = "config.yaml"

	// StoreRootEnv names the environment variable that overrides the configured
	// central store root with an explicit one, so a test harness or an agent can
	// point tk — `tk serve` above all — at a throwaway store without touching the
	// real one. It is an environment variable rather than a cobra flag on purpose:
	// pkg/ticket and internal/mcp resolve config directly and never see cobra's
	// flags, and the env is the lever a harness has over a subprocess it spawns.
	StoreRootEnv = "TK_STORE_ROOT"
)

// Config stores tk project configuration (merged view of local + shared).
type Config struct {
	CentralRoot string `yaml:"central_root,omitempty" json:"central_root,omitempty"`
	GitEmail    string `yaml:"git_email,omitempty" json:"git_email,omitempty"`
	GitName     string `yaml:"git_name,omitempty" json:"git_name,omitempty"`
	// DefaultStore decides nothing and never did since the central store became
	// the only topology: no code reads it to choose anything, and tk never
	// writes it. It is retained so an old config still loads and round-trips,
	// the same terms as ProjectConfig.Store. Not a hook to wire up — a second
	// store kind is what ticket/remove-local-tickets-3d72 removed.
	DefaultStore string `yaml:"default_store,omitempty" json:"default_store,omitempty"`
	SyncInterval string `yaml:"sync_interval,omitempty" json:"sync_interval,omitempty"`
	// SpawnCommand: read it via project.SpawnCommand(). The value here is the
	// merged one, whose local half is the override root's config under
	// TK_STORE_ROOT, and it exists for Save's round-trip only — never as a read
	// path, because the template is the string handed to `sh -c` as the machine
	// owner.
	SpawnCommand string `yaml:"spawn_command,omitempty" json:"spawn_command,omitempty"`
	// VerifyAllow: read it via project.VerifyAllow(), for the same reason and on
	// the same terms — merged from a half the override root may own, kept here so
	// Save round-trips it, and never a read path, because the list decides which
	// programs run as the machine owner.
	VerifyAllow VerifyAllowList          `yaml:"verify_allow,omitempty" json:"verify_allow,omitempty"`
	Projects    map[string]ProjectConfig `yaml:"projects"`
}

// VerifyAllowList is the verify_allow setting. Absent and present-but-empty
// mean different things — fall back to the default, versus refuse everything —
// so it defines IsZero rather than letting omitempty collapse them: a save
// round-trip (project registration, for one) must not turn a user's explicit
// `verify_allow: []` back into an unset key, which would silently restore the
// defaults.
type VerifyAllowList []string

// IsZero reports whether omitempty should drop the field. Only an absent list
// is dropped.
func (l VerifyAllowList) IsZero() bool { return l == nil }

// defaultVerifyAllow is the argv[0] allow-list used when local config sets no
// verify_allow: the ordinary test runners. Allow-listing a program is not a
// claim that it is safe — it trusts whoever can write a verify line with
// everything that program can do, including the forms that reach outside the
// repo:
//
//   - `go run pkg@version` and `go install pkg@version` fetch and execute a
//     remote module.
//   - `cargo install <crate>` fetches a remote crate and executes its build.rs
//     at build time.
//   - `make -f <file>` (or `make -C <dir>`) runs recipes from a file the
//     command line names rather than the repo's Makefile.
//
// All are reachable here; `go test` is the primary use case and cannot be
// dropped over them. Shells and general-purpose language interpreters are
// absent because they make that reach unconditional and buy nothing back:
// `sh -c` or `python3 -c` runs whatever string the ticket supplied. swift is
// absent for that reason and not as a build driver — `swift -e '<code>'` runs
// arbitrary Swift with the full standard library, so a Swift project wanting
// `swift test` adds swift to verify_allow deliberately, accepting that a verify
// line can then run any Swift the ticket names. npm, pnpm and yarn are absent
// because `npm exec`, `pnpm dlx` and `yarn dlx` fetch and run an arbitrary
// remote package as a documented feature, so a JS project that adds one back is
// opting into a verify line being able to run any package on the registry.
var defaultVerifyAllow = []string{"go", "make", "cargo", "pytest"}

// ProjectConfig stores per-project settings.
type ProjectConfig struct {
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Store is the registration marker. "central" is the only value tk writes
	// and the only one that means anything: tk resolves no other kind of store,
	// so an entry carrying an old `store: local` reads as an unregistered
	// project — the repo owns no store and every command says so, naming its
	// .tickets/ if it still has one. The field is kept rather than dropped so
	// those configs still load, and so a save round-trips whatever a project
	// entry already carried instead of rewriting another machine's config.
	//
	// CentralRegistered is the only reader that decides anything from it, and it
	// asks only whether the value is "central". The two others neither branch on
	// a value nor compare one: saveShared tests presence to decide whether a
	// project entry belongs in the shared config at all, and `tk status` prints
	// the raw string in its store column.
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
	centralRoot, err := centralStoreRootFromLocal(local)
	if err != nil {
		return Config{}, err
	}
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

	// One resolution decides both halves: saveLocal must leave the shared-owned
	// per-project fields out of the local file exactly when saveShared is about
	// to write them, or a stale local copy keeps merging `store: central` for a
	// project the shared config no longer lists.
	centralRoot, err := centralStoreRootFromLocal(cfg)
	if err != nil {
		return err
	}

	if err := saveLocal(cfg, centralRoot != ""); err != nil {
		return err
	}

	// Only write shared config if central root is configured
	if centralRoot != "" {
		sharedPath := filepath.Join(centralRoot, configFileName)
		return saveShared(cfg, sharedPath)
	}
	return nil
}

// StoreRootOverride returns the TK_STORE_ROOT store root, whether the variable
// is set at all, and the error a set-but-unusable value is. A value that is not
// an absolute path is that error and never falls back to the configured root:
// silently resolving the real central store from a broken override is the exact
// failure the override exists to prevent.
//
// Presence is tested with LookupEnv rather than by comparing the value to "",
// because the empty string is a set-but-unusable value, not an absent one: a
// harness exporting a variable that expanded to nothing has named a store root
// tk cannot resolve, and treating it as unset would resolve the configured one.
func StoreRootOverride() (string, bool, error) {
	root, ok := os.LookupEnv(StoreRootEnv)
	if !ok {
		return "", false, nil
	}
	if !filepath.IsAbs(root) {
		return "", true, fmt.Errorf("%s must be an absolute path, got %q", StoreRootEnv, root)
	}
	return root, true, nil
}

// ConfigPath returns ~/.ticket/config.yaml, or the override root's own
// .ticket/config.yaml when TK_STORE_ROOT is set. Keeping the local config
// inside the override root mirrors the production layout and keeps an isolated
// run from reading or writing the machine's shared one.
func ConfigPath() (string, error) {
	if root, ok, err := StoreRootOverride(); ok {
		if err != nil {
			return "", err
		}
		return filepath.Join(root, configDirName, configFileName), nil
	}
	return homeConfigPath()
}

// homeConfigPath returns ~/.ticket/config.yaml — the machine owner's own
// config, which TK_STORE_ROOT never relocates. Only the controls that must
// answer to the machine owner rather than to whoever set the environment use
// it directly; everything else goes through ConfigPath.
//
// Two settings are read from it: verify_allow and spawn_command. They share the
// property that decides it — each one decides code that runs as the machine
// owner, verify_allow by permitting a program and spawn_command by being a
// shell string — so neither may come from a root whoever set the variable owns.
func homeConfigPath() (string, error) {
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

// IsConfigured returns true if ~/.ticket/config.yaml exists and has central_root
// set, or if TK_STORE_ROOT is set — an override is configuration in itself, so an
// isolated run needs no local config file at all.
//
// A set-but-unusable override still reports configured: it is a broken store
// root, not a missing configuration, and the CLI refuses it before this gate
// with an error naming the variable. Reporting it unconfigured here would send
// the reader to `tk init`, and falling through to the local config would
// resolve the real store.
func IsConfigured() bool {
	if _, ok, _ := StoreRootOverride(); ok {
		return true
	}
	local, err := loadLocalOnly()
	if err != nil {
		return false
	}
	return local.CentralRoot != ""
}

// VerifyAllow returns the argv[0] allow-list a ticket's verify commands are
// checked against, plus the error that made it unreadable. It reads
// ~/.ticket/config.yaml directly rather than the merged config on purpose: the
// shared config lives inside the synced tickets repo, so whatever can push a
// malicious verify command there could widen the list meant to refuse it in
// the same push.
//
// It reads the home path directly rather than ConfigPath for the same reason:
// TK_STORE_ROOT deliberately does not move this one setting. The override root
// belongs to whoever set the variable — a harness, or an agent — so relocating
// the allow-list there would let a sandbox widen it (`verify_allow: [sh]`), and
// a fresh sandbox with no config at all would silently restore the defaults
// over a list the machine owner had narrowed. The allow-list always comes from
// the machine owner's own config, so a sandbox can neither widen nor narrow it.
//
// Three states are kept apart so the control fails closed. A local config that
// cannot be read or parsed — partial write, merge conflict markers, bad
// permissions — returns an empty list and the error, so nothing runs and the
// refusal can say why rather than silently restoring defaults over a list the
// user had narrowed. A `verify_allow` that is present but empty returns empty,
// which is how a user refuses everything. Only a genuinely absent key falls
// back to defaultVerifyAllow.
func VerifyAllow() ([]string, error) {
	path, err := homeConfigPath()
	if err != nil {
		return []string{}, err
	}
	local, err := loadFile(path)
	if err != nil {
		return []string{}, err
	}
	if local.VerifyAllow == nil {
		// A copy: a caller appending within capacity would otherwise rewrite the
		// process-wide default of a security control.
		return slices.Clone(defaultVerifyAllow), nil
	}
	return local.VerifyAllow, nil
}

// SpawnCommand returns the TUI's spawn_command template, plus the error that
// made it unreadable. It reads ~/.ticket/config.yaml directly for the same
// reason VerifyAllow does: the template is handed to `sh -c`, so whoever
// supplies it runs code as the machine owner, and neither the synced shared
// config nor a TK_STORE_ROOT root the caller named may be that source.
//
// An empty template is the caller's signal to use the built-in default, so a
// sandbox with no home config gets the machine owner's default rather than a
// template of its own.
func SpawnCommand() (string, error) {
	path, err := homeConfigPath()
	if err != nil {
		return "", err
	}
	local, err := loadFile(path)
	if err != nil {
		return "", err
	}
	return local.SpawnCommand, nil
}

// CentralStoreRoot returns the central ticket store root directory, or the
// TK_STORE_ROOT override when set — which is answered without reading any
// config file, so no configured store is touched.
// Returns an error if central_root is not configured — run `tk init` first.
func CentralStoreRoot() (string, error) {
	if root, ok, err := StoreRootOverride(); ok {
		return root, err
	}
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
// directory: the `path` its config entry records, or "" when it records none —
// a project registered on another machine has no local path, and the local half
// of the config is the only thing that knows where the repo is.
//
// There is nothing to fall back to. ticketsDir is always
// <centralRoot>/tickets/<project>, so its parent is the central tickets dir and
// never a repo; returning it would hand the caller a directory inside the ticket
// store, which for `tk ui` is the directory a spawned work session runs in. The
// caller decides what to do with "" — it knows which repo the resolution started
// from, and this does not.
func ResolveWorkDir(ticketsDir string, cfg Config) string {
	abs, err := filepath.Abs(ticketsDir)
	if err != nil {
		abs = ticketsDir
	}

	// Derive the project name the same way the TUI does: the base of the
	// tickets dir.
	name := filepath.Base(abs)

	if p, ok := cfg.Projects[name]; ok && p.Path != "" {
		return p.Path
	}
	return ""
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

// centralStoreRootFromLocal extracts central_root from a config, or returns the
// TK_STORE_ROOT override when set — the shared config lives under whichever root
// wins, so an isolated run must not read or write the configured store's.
// Returns empty string if not configured.
func centralStoreRootFromLocal(cfg Config) (string, error) {
	if root, ok, err := StoreRootOverride(); ok {
		return root, err
	}
	if cfg.CentralRoot != "" && filepath.IsAbs(cfg.CentralRoot) {
		return cfg.CentralRoot, nil
	}
	return "", nil
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
		VerifyAllow:  local.VerifyAllow,
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

// saveLocal writes ~/.ticket/config.yaml with local-only fields. hasShared says
// whether a shared config is being written alongside it, and comes from Save's
// single resolution of the central root.
func saveLocal(cfg Config, hasShared bool) error {
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
		VerifyAllow:  cfg.VerifyAllow,
		Projects:     map[string]ProjectConfig{},
	}

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
