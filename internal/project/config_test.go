package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := Config{Projects: map[string]ProjectConfig{}}
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

func TestCentralStoreRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

	root, err := CentralStoreRoot()
	if err != nil {
		t.Fatalf("CentralStoreRoot: %v", err)
	}
	expected := filepath.Join(home, "code", "forge-data", "tickets")
	if root != expected {
		t.Errorf("CentralStoreRoot = %q, want %q", root, expected)
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

	root, err := CentralStoreRoot()
	if err != nil {
		t.Fatalf("CentralStoreRoot: %v", err)
	}
	// CentralStoreRoot should NOT use TICKETS_DIR — different semantics
	expected := filepath.Join(home, "code", "forge-data", "tickets")
	if root != expected {
		t.Errorf("CentralStoreRoot = %q, want %q (should ignore TICKETS_DIR)", root, expected)
	}
}

func TestCentralProjectDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

	dir, err := CentralProjectDir("myproject")
	if err != nil {
		t.Fatalf("CentralProjectDir: %v", err)
	}
	expected := filepath.Join(home, "code", "forge-data", "tickets", "myproject")
	if dir != expected {
		t.Errorf("CentralProjectDir = %q, want %q", dir, expected)
	}
}
