package project

import (
	"os"
	"path/filepath"
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
	t.Setenv("TICKETS_DIR", "")

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

func TestCentralStoreRootIgnoresEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "/custom/tickets")

	// Set central_root in config
	cfg := Config{CentralRoot: "/configured/path", Projects: map[string]ProjectConfig{}}
	Save(cfg)

	root, err := CentralStoreRoot()
	if err != nil {
		t.Fatalf("CentralStoreRoot: %v", err)
	}
	// Should use config, not TICKETS_DIR
	if root != "/configured/path" {
		t.Errorf("CentralStoreRoot = %q, want /configured/path (should ignore TICKETS_DIR)", root)
	}
}

func TestCentralProjectDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

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
